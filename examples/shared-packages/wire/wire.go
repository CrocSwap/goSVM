// Package wire uses explicit little-endian layouts, independent of Go/C memory.
package wire

import "example.com/shared-app/model"

const PoolBytes uint64 = 24
const SwapBytes uint64 = 16

func read(s []byte, off uint64) uint64 {
	n := uint64(0)
	for j := uint64(0); j < 8; j++ {
		n = n | uint64(s[off+j])<<(j*8)
	}
	return n
}
func write(s []byte, off, n uint64) {
	for j := uint64(0); j < 8; j++ {
		s[off+j] = byte(n >> (j * 8))
	}
}

// Callers validate exact lengths before decoding or encoding.
func DecodePool(s []byte) model.Pool {
	return model.Pool{ReserveX: model.Amount(read(s, 0)), ReserveY: model.Amount(read(s, 8)), Sequence: read(s, 16)}
}
func EncodePool(s []byte, p model.Pool) {
	write(s, 0, uint64(p.ReserveX))
	write(s, 8, uint64(p.ReserveY))
	write(s, 16, p.Sequence)
}
func DecodeSwap(s []byte) model.SwapArgs {
	return model.SwapArgs{AmountIn: model.Amount(read(s, 0)), MinOut: model.Amount(read(s, 8))}
}
func EncodeSwap(s []byte, a model.SwapArgs) {
	write(s, 0, uint64(a.AmountIn))
	write(s, 8, uint64(a.MinOut))
}
