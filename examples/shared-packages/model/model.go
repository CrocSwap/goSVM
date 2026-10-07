// Package model defines canonical types for on-chain code and ordinary Go clients.
package model

type PublicKey [32]byte
type Amount uint64

type Pool struct {
	ReserveX Amount
	ReserveY Amount
	Sequence uint64
}
type SwapArgs struct {
	AmountIn Amount
	MinOut   Amount
}

const MaxReserve Amount = 1000000000
const ErrInput uint64 = 7
const ErrSlippage uint64 = 9
