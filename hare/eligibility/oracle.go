package eligibility

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/hashicorp/golang-lru"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

const vrfMsgCacheSize = 20 // numRounds per layer is <= 2. numConcurrentLayers<=10 (typically <=2) so numRounds*numConcurrentLayers <= 2*10 = 20 is a good upper bound
const activesCacheSize = 5 // we don't expect to handle more than two layers concurrently

var (
	errGenesis            = errors.New("no data about active nodes for genesis")
	errNoContextualBlocks = errors.New("no contextually valid blocks")
)

type valueProvider interface {
	Value(layer types.LayerID) (uint32, error)
}

// a func to retrieve the active set size for the provided layer
// this func is assumed to be cpu intensive and hence we cache its results
type activeSetFunc func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error)

type signer interface {
	Sign(msg []byte) ([]byte, error)
}

type goodBlocksProvider interface {
	ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error)
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(msg, sig, pub []byte) (bool, error)

// Oracle is the hare eligibility oracle
type Oracle struct {
	lock               sync.Mutex
	beacon             valueProvider
	getActiveSet       activeSetFunc
	vrfSigner          signer
	vrfVerifier        verifierFunc
	layersPerEpoch     uint16
	vrfMsgCache        addGet
	activesCache       addGet
	genesisTotalWeight uint64
	blocksProvider     goodBlocksProvider
	cfg                eCfg.Config
	log.Log
}

// Returns the relative layer id that w.h.p. we have agreement on its contextually valid blocks
// safe layer is defined to be the confidence param layers prior to the provided Layer
func safeLayer(layer types.LayerID, safetyParam types.LayerID) types.LayerID {
	if layer <= safetyParam { // assuming genesis is zero
		return config.Genesis
	}

	return layer - safetyParam
}

func roundedSafeLayer(layer types.LayerID, safetyParam types.LayerID,
	layersPerEpoch uint16, epochOffset types.LayerID) types.LayerID {

	sl := safeLayer(layer, safetyParam)
	if sl == config.Genesis {
		return config.Genesis
	}

	se := types.LayerID(sl.GetEpoch(layersPerEpoch)) // the safe epoch

	roundedLayer := se*types.LayerID(layersPerEpoch) + epochOffset
	if sl >= roundedLayer { // the safe layer is after the rounding threshold
		return roundedLayer // round to threshold
	}

	if roundedLayer <= types.LayerID(layersPerEpoch) { // we can't go before genesis
		return config.Genesis // just return genesis
	}

	// round to the previous epoch threshold
	return roundedLayer - types.LayerID(layersPerEpoch)
}

// New returns a new eligibility oracle instance.
func New(beacon valueProvider, activeSetFunc activeSetFunc, vrfVerifier verifierFunc, vrfSigner signer,
	layersPerEpoch uint16, genesisTotalWeight uint64, goodBlocksProvider goodBlocksProvider,
	cfg eCfg.Config, log log.Log) *Oracle {
	vmc, e := lru.New(vrfMsgCacheSize)
	if e != nil {
		log.Panic("Could not create lru cache err=%v", e)
	}

	ac, e := lru.New(activesCacheSize)
	if e != nil {
		log.Panic("Could not create lru cache err=%v", e)
	}

	return &Oracle{
		beacon:             beacon,
		getActiveSet:       activeSetFunc,
		vrfVerifier:        vrfVerifier,
		vrfSigner:          vrfSigner,
		layersPerEpoch:     layersPerEpoch,
		vrfMsgCache:        vmc,
		activesCache:       ac,
		genesisTotalWeight: genesisTotalWeight,
		blocksProvider:     goodBlocksProvider,
		cfg:                cfg,
		Log:                log,
	}
}

type vrfMessage struct {
	Beacon uint32
	Round  int32
	Layer  types.LayerID
}

func buildKey(l types.LayerID, r int32) [2]uint64 {
	return [2]uint64{uint64(l), uint64(r)}
}

// buildVRFMessage builds the VRF message used as input for the BLS (msg=Beacon##Layer##Round)
func (o *Oracle) buildVRFMessage(layer types.LayerID, round int32) ([]byte, error) {
	key := buildKey(layer, round)

	o.lock.Lock()

	// check cache
	if val, exist := o.vrfMsgCache.Get(key); exist {
		o.lock.Unlock()
		return val.([]byte), nil
	}

	// get value from Beacon
	v, err := o.beacon.Value(layer)
	if err != nil {
		o.With().Error("Could not get hare Beacon value", log.Err(err), layer, log.Int32("round", round))
		o.lock.Unlock()
		return nil, err
	}

	// marshal message
	var w bytes.Buffer
	msg := vrfMessage{Beacon: v, Round: round, Layer: layer}
	_, err = xdr.Marshal(&w, &msg)
	if err != nil {
		o.With().Error("Fatal: could not marshal xdr", log.Err(err))
		o.lock.Unlock()
		return nil, err
	}

	val := w.Bytes()
	o.vrfMsgCache.Add(key, val) // update cache

	o.lock.Unlock()
	return val, nil
}

func (o *Oracle) totalWeight(layer types.LayerID) (uint64, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return o.genesisTotalWeight, nil
		}

		o.With().Error("totalWeight erred while calling actives func", log.Err(err), layer)
		return 0, err
	}

	var totalWeight uint64
	for _, w := range actives {
		totalWeight += w
	}
	return totalWeight, nil
}

func (o *Oracle) minerWeight(layer types.LayerID, id string) (uint64, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return o.genesisTotalWeight, nil
		}

		o.With().Error("totalWeight erred while calling actives func", log.Err(err), layer)
		return 0, err
	}

	w, ok := actives[id]
	if !ok {
		return 0, errors.New("miner is not active in specified layer")
	}
	return w, nil
}

func calcVrfFrac(sig []byte) fixed.Fixed {
	sha := sha256.Sum256(sig)
	return fixed.FracFromBytes(sha[:8])
}

func (o *Oracle) prepareEligibilityCheck(layer types.LayerID, round int32, sig []byte, id types.NodeID) (uint64, uint64, bool, error) {
	msg, err := o.buildVRFMessage(layer, round)
	if err != nil {
		o.Error("eligibility: could not build VRF message")
		return 0, 0, true, err
	}

	// validate message
	res, err := o.vrfVerifier(msg, sig, id.VRFPublicKey)
	if err != nil {
		o.Error("eligibility: VRF verification failed: %v", err)
		return 0, 0, true, err
	}
	if !res {
		o.With().Info("eligibility: a node did not pass VRF signature verification",
			id,
			layer)
		return 0, 0, true, nil
	}

	// get active set size
	totalWeight, err := o.totalWeight(layer)
	if err != nil {
		return 0, 0, true, err
	}

	// require totalWeight > 0
	if totalWeight == 0 {
		o.Warning("eligibility: total weight is zero")
		return 0, 0, true, errors.New("total weight is zero")
	}

	// calc hash & check threshold
	minerWeight, err := o.minerWeight(layer, string(id.VRFPublicKey))
	if err != nil {
		return 0, 0, true, err
	}
	return totalWeight, minerWeight, false, nil
}

// Validate validates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is the role
// proof and assuming commSize as the expected committee size.
func (o *Oracle) Validate(layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte, eligibilityCount uint16) (bool, error) {
	totalWeight, minerWeight, done, err := o.prepareEligibilityCheck(layer, round, sig, id)
	if done {
		return false, err
	}
	if uint64(int(minerWeight)) != minerWeight {
		return false, fmt.Errorf("minerWeight overflows int (%d)", minerWeight)
	}
	n := fixed.New(int(minerWeight))
	p := fixed.DivUint64(uint64(committeeSize), totalWeight)
	vrfFrac := calcVrfFrac(sig)

	x := fixed.New(int(eligibilityCount))
	if !fixed.BinCDF(n, p, x.Sub(fixed.One)).GreaterThan(vrfFrac) && vrfFrac.LessThan(fixed.BinCDF(n, p, x)) {
		return true, nil
	}
	o.With().Info("eligibility: node did not pass VRF eligibility threshold",
		id,
		log.Int("committee_size", committeeSize),
		log.Uint64("total_weight", totalWeight),
		log.Int32("round", round),
		layer)
	return false, nil
}

// CalcEligibility calculates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is
// the role proof and assuming commSize as the expected committee size.
func (o *Oracle) CalcEligibility(layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (uint16, error) {
	totalWeight, minerWeight, done, err := o.prepareEligibilityCheck(layer, round, sig, id)
	if done {
		return 0, err
	}
	if uint64(int(minerWeight)) != minerWeight {
		return 0, fmt.Errorf("minerWeight overflows int (%d)", minerWeight)
	}
	n := fixed.New(int(minerWeight))
	p := fixed.DivUint64(uint64(committeeSize), totalWeight)
	vrfFrac := calcVrfFrac(sig)

	one := fixed.One
	for x := fixed.New(0); x.Value() < n.Value(); x = x.Add(one) {
		if fixed.BinCDF(n, p, x).GreaterThan(vrfFrac) {
			return uint16(x.Floor()), nil
		}
	}
	return uint16(n.Floor()), nil
}

// Proof returns the role proof for the current Layer & Round
func (o *Oracle) Proof(layer types.LayerID, round int32) ([]byte, error) {
	msg, err := o.buildVRFMessage(layer, round)
	if err != nil {
		o.Error("Proof: could not build VRF message err=%v", err)
		return nil, err
	}

	sig, err := o.vrfSigner.Sign(msg)
	if err != nil {
		o.Error("Proof: could not sign VRF message err=%v", err)
		return nil, err
	}

	return sig, nil
}

// Returns a map of all active nodes in the specified layer id
func (o *Oracle) actives(layer types.LayerID) (map[string]uint64, error) {
	sl := roundedSafeLayer(layer, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	safeEp := sl.GetEpoch(o.layersPerEpoch)

	// check genesis
	if safeEp.IsGenesis() {
		return nil, errGenesis
	}

	// lock until any return
	// note: no need to lock per safeEp - we do not expect many concurrent requests per safeEp (max two)
	o.lock.Lock()

	// check cache
	if val, exist := o.activesCache.Get(safeEp); exist {
		o.lock.Unlock()
		return val.(map[string]uint64), nil
	}

	// build a map of all blocks on the current layer
	mp, err := o.blocksProvider.ContextuallyValidBlock(sl)
	if err != nil {
		o.lock.Unlock()
		return nil, err
	}

	// no contextually valid blocks
	if len(mp) == 0 {
		o.With().Error("Could not calculate hare active set size: no contextually valid blocks",
			layer, layer.GetEpoch(o.layersPerEpoch),
			log.FieldNamed("safe_layer_id", sl), log.FieldNamed("safe_epoch_id", safeEp))
		o.lock.Unlock()
		return nil, errNoContextualBlocks
	}

	activeMap, err := o.getActiveSet(safeEp, mp)
	if err != nil {
		o.With().Error("Could not retrieve active set size", log.Err(err), layer, layer.GetEpoch(o.layersPerEpoch),
			log.FieldNamed("safe_layer_id", sl), log.FieldNamed("safe_epoch_id", safeEp))
		o.lock.Unlock()
		return nil, err
	}

	// update
	o.activesCache.Add(safeEp, activeMap)

	o.lock.Unlock()
	return activeMap, nil
}

// IsIdentityActiveOnConsensusView returns true if the provided identity is active on the consensus view derived
// from the specified layer, false otherwise.
func (o *Oracle) IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return true, nil // all ids are active in genesis
		}

		o.With().Error("IsIdentityActiveOnConsensusView erred while calling actives func", layer, log.Err(err))
		return false, err
	}
	_, exist := actives[edID]

	return exist, nil
}
