package multisig

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/registry"
)

func TestKeysLimits(t *testing.T) {
	reg := registry.New()
	Register(reg)

	for i, address := range []core.Address{TemplateAddress1, TemplateAddress2, TemplateAddress3} {
		expectedK := i + 1
		t.Run(strconv.Itoa(expectedK), func(t *testing.T) {
			for n := 0; n < StorageLimit+5; n++ {
				t.Run(strconv.Itoa(n), func(t *testing.T) {
					handler := reg.Get(address)
					args := SpawnArguments{PublicKeys: make([]core.PublicKey, n)}
					_, err := handler.New(&args)
					if n < expectedK || n > StorageLimit {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}
