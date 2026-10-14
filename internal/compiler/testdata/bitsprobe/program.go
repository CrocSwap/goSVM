// Package main is the math/bits SBF probe (TestBitsSBF and
// results/compiler/2026-10-14-math-bits). Account 0 holds 48-byte records:
// op, a, b, c (inputs) and r0, r1 (outputs written by the program). The
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

// vector applies one math/bits function selected by the record's op.
func vector(d []byte, o uint64) {
	op, a, b, c := load(d, o), load(d, o+8), load(d, o+16), load(d, o+24)
	var r0, r1 uint64
	switch op {
	case 0:
		r0, r1 = bits.Mul64(a, b)
	case 1:
		r0, r1 = bits.Div64(a, b, c)
	case 2:
		r0, r1 = bits.Add64(a, b, c)
	case 3:
		r0 = uint64(bits.LeadingZeros64(a))
	case 4:
		r0, r1 = bits.Sub64(a, b, c)
	case 5:
		r0 = bits.Rem64(a, b, c)
	case 6:
		q, r := bits.Div32(uint32(a), uint32(b), uint32(c))
		r0, r1 = uint64(q), uint64(r)
	case 7:
		r0 = uint64(bits.Rem32(uint32(a), uint32(b), uint32(c)))
	case 8:
		r0, r1 = uint64(bits.TrailingZeros64(a)), uint64(bits.OnesCount64(a))
	case 9:
		r0, r1 = bits.Reverse64(a), bits.RotateLeft64(a, -3)
	case 10:
		s, carry := bits.Add32(uint32(a), uint32(b), uint32(c))
		h, l := bits.Mul32(uint32(a), uint32(b))
		r0, r1 = uint64(s)|uint64(carry)<<32, uint64(h)<<32|uint64(l)
	}
	store(d, o+32, r0)
	store(d, o+40, r1)
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

// Modes: 0 math/bits vectors; 1 hand-written mulDiv(a, b, c); 2 math/bits
// mulDiv(a, b, c); 3 the same loop with no multiply/divide (CU baseline).
func Process(c solana.Context) uint64 {
	d := solana.Data(c, 0)
	ix := solana.Instruction(c)
	first, count, mode := load(ix, 0), load(ix, 8), load(ix, 16)
	for j := first; j < first+count; j++ {
		o := j * record
		switch mode {
		case 0:
			vector(d, o)
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
