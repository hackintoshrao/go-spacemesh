package types

import "github.com/spacemeshos/go-scale"

// Account represents account state at a certain layer.
type Account struct {
	Layer       LayerID
	Address     Address
	Initialized bool
	Nonce       uint64
	Balance     uint64
	Template    *scale.Address
	State       []byte
}
