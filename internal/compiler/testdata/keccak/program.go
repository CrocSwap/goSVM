// Package program exercises solana.Keccak256. Account 0 holds the input;
// account 1 receives status:u64 followed by the output buffer. Instruction:
// offset, length and output offset, each u64le.
package program

import "gosvm/solana"

func read(b []byte, off uint64) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(b[off+j])<<(j*8)
	}
	return v
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 24 || solana.Count(c) != 2 {
		return 1
	}
	result := solana.Data(c, 1)
	if len(result) < 8 {
		return 1
	}
	code := solana.Keccak256(c, solana.Data(c, 0), read(ix, 0), read(ix, 8), result[8:], read(ix, 16))
	for j := uint64(0); j < 8; j++ {
		result[j] = byte(code >> (j * 8))
	}
	return 0
}
