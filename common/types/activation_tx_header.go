package types

// ActivationTxHeader is the header of an activation transaction. It includes all fields from the NIPostChallenge, as
// well as the coinbase address and total weight.
type ActivationTxHeader struct {
	NIPostChallenge
	Coinbase Address

	// NumUnits holds the count of space units that have been reserved by the node for the
	// current epoch; a unit represents a configurable amount of data for PoST
	NumUnits uint32

	ID     ATXID  // the ID of the ATX
	NodeID NodeID // the id of the Node that created the ATX (public key)

	BaseTickHeight uint64

	// TickCount number of ticks performed by PoET; a tick represents a number of sequential
	// hashes
	TickCount uint64
}

// GetWeight of the ATX. The total weight of the epoch is expected to fit in a uint64 and is
// sum(atx.NumUnits * atx.TickCount for each ATX in a given epoch).
// Space Units sizes are chosen such that NumUnits for all ATXs in an epoch is expected to be < 10^9.
// PoETs should produce ~10k ticks at genesis, but are expected due to technological advances
// to produce more over time. A uint64 should be large enough to hold the total weight of an epoch,
// for at least the first few years.
func (atxh *ActivationTxHeader) GetWeight() uint64 {
	return getWeight(uint64(atxh.NumUnits), atxh.TickCount)
}

func getWeight(numUnits, tickCount uint64) uint64 {
	return safeMul(numUnits, tickCount)
}

func safeMul(a, b uint64) uint64 {
	c := a * b
	if a > 1 && b > 1 && c/b != a {
		panic("uint64 overflow")
	}
	return c
}

// TickHeight returns a sum of base tick height and tick count.
func (atxh *ActivationTxHeader) TickHeight() uint64 {
	return atxh.BaseTickHeight + atxh.TickCount
}
