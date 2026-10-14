// Package main is the math/bits mulDiv CU comparison (TestBitsSBF and
// results/compiler/2026-10-14-math-bits): bitsprobe without the vector
// dispatcher, so each mulDiv has exactly one call site, as in full-swap.
// Account 0 holds 48-byte records: op (unused), a, b, c and r0, r1. The
// instruction is three little-endian words: first record, record count, mode.
package main

import (
	"gosvm/solana"
	"math/bits"
)

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

// mulDivHand is examples/full-swap/model/quote.go's mulDiv, unchanged: adapted
// from Go's math/bits Mul64 and Div64 (Go Authors, BSD-3-Clause; see
// THIRD_PARTY_NOTICES and third_party/Go-LICENSE).
func mulDivHand(a, b, d uint64) uint64 {
	const mask = 4294967295
	a0 := a & mask
	a1 := a >> 32
	b0 := b & mask
	b1 := b >> 32
	w0 := a0 * b0
	t := a1*b0 + (w0 >> 32)
	w1 := (t & mask) + a0*b1
	hi := a1*b1 + (t >> 32) + (w1 >> 32)
	lo := a * b
	if d == 0 || hi >= d {
		return 0
	}
	if hi == 0 {
		return lo / d
	}
	s := uint64(0)
	for d&9223372036854775808 == 0 {
		d = d << 1
		s++
	}
	const base = 4294967296
	d1 := d >> 32
	d0 := d & mask
	u32 := hi<<s | lo>>(64-s)
	u10 := lo << s
	u1 := u10 >> 32
	u0 := u10 & mask
	q1 := u32 / d1
	r := u32 - q1*d1
	for q1 >= base || q1*d0 > base*r+u1 {
		q1--
		r = r + d1
		if r >= base {
			break
		}
	}
	u21 := u32*base + u1 - q1*d
	q0 := u21 / d1
	r = u21 - q0*d1
	for q0 >= base || q0*d0 > base*r+u0 {
		q0--
		r = r + d1
		if r >= base {
			break
		}
	}
	return q1*base + q0
}

// mulDivBits is the same contract written with math/bits.
func mulDivBits(a, b, d uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if d == 0 || hi >= d {
		return 0
	}
	q, _ := bits.Div64(hi, lo, d)
	return q
}

// Modes: 1 hand-written mulDiv(a, b, c); 2 math/bits
// mulDiv(a, b, c); 3 the same loop with no multiply/divide (CU baseline).
func Process(c solana.Context) uint64 {
	d := solana.Data(c, 0)
	ix := solana.Instruction(c)
	first, count, mode := load(ix, 0), load(ix, 8), load(ix, 16)
	for j := first; j < first+count; j++ {
		o := j * record
		switch mode {
		case 1:
			store(d, o+32, mulDivHand(load(d, o+8), load(d, o+16), load(d, o+24)))
		case 2:
			store(d, o+32, mulDivBits(load(d, o+8), load(d, o+16), load(d, o+24)))
		default:
			store(d, o+32, load(d, o+8)^load(d, o+16)^load(d, o+24))
		}
	}
	return 0
}
