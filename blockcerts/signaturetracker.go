package blockcerts

import (
    "context"
    "fmt"
    "sync"

    certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
    "github.com/spacemeshos/go-spacemesh/common/types"
    "github.com/spacemeshos/go-spacemesh/log"
)

type SigCacher interface {
    CacheBlockSignature(ctx context.Context,
        blockSignature certtypes.BlockSignatureMsg)
}

type sigCache struct {
    logger             log.Logger
    blockSigsByLayer   sync.Map // entries are written once but read many times
    cacheBoundary      types.LayerID
    cacheBoundaryMutex sync.RWMutex
    completedCertsCh   chan<- certtypes.BlockCertificate
}

func (c *sigCache) CacheBlockSignature(ctx context.Context, sigMsg certtypes.BlockSignatureMsg) {
    // TODO: decide on how to make this concurrent without asking the use to manage it
    logger := c.logger.WithContext(ctx)
    c.cacheBoundaryMutex.RLock()
    logger.Debug("CacheBlockSignature: acquired Rlock on the cache boundary")
    defer c.cacheBoundaryMutex.RUnlock()
    if sigMsg.LayerID.Before(c.cacheBoundary) {
        logger.Error("block msg is from layer %d which is older "+
            "than cache boundary %d", sigMsg.LayerID.Value, c.cacheBoundary.Value)
        return
    }
    tracker, loaded := c.blockSigsByLayer.LoadOrStore(sigMsg.LayerID.Value,
        newBlockSignatureTracker(sigMsg.LayerID, c.completedCertsCh, logger)) // TODO: fix memory issue (newBlockSig... called every time even if loading)
    if !loaded {
        logger.Debug("newBlockSignatureTracker: created tracker for block "+
            "signatures for layer %v", sigMsg.LayerID.String())
    }
    cert := tracker.(*blockSignatureTracker)
    cert.addSig(sigMsg)
}

// updateCacheBoundary updates the layer boundary. Block signatures aren't
// cached from before boundary. The cache limit should follow hdist.
func (c *sigCache) updateCacheBoundary(layer types.LayerID) error {
    c.cacheBoundaryMutex.Lock()
    defer c.cacheBoundaryMutex.Unlock()
    // The layer boundary for the cache should only move forward.
    if layer.Value <= c.cacheBoundary.Value {
        err := fmt.Errorf("new trackableBlockSignature cache limit is equal to or " +
            "older than previous cacheBoundary")
        c.logger.Error(err.Error())
        return err
    }
    go c.cleanup(c.cacheBoundary) // cleanup starting from old cacheBoundary
    c.cacheBoundary = layer
    return nil
}

func (c *sigCache) cleanup(startingAt types.LayerID) {
    start := startingAt.Value
    c.cacheBoundaryMutex.RLock()
    end := c.cacheBoundary.Value
    c.cacheBoundaryMutex.RUnlock()
    for layer := start; layer < end; layer++ {
        c.blockSigsByLayer.Delete(layer)
    }
}

type trackableBlockSignature struct {
    BlockID types.BlockID
    certtypes.BlockSignature
}
type blockSignatureTracker struct {
    layerID        types.LayerID
    signatures     map[types.NodeID]trackableBlockSignature
    thresholdCount map[types.BlockID]int
    sync.Mutex
    completedCerts chan<- certtypes.BlockCertificate
    ifNotAlready   sync.Once // completedCerts channel only gets one cert
    logger         log.Logger
}

func newBlockSignatureTracker( // did I forget to pass in the threshold?
    layerID types.LayerID, completedCerts chan<- certtypes.BlockCertificate, logger log.Logger) *blockSignatureTracker {
    newTracker := blockSignatureTracker{
        layerID:        layerID,
        signatures:     map[types.NodeID]trackableBlockSignature{},
        thresholdCount: map[types.BlockID]int{},
        Mutex:          sync.Mutex{},
        completedCerts: completedCerts,
        ifNotAlready:   sync.Once{},
        logger:         logger,
    }

    return &newTracker
}

// addSig doesn't do any validation or verification and is a blocking operation.
// It should be run in its own goroutine.
func (t *blockSignatureTracker) addSig(sig certtypes.BlockSignatureMsg) {
    t.logger.Debug("addSig entry")
    t.Lock()
    t.logger.Debug("addSig: acquired Write Lock on self (sigTracker)")
    defer t.Unlock()
    // check if a BlockID reached threshold # of signatures
    var majorityBlockID types.BlockID
    var thresholdReached = false
    for blockID, threshold := range t.thresholdCount {
        if len(t.signatures) == threshold { // TODO: fix this, shouldn't be counting signatures here. Should depend on threshold count map
            majorityBlockID = blockID
            thresholdReached = true
            break
        } else if len(t.signatures) > threshold {
            panic("block certificate signatures: addSig atomicity" +
                "was not maintained.")
        }
    }
    if thresholdReached {
        t.ifNotAlready.Do(func() {
            var sigs []certtypes.BlockSignature
            for _, sig := range t.signatures {
                if sig.BlockID.Compare(majorityBlockID) {
                    sigs = append(sigs, sig.BlockSignature)
                }
            }
            blockCert := certtypes.BlockCertificate{
                BlockID:               majorityBlockID,
                TerminationSignatures: sigs,
            }
            t.completedCerts <- blockCert
        })
        return
    }

    t.signatures[sig.SignerNodeID] = trackableBlockSignature{
        BlockID:        sig.BlockID,
        BlockSignature: sig.BlockSignature,
    }
    t.thresholdCount[sig.BlockID] += int(sig.SignerCommitteeSeats)
}
