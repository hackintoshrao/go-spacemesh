package syncer_test

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
)

type testForkFinder struct {
	*syncer.ForkFinder
	db       *sql.Database
	mFetcher *mocks.Mockfetcher
}

func newTestForkFinder(t *testing.T, maxHashes uint32) *testForkFinder {
	mf := mocks.NewMockfetcher(gomock.NewController(t))
	db := sql.InMemory()
	require.NoError(t, layers.SetHashes(db, types.GetEffectiveGenesis(), types.RandomHash(), types.RandomHash()))
	return &testForkFinder{
		ForkFinder: syncer.NewForkFinder(logtest.New(t), db, mf, maxHashes, time.Hour),
		db:         db,
		mFetcher:   mf,
	}
}

func TestForkFinder_Purge(t *testing.T) {
	tf := newTestForkFinder(t, 5)
	numCached := 10
	tf.UpdateAgreement(p2p.Peer(strconv.Itoa(0)), types.NewLayerID(uint32(1)), types.RandomHash(), time.Now().Add(-2*time.Hour))
	for i := 1; i < numCached; i++ {
		tf.UpdateAgreement(p2p.Peer(strconv.Itoa(i)), types.NewLayerID(uint32(i+1)), types.RandomHash(), time.Now())
	}
	tf.mFetcher.EXPECT().GetPeers().Return([]p2p.Peer{})
	require.Equal(t, numCached, tf.NumPeersCached())
	tf.Purge(false)
	require.Equal(t, 9, tf.NumPeersCached())
	tf.Purge(false, p2p.Peer(strconv.Itoa(numCached-1)), p2p.Peer(strconv.Itoa(numCached-2)))
	require.Equal(t, 7, tf.NumPeersCached())
	tf.Purge(true)
	require.Equal(t, 0, tf.NumPeersCached())
}

func createPeerHashes(max uint32) []types.Hash32 {
	peerHashes := make([]types.Hash32, max+1)
	gLid := types.GetEffectiveGenesis()
	for i := uint32(0); i <= max; i++ {
		lid := types.NewLayerID(i)
		if lid.Before(gLid) {
			peerHashes[i] = types.Hash32{}
		} else {
			peerHashes[i] = types.RandomHash()
		}
	}
	return peerHashes
}

func storeNodeHashes(t *testing.T, db *sql.Database, peerHashes []types.Hash32, diverge types.LayerID) {
	for i, hash := range peerHashes {
		lid := types.NewLayerID(uint32(i))
		if lid.Before(diverge) {
			require.NoError(t, layers.SetHashes(db, lid, types.Hash32{}, hash))
		} else {
			require.NoError(t, layers.SetHashes(db, lid, types.Hash32{}, types.RandomHash()))
		}
	}
}

func serveHashReq(t *testing.T, req *fetch.MeshHashRequest, peerHashes []types.Hash32) (*fetch.MeshHashes, error) {
	var (
		lids   = []types.LayerID{req.From}
		hashes = []types.Hash32{peerHashes[req.From.Uint32()]}
		steps  uint32
		lid    = req.From.Add(req.Delta)
	)
	for ; ; lid = lid.Add(req.Delta) {
		steps++
		if !lid.Before(req.To) {
			lids = append(lids, req.To)
			hashes = append(hashes, peerHashes[req.To.Uint32()])
			break
		}
		lids = append(lids, lid)
		hashes = append(hashes, peerHashes[lid.Uint32()])
	}
	require.Equal(t, req.Steps, steps, fmt.Sprintf("exp: %v, got %v", req.Steps, steps))
	mh := &fetch.MeshHashes{
		Layers: lids,
		Hashes: hashes,
	}
	return mh, nil
}

func TestForkFinder_FindFork_Permutation(t *testing.T) {
	peer := p2p.Peer("grumpy")
	max := uint32(173)
	diverge := uint32(rand.Intn(int(max)))
	gLid := types.GetEffectiveGenesis()
	if diverge < gLid.Uint32() {
		diverge = gLid.Uint32() + 1
	}
	expected := types.NewLayerID(diverge - 1)
	peerHashes := createPeerHashes(max)
	maxLid := types.NewLayerID(max)
	for maxHashes := uint32(30); maxHashes >= 5; maxHashes -= 3 {
		for lid := maxLid; lid.After(expected); lid = lid.Sub(1) {
			tf := newTestForkFinder(t, maxHashes)
			storeNodeHashes(t, tf.db, peerHashes, types.NewLayerID(diverge))
			tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
					return serveHashReq(t, req, peerHashes)
				}).AnyTimes()

			fork, err := tf.FindFork(context.TODO(), peer, lid, peerHashes[lid.Uint32()])
			require.NoError(t, err)
			require.Equal(t, expected, fork)
		}
	}
}

func TestForkFinder_MeshChangedMidSession(t *testing.T) {
	maxHashes := uint32(100)
	peer := p2p.Peer("grumpy")
	lastAgreedLid := types.NewLayerID(35)
	lastAgreedHash := types.RandomHash()

	t.Run("peer mesh changed", func(t *testing.T) {
		t.Parallel()

		tf := newTestForkFinder(t, maxHashes)
		require.NoError(t, layers.SetHashes(tf.db, lastAgreedLid, types.RandomHash(), lastAgreedHash))
		tf.UpdateAgreement(peer, lastAgreedLid, lastAgreedHash, time.Now())
		tf.UpdateAgreement("shorty", types.NewLayerID(111), types.RandomHash(), time.Now())
		require.Equal(t, tf.NumPeersCached(), 2)
		tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
				mh := &fetch.MeshHashes{
					Layers: []types.LayerID{types.NewLayerID(35), types.NewLayerID(36), types.NewLayerID(37)},
					Hashes: []types.Hash32{types.RandomHash(), types.RandomHash(), types.RandomHash()},
				}
				return mh, nil
			})

		_, err := tf.FindFork(context.TODO(), peer, types.NewLayerID(37), types.RandomHash())
		require.ErrorIs(t, err, syncer.ErrPeerMeshChangedMidSession)
		require.Equal(t, tf.NumPeersCached(), 1)
	})

	t.Run("node mesh changed", func(t *testing.T) {
		t.Parallel()

		tf := newTestForkFinder(t, maxHashes)
		require.NoError(t, layers.SetHashes(tf.db, lastAgreedLid, types.RandomHash(), lastAgreedHash))
		tf.UpdateAgreement(peer, lastAgreedLid, lastAgreedHash, time.Now())
		tf.UpdateAgreement("shorty", types.NewLayerID(111), types.RandomHash(), time.Now())
		require.Equal(t, tf.NumPeersCached(), 2)
		lastDiffLid := types.NewLayerID(37)
		lastDiffHash := types.RandomHash()
		tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
				mh := &fetch.MeshHashes{
					Layers: []types.LayerID{types.NewLayerID(35), types.NewLayerID(36), types.NewLayerID(37)},
					Hashes: []types.Hash32{lastAgreedHash, types.RandomHash(), lastDiffHash},
				}
				// changes the node's own hash for lastAgreedLid
				for _, lid := range mh.Layers {
					require.NoError(t, layers.SetHashes(tf.db, lid, types.RandomHash(), types.RandomHash()))
				}
				return mh, nil
			})

		_, err := tf.FindFork(context.TODO(), peer, lastDiffLid, lastDiffHash)
		require.ErrorIs(t, err, syncer.ErrNodeMeshChangedMidSession)
		require.Equal(t, tf.NumPeersCached(), 0)
	})
}

func TestForkFinder_FindFork_Edges(t *testing.T) {
	max := types.NewLayerID(20)
	diverge := types.NewLayerID(12)
	tt := []struct {
		name               string
		lastSame, lastDiff types.LayerID
		expReqs            int
	}{
		{
			name:     "no prior hash agreement",
			lastDiff: types.NewLayerID(20),
			expReqs:  2,
		},
		{
			name:     "prior agreement",
			lastDiff: types.NewLayerID(20),
			lastSame: types.NewLayerID(8),
			expReqs:  2,
		},
		{
			name:     "immediate detection",
			lastDiff: types.NewLayerID(12),
			lastSame: types.NewLayerID(11),
			expReqs:  0,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			maxHashes := uint32(5)
			peerHashes := createPeerHashes(max.Uint32())
			tf := newTestForkFinder(t, maxHashes)
			storeNodeHashes(t, tf.db, peerHashes, diverge)

			peer := p2p.Peer("grumpy")
			if tc.lastSame != (types.LayerID{}) {
				tf.UpdateAgreement(peer, tc.lastSame, peerHashes[tc.lastSame.Uint32()], time.Now())
			}

			tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
					return serveHashReq(t, req, peerHashes)
				}).Times(tc.expReqs)

			fork, err := tf.FindFork(context.TODO(), peer, tc.lastDiff, peerHashes[tc.lastDiff.Uint32()])
			require.NoError(t, err)
			require.Equal(t, types.NewLayerID(11), fork)
		})
	}
}
