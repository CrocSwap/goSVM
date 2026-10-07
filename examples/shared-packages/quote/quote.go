// Package quote is compiled on chain and called directly by native Go services.
package quote

import (
	arithmetic "example.com/shared-app/math"
	"example.com/shared-app/model"
)

func Valid(p model.Pool, a model.SwapArgs) bool {
	return p.ReserveX != 0 && p.ReserveY != 0 && a.AmountIn != 0 && p.ReserveX <= model.MaxReserve && p.ReserveY <= model.MaxReserve && a.AmountIn <= model.MaxReserve-p.ReserveX && p.Sequence != ^uint64(0)
}
func Quote(p model.Pool, a model.SwapArgs) model.Amount {
	if !Valid(p, a) {
		return 0
	}
	return model.Amount(arithmetic.MulDiv(uint64(p.ReserveY), uint64(a.AmountIn), uint64(p.ReserveX+a.AmountIn)))
}

// Mark preserves value semantics for the same key type on both sides.
func Mark(key model.PublicKey, value byte) model.PublicKey {
	copy := key
	copy[31] = value
	return copy
}
