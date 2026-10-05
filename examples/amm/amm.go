// Package amm is a bounded constant-product arithmetic experiment, not a token AMM.
package amm

// Process swaps X for Y with a 30bps fee. State is reserveX, reserveY, swapCount;
// instruction is amountIn, minAmountOut, all little-endian uint64.
// Caps deliberately keep intermediate arithmetic inside uint64.
func Process(state []byte, instruction []byte) uint64 {
	if len(state) != 24 || len(instruction) != 16 {
		return 1
	}
	x := load64(state, 0)
	y := load64(state, 8)
	n := load64(state, 16)
	amount := load64(instruction, 0)
	minimum := load64(instruction, 8)
	if x == 0 || y == 0 || x > 1000000000 || y > 1000000000 {
		return 2
	}
	if amount == 0 || amount > 1000000 || x+amount > 1000000000 {
		return 3
	}
	if n == 18446744073709551615 {
		return 4
	}
	effective := amount * 9970
	out := y * effective / (x*10000 + effective)
	if out == 0 || out < minimum {
		return 5
	}
	store64(state, 0, x+amount)
	store64(state, 8, y-out)
	store64(state, 16, n+1)
	return 0
}

func load64(data []byte, offset uint64) uint64 {
	var value uint64
	for i := uint64(0); i < 8; i++ {
		value = value | (uint64(data[offset+i]) << (i * 8))
	}
	return value
}

func store64(data []byte, offset uint64, value uint64) {
	for i := uint64(0); i < 8; i++ {
		data[offset+i] = byte(value >> (i * 8))
	}
}
