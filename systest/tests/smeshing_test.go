package tests

import (
	"bytes"
	"sort"
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestSmeshing(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	t.Run("Proposals", func(t *testing.T) {
		t.Parallel()
		testSmeshing(t, tctx, cl)
	})
	t.Run("Transactions", func(t *testing.T) {
		t.Parallel()
		testTransactions(t, tctx, cl)
	})
}

func testSmeshing(t *testing.T, tctx *testcontext.Context, cl *cluster.Cluster) {
	const limit = 15

	first := currentLayer(tctx, t, cl.Client(0))
	// TODO fetch epoch size from API
	first = nextFirstLayer(first, 4)
	last := first + limit
	tctx.Log.Debugw("watching layer between", "first", first, "last", last)

	createdch := make(chan *spacemeshv1.Proposal, cl.Total()*(limit+1))
	includedAll := make([]map[uint32][]*spacemeshv1.Proposal, cl.Total())
	for i := 0; i < cl.Total(); i++ {
		includedAll[i] = map[uint32][]*spacemeshv1.Proposal{}
	}

	eg, ctx := errgroup.WithContext(tctx)
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		tctx.Log.Debugw("watching", "client", client.Name, "i", i)
		watchProposals(ctx, eg, cl.Client(i), func(proposal *spacemeshv1.Proposal) (bool, error) {
			if proposal.Layer.Number < first {
				return true, nil
			}
			tctx.Log.Debugw("received proposal event",
				"client", client.Name,
				"layer", proposal.Layer.Number,
				"smesher", prettyHex(proposal.Smesher.Id),
				"eligibilities", len(proposal.Eligibilities),
				"status", spacemeshv1.Proposal_Status_name[int32(proposal.Status)],
			)
			if proposal.Layer.Number > last {
				return false, nil
			}
			if proposal.Status == spacemeshv1.Proposal_Created {
				createdch <- proposal
			} else {
				includedAll[i][proposal.Layer.Number] = append(includedAll[i][proposal.Layer.Number], proposal)
			}
			return true, nil
		})
	}

	require.NoError(t, eg.Wait())
	close(createdch)

	created := map[uint32][]*spacemeshv1.Proposal{}
	beacons := map[uint64]map[string]struct{}{}
	for proposal := range createdch {
		created[proposal.Layer.Number] = append(created[proposal.Layer.Number], proposal)
		if edata := proposal.GetData(); edata != nil {
			if _, exist := beacons[proposal.Epoch.Value]; !exist {
				beacons[proposal.Epoch.Value] = map[string]struct{}{}
			}
			beacons[proposal.Epoch.Value][prettyHex(edata.Beacon)] = struct{}{}
		}
	}
	requireEqualEligibilities(t, created)
	requireEqualProposals(t, created, includedAll)
	for epoch := range beacons {
		require.Len(t, beacons[epoch], 1, "epoch=%d", epoch)
	}
}

func requireEqualProposals(tb testing.TB, reference map[uint32][]*spacemeshv1.Proposal, received []map[uint32][]*spacemeshv1.Proposal) {
	tb.Helper()
	for layer := range reference {
		sort.Slice(reference[layer], func(i, j int) bool {
			return bytes.Compare(reference[layer][i].Smesher.Id, reference[layer][j].Smesher.Id) == -1
		})
	}
	for i, included := range received {
		for layer := range included {
			sort.Slice(included[layer], func(i, j int) bool {
				return bytes.Compare(included[layer][i].Smesher.Id, included[layer][j].Smesher.Id) == -1
			})
		}
		for layer, proposals := range reference {
			require.Len(tb, included[layer], len(proposals), "client=%d layer=%d", i, layer)
			for i := range proposals {
				assert.Equal(tb, proposals[i].Id, included[layer][i].Id, "client=%d layer=%d", i, layer)
			}
		}
	}
}

func requireEqualEligibilities(tb testing.TB, proposals map[uint32][]*spacemeshv1.Proposal) {
	tb.Helper()

	aggregated := map[string]int{}
	for _, perlayer := range proposals {
		for _, proposal := range perlayer {
			aggregated[string(proposal.Smesher.Id)] += len(proposal.Eligibilities)
		}
	}
	referenceEligibilities := -1
	for smesher, eligibilities := range aggregated {
		if referenceEligibilities < 0 {
			referenceEligibilities = eligibilities
		} else {
			assert.Equal(tb, referenceEligibilities, eligibilities, prettyHex([]byte(smesher)))
		}
	}
}
