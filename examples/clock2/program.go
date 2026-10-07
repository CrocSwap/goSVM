// Package clock2 exercises the actual SDK-2 Clock accessor, not a supplied time.
package clock2

import "gosvm/solana"

func read(data []byte, off uint64) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(data[off+j])<<(j*8)
	}
	return v
}
func write(data []byte, off, value uint64) {
	for j := uint64(0); j < 8; j++ {
		data[off+j] = byte(value >> (j * 8))
	}
}
func Process(c solana.Context) uint64 {
	if solana.Count(c) != 1 {
		return 4000
	}
	ix, data := solana.Instruction(c), solana.Data(c, 0)
	if len(ix) != 9 || len(data) != 56 || !solana.Writable(c, 0) {
		return 4000
	}
	if ix[0] == 2 {
		return solana.Clock(c, data[1:40])
	}
	if ix[0] == 3 {
		return solana.Clock(c, data[1:42])
	}
	var clock [40]byte
	code := solana.Clock(c, clock[:])
	if code != 0 {
		return code
	}
	now, previous := read(clock[:], 32), read(data, 40)
	if now>>63 != 0 {
		return 6021
	}
	if now < previous {
		return 6022
	}
	delta, rate := now-previous, read(ix, 1)
	if rate != 0 && delta > 18446744073709551615/rate {
		delta = 0
	} else {
		delta = delta * rate
	}
	for j := uint64(0); j < 40; j++ {
		data[j] = clock[j]
	}
	write(data, 40, now)
	write(data, 48, delta)
	if ix[0] == 1 {
		return 6099
	}
	return 0
}
