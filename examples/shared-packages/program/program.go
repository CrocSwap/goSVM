// Package program exercises imports through the legacy bounded starter ABI.
// It models arithmetic/state updates only; there are no token transfers here.
package program

import (
	"example.com/shared-app/model"
	"example.com/shared-app/quote"
	"example.com/shared-app/wire"
)

func Process(s, i []byte) uint64 {
	if uint64(len(s)) != wire.PoolBytes || uint64(len(i)) != wire.SwapBytes {
		return model.ErrInput
	}
	pool := wire.DecodePool(s)
	args := wire.DecodeSwap(i)
	if !quote.Valid(pool, args) {
		return model.ErrInput
	}
	out := quote.Quote(pool, args)
	if out < args.MinOut {
		return model.ErrSlippage
	}
	key := model.PublicKey{0: byte(pool.Sequence)}
	marked := quote.Mark(key, byte(pool.Sequence+1))
	if marked[0] != key[0] || key[31] != 0 {
		return model.ErrInput
	}
	pool.ReserveX = pool.ReserveX + args.AmountIn
	pool.ReserveY = pool.ReserveY - out
	pool.Sequence = pool.Sequence + 1
	wire.EncodePool(s, pool)
	return 0
}
