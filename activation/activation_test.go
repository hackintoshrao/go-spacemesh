package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/mocks"
	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/kvstore"
)

// ========== Vars / Consts ==========

const (
	layersPerEpoch   = 10
	postGenesisEpoch = 2

	testTickSize = 1
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

var (
	sig         = NewMockSigner()
	otherSig    = NewMockSigner()
	coinbase    = types.GenerateAddress([]byte("33333"))
	goldenATXID = types.ATXID(types.HexToHash32("77777"))
	prevAtxID   = types.ATXID(types.HexToHash32("44444"))
	chlng       = types.HexToHash32("55555")
	poetRef     = types.BytesToHash([]byte("66666"))
	poetBytes   = []byte("66666")

	postGenesisEpochLayer = types.NewLayerID(22)

	net               = &NetMock{}
	layerClockMock    = &LayerClockMock{}
	nipostBuilderMock = &NIPostBuilderMock{}
	nipost            = newNIPostWithChallenge(&chlng, poetBytes)
	initialPost       = &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}
)

// ========== Mocks ==========

type NetMock struct {
	lastTransmission []byte
	atxHdlr          atxHandler
}

func (n *NetMock) Publish(_ context.Context, _ string, d []byte) error {
	n.lastTransmission = d
	go n.hookToAtxPool(d)
	return nil
}

func (n *NetMock) hookToAtxPool(transmission []byte) {
	if atx, err := types.BytesToAtx(transmission); err == nil {
		if err := atx.CalcAndSetID(); err != nil {
			panic(err)
		}
		if err := atx.CalcAndSetNodeID(); err != nil {
			panic(err)
		}

		if n.atxHdlr != nil {
			if atxDb, ok := n.atxHdlr.(*Handler); ok {
				vAtx, err := atx.Verify(0, 1)
				if err != nil {
					panic(err)
				}
				if err := atxDb.StoreAtx(context.TODO(), vAtx); err != nil {
					panic(err)
				}
			}
		}
	}
}

func NewMockSigner() *MockSigning {
	return &MockSigning{signing.NewEdSigner()}
}

// TODO(mafa): replace this mock with the generated mock from "github.com/spacemeshos/go-spacemesh/signing/mocks".
type MockSigning struct {
	signer *signing.EdSigner
}

func (ms *MockSigning) NodeID() types.NodeID {
	return types.BytesToNodeID(ms.signer.PublicKey().Bytes())
}

func (ms *MockSigning) Sign(m []byte) []byte {
	return ms.signer.Sign(m)
}

type NIPostBuilderMock struct {
	poetRef         []byte
	buildNIPostFunc func(challenge *types.Hash32, commitmentAtx types.ATXID) (*types.NIPost, time.Duration, error)
	SleepTime       int
}

func (np NIPostBuilderMock) updatePoETProvers([]PoetProvingServiceClient) {}

func (np *NIPostBuilderMock) BuildNIPost(_ context.Context, challenge *types.Hash32, commitmentAtx types.ATXID, _ time.Time) (*types.NIPost, time.Duration, error) {
	if np.buildNIPostFunc != nil {
		return np.buildNIPostFunc(challenge, commitmentAtx)
	}
	return newNIPostWithChallenge(challenge, np.poetRef), 0, nil
}

// TODO(mafa): use gomock instead of this.
type NIPostErrBuilderMock struct{}

func (np *NIPostErrBuilderMock) updatePoETProvers([]PoetProvingServiceClient) {}

func (np *NIPostErrBuilderMock) BuildNIPost(context.Context, *types.Hash32, types.ATXID, time.Time) (*types.NIPost, time.Duration, error) {
	return nil, 0, fmt.Errorf("NIPost builder error")
}

// TODO(mafa): use gomock instead of this; see handler_test.go for examples.
type ValidatorMock struct{}

func (*ValidatorMock) Validate([]byte, *types.NIPost, types.Hash32, uint32) (uint64, error) {
	return 1, nil
}

func (*ValidatorMock) ValidatePost([]byte, *types.Post, *types.PostMetadata, uint32) error {
	return nil
}

// TODO(mafa): use gomock instead of this.
type FaultyNetMock struct {
	bt     []byte
	retErr bool
}

func (n *FaultyNetMock) Publish(_ context.Context, _ string, d []byte) error {
	n.bt = d
	if n.retErr {
		return fmt.Errorf("faulty")
	}
	// not calling `go hookToAtxPool(d)`
	return nil
}

// ========== Helper functions ==========

func newCachedDB(tb testing.TB) *datastore.CachedDB {
	return datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
}

func newAtxHandler(tb testing.TB, cdb *datastore.CachedDB) *Handler {
	receiver := mocks.NewMockatxReceiver(gomock.NewController(tb))
	return NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, &ValidatorMock{}, receiver, logtest.New(tb).WithName("atxHandler"))
}

func newChallenge(sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID, cATX *types.ATXID) types.NIPostChallenge {
	return types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
		CommitmentATX:  cATX,
	}
}

func newAtx(t testing.TB, challenge types.NIPostChallenge, sig *MockSigning, nipost *types.NIPost, numUnits uint32, coinbase types.Address) *types.ActivationTx {
	atx := types.NewActivationTx(challenge, coinbase, nipost, numUnits, nil)
	require.NoError(t, SignAtx(sig, atx))
	require.NoError(t, atx.CalcAndSetID())
	require.NoError(t, atx.CalcAndSetNodeID())
	return atx
}

func newActivationTx(
	t testing.TB,
	sig *MockSigning,
	sequence uint64,
	prevATX types.ATXID,
	positioningATX types.ATXID,
	cATX *types.ATXID,
	pubLayerID types.LayerID,
	startTick, numTicks uint64,
	coinbase types.Address,
	numUnits uint32,
	nipost *types.NIPost,
) *types.VerifiedActivationTx {
	challenge := newChallenge(sequence, prevATX, positioningATX, pubLayerID, cATX)
	atx := newAtx(t, challenge, sig, nipost, numUnits, coinbase)
	vAtx, err := atx.Verify(startTick, numTicks)
	require.NoError(t, err)
	return vAtx
}

type LayerClockMock struct {
	currentLayer types.LayerID
}

func (l *LayerClockMock) LayerToTime(types.LayerID) time.Time {
	return time.Time{}
}

func (l *LayerClockMock) GetCurrentLayer() types.LayerID {
	return l.currentLayer
}

func (l *LayerClockMock) AwaitLayer(types.LayerID) chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		close(ch)
	}()
	return ch
}

// TODO(mafa): replace this mock a generated one.
type mockSyncer struct{}

func (m *mockSyncer) RegisterForATXSynced() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func newBuilder(tb testing.TB, cdb *datastore.CachedDB, hdlr atxHandler, opts ...BuilderOption) *Builder {
	net.atxHdlr = hdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, hdlr, net, nipostBuilderMock, &postSetupProviderMock{},
		layerClockMock, &mockSyncer{}, logtest.New(tb).WithName("atxBuilder"), opts...)
	b.initialPost = initialPost
	b.commitmentAtx = &goldenATXID
	return b
}

func lastTransmittedAtx(t *testing.T) types.ActivationTx {
	var signedAtx types.ActivationTx
	err := codec.Decode(net.lastTransmission, &signedAtx)
	require.NoError(t, err)
	return signedAtx
}

func assertLastAtx(r *require.Assertions, posAtx, prevAtx *types.VerifiedActivationTx, layersPerEpoch uint32) {
	sigAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	r.NoError(sigAtx.CalcAndSetNodeID())

	atx := sigAtx
	r.Equal(sig.NodeID(), atx.NodeID())
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.ID(), atx.PrevATXID)
		r.Nil(atx.InitialPost)
		r.Nil(atx.InitialPostIndices)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(*types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.InitialPost)
		r.NotNil(atx.InitialPostIndices)
	}
	r.Equal(posAtx.ID(), atx.PositioningATX)
	r.Equal(posAtx.PubLayerID.Add(layersPerEpoch), atx.PubLayerID)
	r.Equal(poetRef, atx.GetPoetProofRef())
}

func publishAtx(b *Builder, clockEpoch types.EpochID, buildNIPostLayerDuration uint32) (published, builtNIPost bool, err error) {
	net.lastTransmission = nil
	nipostBuilderMock.buildNIPostFunc = func(challenge *types.Hash32, commitmentAtx types.ATXID) (*types.NIPost, time.Duration, error) {
		builtNIPost = true
		layerClockMock.currentLayer = layerClockMock.currentLayer.Add(buildNIPostLayerDuration)
		return newNIPostWithChallenge(challenge, poetBytes), 0, nil
	}
	layerClockMock.currentLayer = clockEpoch.FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	nipostBuilderMock.buildNIPostFunc = nil
	return net.lastTransmission != nil, builtNIPost, err
}

func addPrevAtx(t *testing.T, db sql.Executor, epoch types.EpochID) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: epoch.FirstLayer(),
			},
		},
	}
	return addAtx(t, db, sig, atx)
}

func addAtx(t *testing.T, db sql.Executor, sig signer, atx *types.ActivationTx) *types.VerifiedActivationTx {
	require.NoError(t, SignAtx(sig, atx))
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx, time.Now()))
	return vAtx
}

// ========== Tests ==========

func TestBuilder_waitForFirstATX(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	poetCfg := PoetConfig{
		PhaseShift:  5 * time.Millisecond,
		CycleGap:    2 * time.Millisecond,
		GracePeriod: time.Millisecond,
	}
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, &postSetupProviderMock{},
		mClock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"),
		WithPoetConfig(poetCfg))
	b.initialPost = initialPost

	ch := make(chan struct{}, 1)
	close(ch)
	current := types.NewLayerID(layersPerEpoch * 2) // first layer of epoch 2
	addPrevAtx(t, cdb, current.GetEpoch()-1)
	mClock.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
	mClock.EXPECT().LayerToTime(current).Return(time.Now().Add(100 * time.Millisecond))
	require.True(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_waitForFirstATX_nextEpoch(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	poetCfg := PoetConfig{
		PhaseShift:  5 * time.Millisecond,
		CycleGap:    2 * time.Millisecond,
		GracePeriod: time.Millisecond,
	}
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, &postSetupProviderMock{},
		mClock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"),
		WithPoetConfig(poetCfg))
	b.initialPost = initialPost

	ch := make(chan struct{}, 1)
	close(ch)
	current := types.NewLayerID(layersPerEpoch * 2) // first layer of epoch 2
	addPrevAtx(t, cdb, current.GetEpoch()-1)
	mClock.EXPECT().GetCurrentLayer().Return(current)
	mClock.EXPECT().LayerToTime(current).Return(time.Now().Add(-5 * time.Millisecond))
	mClock.EXPECT().AwaitLayer(current.Add(layersPerEpoch)).Return(ch)
	mClock.EXPECT().GetCurrentLayer().Return(current.Add(layersPerEpoch)).AnyTimes()
	require.True(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_waitForFirstATX_Genesis(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b.layerClock = mClock

	current := types.NewLayerID(0)
	mClock.EXPECT().GetCurrentLayer().Return(current)
	require.False(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_waitForFirstATX_NoWait(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)
	mClock := mocks.NewMocklayerClock(gomock.NewController(t))
	b.layerClock = mClock

	current := types.NewLayerID(layersPerEpoch)
	addPrevAtx(t, cdb, current.GetEpoch())
	mClock.EXPECT().GetCurrentLayer().Return(current)
	require.False(t, b.waitForFirstATX(context.TODO()))
}

func TestBuilder_StartSmeshingCoinbase(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)

	coinbase := types.Address{1, 1, 1}
	require.NoError(t, builder.StartSmeshing(coinbase, atypes.PostSetupOpts{}))
	t.Cleanup(func() { builder.StopSmeshing(true) })
	require.Equal(t, coinbase, builder.Coinbase())
}

func TestBuilder_StartSmeshingTwiceError(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)

	coinbase := types.Address{1, 1, 1}
	require.NoError(t, builder.StartSmeshing(coinbase, atypes.PostSetupOpts{}))
	require.ErrorContains(t, builder.StartSmeshing(coinbase, atypes.PostSetupOpts{}), "already started")
}

func TestBuilder_StartSmeshingAfterError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)

	postSetupMock := mocks.NewMockPostSetupProvider(ctrl)
	postSetupMock.EXPECT().StartSession(gomock.Any(), gomock.Any()).Times(1).Return(nil, errors.New("couldn't start session"))
	postSetupMock.EXPECT().StartSession(gomock.Any(), gomock.Any()).Return(make(chan struct{}), nil)

	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, postSetupMock,
		layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.initialPost = initialPost

	coinbase := types.Address{1, 1, 1}
	require.ErrorContains(t, b.StartSmeshing(coinbase, atypes.PostSetupOpts{}), "couldn't start session")
	require.NoError(t, b.StartSmeshing(coinbase, atypes.PostSetupOpts{}))
}

func TestBuilder_RestartSmeshing(t *testing.T) {
	getBuilder := func(t *testing.T) *Builder {
		cdb := newCachedDB(t)
		atxHdlr := newAtxHandler(t, cdb)
		net.atxHdlr = atxHdlr
		cfg := Config{
			CoinbaseAccount: coinbase,
			GoldenATXID:     goldenATXID,
			LayersPerEpoch:  layersPerEpoch,
		}
		sessionChan := make(chan struct{})
		close(sessionChan)
		builder := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock,
			&postSetupProviderMock{sessionChan: sessionChan},
			layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
		builder.initialPost = initialPost
		return builder
	}

	t.Run("Single threaded", func(t *testing.T) {
		builder := getBuilder(t)
		for i := 0; i < 100; i++ {
			require.NoError(t, builder.StartSmeshing(types.Address{}, atypes.PostSetupOpts{}))
			require.Never(t, func() bool { return !builder.Smeshing() }, 400*time.Microsecond, 50*time.Microsecond, "failed on execution %d", i)
			require.Truef(t, builder.Smeshing(), "failed on execution %d", i)
			require.NoError(t, builder.StopSmeshing(true))
			require.Eventually(t, func() bool { return !builder.Smeshing() }, 100*time.Millisecond, time.Millisecond, "failed on execution %d", i)
		}
	})

	t.Run("Multi threaded", func(t *testing.T) {
		// Meant to be run with -race to detect races.
		// It cannot check `builder.Smeshing()` as Start/Stop is happening from many goroutines simultaneously.
		// Both Start and Stop can fail as it is not known if builder is smeshing or not.
		builder := getBuilder(t)
		eg, _ := errgroup.WithContext(context.Background())
		for worker := 0; worker < 10; worker += 1 {
			eg.Go(func() error {
				for i := 0; i < 100; i++ {
					builder.StartSmeshing(types.Address{}, atypes.PostSetupOpts{})
					builder.StopSmeshing(true)
				}
				return nil
			})
		}
		eg.Wait()
	})
}

func TestBuilder_StopSmeshing_failsWhenNotStarted(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)

	require.ErrorContains(t, builder.StopSmeshing(true), "not started")
}

func TestBuilder_StopSmeshing_doesNotStopOnPoSTError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)

	postSetupMock := mocks.NewMockPostSetupProvider(ctrl)
	postSetupMock.EXPECT().StartSession(gomock.Any(), gomock.Any()).Return(make(chan struct{}), nil)
	postSetupMock.EXPECT().StopSession(gomock.Any()).Return(errors.New("couldn't stop session"))

	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, postSetupMock,
		layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.initialPost = initialPost

	coinbase := types.Address{1, 1, 1}
	require.NoError(t, b.StartSmeshing(coinbase, atypes.PostSetupOpts{}))
	require.Error(t, b.StopSmeshing(true))
	require.True(t, b.Smeshing())
}

func TestBuilder_findCommitmentAtx_UsesLatestAtx(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)

	latestAtx := addPrevAtx(t, cdb, 1)
	atx, err := builder.findCommitmentAtx()
	require.NoError(t, err)
	require.Equal(t, latestAtx.ID(), atx)
}

func TestBuilder_findCommitmentAtx_DefaultsToGoldenAtx(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)

	atx, err := builder.findCommitmentAtx()
	require.NoError(t, err)
	require.Equal(t, goldenATXID, atx)
}

func TestBuilder_getCommitmentAtx_storesCommitmentAtx(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)
	builder.commitmentAtx = nil

	atx, err := builder.getCommitmentAtx(context.TODO())
	require.NoError(t, err)

	stored, err := kvstore.GetCommitmentATXForNode(cdb, builder.nodeID)
	require.NoError(t, err)

	require.Equal(t, *atx, stored)
}

func TestBuilder_getCommitmentAtx_getsStoredCommitmentAtx(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)
	builder.commitmentAtx = nil
	commitmentAtx := types.RandomATXID()

	// add a newer ATX by a different node
	newATX := addAtx(t, cdb, NewMockSigner(), &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.LayerID{Value: 1},
			},
		},
	})

	err := kvstore.AddCommitmentATXForNode(cdb, commitmentAtx, builder.nodeID)
	require.NoError(t, err)

	atx, err := builder.getCommitmentAtx(context.TODO())
	require.NoError(t, err)
	require.Equal(t, commitmentAtx, *atx)
	require.NotEqual(t, newATX.ID(), atx)
}

func TestBuilder_getCommitmentAtx_getsCommitmentAtxFromInitialAtx(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	builder := newBuilder(t, cdb, atxHdlr)
	builder.commitmentAtx = nil
	commitmentAtx := types.RandomATXID()

	// add an atx by the same node
	newATX := addAtx(t, cdb, sig, &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID:    types.LayerID{Value: 1},
				CommitmentATX: &commitmentAtx,
			},
		},
	})

	atx, err := builder.getCommitmentAtx(context.TODO())
	require.NoError(t, err)
	require.Equal(t, commitmentAtx, *atx)
	require.NotEqual(t, newATX.ID(), atx)
}

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPrevAtx))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPrevAtx, vPrevAtx, layersPerEpoch)

	// create and publish another ATX
	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	r.NoError(prevAtx.CalcAndSetID())
	vPublishedAtx, err := publishedAtx.Verify(0, 1)
	r.NoError(err)
	published, _, err = publishAtx(b, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPublishedAtx, vPublishedAtx, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx))

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, faultyNet, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.commitmentAtx = &goldenATXID
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "broadcast: failed to broadcast ATX: faulty")
	r.False(published)

	// create and attempt to publish ATX
	faultyNet.retErr = false
	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, faultyNet, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.commitmentAtx = &goldenATXID
	published, builtNipost, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.ErrorIs(err, ErrATXChallengeExpired)
	r.False(published)
	r.True(builtNipost)

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPost should be built)
	b.publisher = net
	net.atxHdlr = atxHdlr
	challenge = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(layersPerEpoch+1), nil)
	posAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))
	published, builtNipost, err = publishAtx(b, postGenesisEpoch+1, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNipost)
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx))

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	// create and attempt to publish ATX
	faultyNet := &FaultyNetMock{retErr: true}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, faultyNet, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.commitmentAtx = &goldenATXID
	published, builtNIPost, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "broadcast: failed to broadcast ATX: faulty")
	r.False(published)
	r.True(builtNIPost)

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it builds a
	// new NIPost.

	// if the network works - the ATX should be published
	b.publisher = net
	net.atxHdlr = atxHdlr
	challenge = newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(3*layersPerEpoch), nil)
	posAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))
	published, builtNIPost, err = publishAtx(b, postGenesisEpoch+3, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	r.True(builtNIPost)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPosAtx, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)
	lid := types.NewLayerID(1)
	challenge := newChallenge(1, prevAtxID, prevAtxID, lid.Add(layersPerEpoch), nil)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))

	challenge = newChallenge(0, *types.EmptyATXID, posAtx.ID(), lid, nil)
	challenge.InitialPostIndices = initialPost.Indices
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	prevAtx.InitialPost = initialPost
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPrevAtx))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPosAtx, vPrevAtx, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_TargetsEpochBasedOnPosAtx(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Sub(layersPerEpoch), nil)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))

	// create and publish ATX based on the best available posAtx, as long as the node is synced
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPosAtx, nil, layersPerEpoch)
}

func TestBuilder_PublishActivationTx_DoesNotPublish2AtxsInSameEpoch(t *testing.T) {
	r := require.New(t)

	// setup
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPrevAtx))

	// create and publish ATX
	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPrevAtx, vPrevAtx, layersPerEpoch)

	publishedAtx, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)
	vAtx, err := publishedAtx.Verify(0, 1)
	r.NoError(err)

	// assert that the next ATX is in the next epoch
	published, _, err = publishAtx(b, postGenesisEpoch, layersPerEpoch) // 👀
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vAtx, vAtx, layersPerEpoch)

	publishedAtx2, err := types.BytesToAtx(net.lastTransmission)
	r.NoError(err)

	r.Equal(publishedAtx.PubLayerID.Add(layersPerEpoch), publishedAtx2.PubLayerID)
}

func TestBuilder_PublishActivationTx_FailsWhenNIPostBuilderFails(t *testing.T) {
	r := require.New(t)

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	nipostBuilder := &NIPostErrBuilderMock{} // 👀 mock that returns error from BuildNIPost()
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.initialPost = initialPost

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))

	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.EqualError(err, "create ATX: failed to build NIPost: NIPost builder error")
	r.False(published)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)

	atx := newActivationTx(t, sig, 1, prevAtxID, prevAtxID, nil, types.NewLayerID(5), 1, 100, coinbase, 100, nipost)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), atx))

	act := newActivationTx(t, sig, 2, atx.ID(), atx.ID(), nil, atx.PubLayerID.Add(10), 0, 100, coinbase, 100, nipost)

	bt, err := codec.Encode(act)
	assert.NoError(t, err)

	a, err := types.BytesToAtx(bt)
	assert.NoError(t, err)

	bt2, err := codec.Encode(a)
	assert.NoError(t, err)

	assert.Equal(t, bt, bt2)
}

func TestBuilder_PublishActivationTx_PosAtxOnSameLayerAsPrevAtx(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr)

	for i := postGenesisEpochLayer; i.Before(postGenesisEpochLayer.Add(3)); i = i.Add(1) {
		challenge := newChallenge(1, prevAtxID, prevAtxID, i.Mul(layersPerEpoch), nil)
		atx := newAtx(t, challenge, sig, nipost, 2, coinbase)
		vAtx, err := atx.Verify(0, 1)
		r.NoError(err)
		require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vAtx))
	}

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer.Add(3).Mul(layersPerEpoch), nil)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPrevAtx))

	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)

	newAtx := lastTransmittedAtx(t)
	r.Equal(prevAtx.ID(), newAtx.PrevATXID)

	posAtx, err := cdb.GetFullAtx(newAtx.PositioningATX)
	r.NoError(err)

	assertLastAtx(r, posAtx, vPrevAtx, layersPerEpoch)

	t.Skip("proves https://github.com/spacemeshos/go-spacemesh/issues/1166")
	// check pos & prev has the same PubLayerID
	r.Equal(prevAtx.PubLayerID, posAtx.PubLayerID)
}

func TestBuilder_SignAtx(t *testing.T) {
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	sig := NewMockSigner()
	cdb := newCachedDB(t)
	receiver := mocks.NewMockatxReceiver(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, &ValidatorMock{}, receiver, logtest.New(t).WithName("atxDB1"))
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	challenge := newChallenge(1, prevAtx, prevAtx, types.NewLayerID(15), nil)
	atx := newAtx(t, challenge, sig, nipost, 100, coinbase)
	atxBytes, err := codec.Encode(&atx.InnerActivationTx)
	assert.NoError(t, err)
	err = b.SignAtx(atx)
	assert.NoError(t, err)

	pubkey, err := signing.ExtractPublicKey(atxBytes, atx.Sig)
	assert.NoError(t, err)
	assert.Equal(t, sig.NodeID().ToBytes(), []byte(pubkey))
}

func TestBuilder_NIPostPublishRecovery(t *testing.T) {
	coinbase := types.GenerateAddress([]byte("0xaaa"))
	net := &NetMock{}
	nipostBuilder := &NIPostBuilderMock{}
	layersPerEpoch := uint32(10)
	sig := NewMockSigner()
	cdb := newCachedDB(t)
	receiver := mocks.NewMockatxReceiver(gomock.NewController(t))
	atxHdlr := NewHandler(cdb, nil, layersPerEpoch, testTickSize, goldenATXID, &ValidatorMock{}, receiver, logtest.New(t).WithName("atxDB1"))
	net.atxHdlr = atxHdlr

	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, &FaultyNetMock{}, nipostBuilderMock, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.commitmentAtx = &goldenATXID

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xbe, 0xef}
	nipostBuilder.poetRef = poetRef
	npst := newNIPostWithChallenge(&chlng, poetRef)

	atx := newActivationTx(t, sig, 1, prevAtx, prevAtx, nil, types.NewLayerID(15), 1, 100, coinbase, 100, npst)

	err := atxHdlr.StoreAtx(context.TODO(), atx)
	assert.NoError(t, err)

	challenge := types.NIPostChallenge{
		Sequence:       2,
		PrevATXID:      atx.ID(),
		PubLayerID:     atx.PubLayerID.Add(b.layersPerEpoch),
		PositioningATX: atx.ID(),
	}

	challengeHash, err := challenge.Hash()
	assert.NoError(t, err)
	npst2 := newNIPostWithChallenge(challengeHash, poetRef)
	layerClockMock.currentLayer = types.EpochID(1).FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	assert.ErrorIs(t, err, ErrATXChallengeExpired)

	// test load in correct epoch
	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.commitmentAtx = &goldenATXID
	err = b.PublishActivationTx(context.TODO())
	assert.NoError(t, err)
	challenge = newChallenge(2, atx.ID(), atx.ID(), atx.PubLayerID.Add(10), nil)
	act := newAtx(t, challenge, sig, npst2, 0, coinbase)
	err = b.SignAtx(act)
	assert.NoError(t, err)

	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, &FaultyNetMock{}, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	err = b.buildNIPostChallenge(context.TODO())
	assert.NoError(t, err)
	got, err := kvstore.GetNIPostChallenge(cdb)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// test load challenge in later epoch - NIPost should be truncated
	b = NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, &FaultyNetMock{}, nipostBuilder, &postSetupProviderMock{}, layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))
	b.commitmentAtx = &goldenATXID
	err = b.loadChallenge()
	assert.NoError(t, err)
	layerClockMock.currentLayer = types.EpochID(4).FirstLayer().Add(3)
	err = b.PublishActivationTx(context.TODO())
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	assert.ErrorIs(t, err, ErrATXChallengeExpired)
	got, err = kvstore.GetNIPostChallenge(cdb)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, got)
}

func TestBuilder_RetryPublishActivationTx(t *testing.T) {
	r := require.New(t)
	bc := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	retryInterval := 10 * time.Microsecond
	expectedTries := 3

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	nipostBuilder := &NIPostBuilderMock{}
	b := NewBuilder(bc, sig.NodeID(), sig, cdb, atxHdlr, net,
		nipostBuilder, &postSetupProviderMock{}, layerClockMock,
		&mockSyncer{}, logtest.New(t).WithName("atxBuilder"),
		WithPoetRetryInterval(retryInterval),
	)
	b.initialPost = initialPost

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	posAtx := newAtx(t, challenge, otherSig, nipost, 2, coinbase)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPosAtx))

	net.lastTransmission = nil
	tries := 0
	builderConfirmation := make(chan struct{})
	// TODO(dshulyak) maybe measure time difference between attempts. It should be no less than retryInterval
	nipostBuilder.buildNIPostFunc = func(challenge *types.Hash32, commitmentAtx types.ATXID) (*types.NIPost, time.Duration, error) {
		tries++
		if tries == expectedTries {
			close(builderConfirmation)
		} else if tries < expectedTries {
			return nil, 0, ErrPoetServiceUnstable
		}
		return newNIPostWithChallenge(challenge, poetBytes), 0, nil
	}
	layerClockMock.currentLayer = types.EpochID(postGenesisEpoch).FirstLayer().Add(3)
	ctx, cancel := context.WithCancel(context.TODO())
	runnerExit := make(chan struct{})
	go func() {
		b.loop(ctx)
		close(runnerExit)
	}()
	t.Cleanup(func() {
		cancel()
		<-runnerExit
	})

	select {
	case <-builderConfirmation:
	case <-time.After(time.Second):
		require.FailNow(t, "failed waiting for required number of tries to occur")
	}
}

func TestBuilder_InitialProofGeneratedOnce(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)

	net.atxHdlr = atxHdlr
	cfg := Config{
		CoinbaseAccount: coinbase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	postSetupProvider := &postSetupProviderMock{}
	b := NewBuilder(cfg, sig.NodeID(), sig, cdb, atxHdlr, net, nipostBuilderMock, postSetupProvider,
		layerClockMock, &mockSyncer{}, logtest.New(t).WithName("atxBuilder"))

	require.NoError(t, b.generateProof(context.TODO()))
	require.Equal(t, 1, postSetupProvider.called)

	challenge := newChallenge(1, prevAtxID, prevAtxID, postGenesisEpochLayer, nil)
	prevAtx := newAtx(t, challenge, sig, nipost, 2, coinbase)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	require.NoError(t, atxHdlr.StoreAtx(context.TODO(), vPrevAtx))

	published, _, err := publishAtx(b, postGenesisEpoch, layersPerEpoch)
	r.NoError(err)
	r.True(published)
	assertLastAtx(r, vPrevAtx, vPrevAtx, layersPerEpoch)

	require.NoError(t, b.generateProof(context.TODO()))
	require.Equal(t, 1, postSetupProvider.called)
}

func TestBuilder_UpdatePoets(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr, WithPoETClientInitializer(func(string) PoetProvingServiceClient {
		poet := mocks.NewMockPoetProvingServiceClient(gomock.NewController(t))
		poet.EXPECT().PoetServiceID(gomock.Any()).Times(1).Return([]byte("poetid"), nil)
		return poet
	}))

	r.Nil(b.receivePendingPoetClients())

	err := b.UpdatePoETServers(context.TODO(), []string{"poet0", "poet1"})
	r.NoError(err)

	clients := b.receivePendingPoetClients()
	r.NotNil(clients)
	r.Len(*clients, 2)
	r.Nil(b.receivePendingPoetClients())
}

func TestBuilder_UpdatePoetsUnstable(t *testing.T) {
	r := require.New(t)

	cdb := newCachedDB(t)
	atxHdlr := newAtxHandler(t, cdb)
	b := newBuilder(t, cdb, atxHdlr, WithPoETClientInitializer(func(string) PoetProvingServiceClient {
		poet := mocks.NewMockPoetProvingServiceClient(gomock.NewController(t))
		poet.EXPECT().PoetServiceID(gomock.Any()).Times(1).Return([]byte("poetid"), errors.New("ERROR"))
		return poet
	}))

	err := b.UpdatePoETServers(context.TODO(), []string{"poet0", "poet1"})
	r.ErrorIs(err, ErrPoetServiceUnstable)
	r.Nil(b.receivePendingPoetClients())
}
