// Package main is the signed-integer SBF probe (TestIntValuesSBF). It is
// built together with examples/int-values/ops.go, whose package clause the
// test rewrites to main. Account 0 holds 48-byte records in the math/bits
// probe layout: code, a, b, c (inputs) and r0, r1; the program writes
// r0 = Eval(code, a, b). The instruction is three little-endian words: first
// record, record count, mode (unused).
package main

import "gosvm/solana"

const record = 48

func load(b []byte, o uint64) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(b[o+j])<<(8*j)
	}
	return v
}

func store(b []byte, o uint64, v uint64) {
	for j := uint64(0); j < 8; j++ {
		b[o+j] = byte(v >> (8 * j))
	}
}

func Process(c solana.Context) uint64 {
	d := solana.Data(c, 0)
	ix := solana.Instruction(c)
	first, count := load(ix, 0), load(ix, 8)
	for j := first; j < first+count; j++ {
		o := j * record
		store(d, o+32, Eval(load(d, o), load(d, o+8), load(d, o+16)))
	}
	return 0
}
