package blocks

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type meshProvider interface {
	AddBlockWithTXs(context.Context, *types.Block) error
	ProcessLayerPerHareOutput(context.Context, types.LayerID, types.BlockID) error
}

type conservativeState interface {
	SelectBlockTXs(types.LayerID, []*types.Proposal) ([]types.TransactionID, error)
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
}

type certifier interface {
	RegisterForCert(context.Context, types.LayerID, types.BlockID) error
	CertifyIfEligible(context.Context, log.Log, types.LayerID, types.BlockID) error
}
