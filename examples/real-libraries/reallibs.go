// Package reallibs calls real third-party Go libraries — holiman/uint256
// v1.3.1, KyberNetwork/int256 v0.1.4, the uniswapv3-sdk-uint256 v0.5.5 math
// (package utils) and lukechampine.com/uint128 v1.3.0 — on operands taken
// from a 256-byte vector and writes every result (values, flags, error
// identities and texts, strings and byte slices) to a 1 KiB output.
//
// Natively it imports the unmodified modules from the module cache (go.mod
// requires them; nothing is replaced). On chain, goSVM compiles the same
// source against trimmed copies of the same modules: the upstream sources
// minus the declarations goSVM cannot compile (trim/trim.json; every kept
// declaration is byte-identical). The differential and SBF tests in
// internal/compiler (reallibs_*_test.go) compare the two byte for byte.
//
// Vector layout: in[0] group, in[1] operation, in[2] text length (mod 65)
// and in[3] flags; in[8:16], in[16:24] and in[24:32] are 64-bit
// little-endian parameters n0, n1 and n2; in[32:192] are five 256-bit
// operands a, b, c, d and e (four little-endian 64-bit words each, least
// significant first), and in[192:256] is text. A first byte of Idle
// returns at once.
package reallibs

import (
	"errors"

	"github.com/KyberNetwork/int256"
	"github.com/holiman/uint256"
)

// W writes values into out, dropping what does not fit.
type W struct {
	out []byte
	o   int
}

func (w *W) u8(v byte) {
	if w.o < len(w.out) {
		w.out[w.o] = v
		w.o++
	}
}

func (w *W) u64(v uint64) {
	for j := 0; j < 8; j++ {
		w.u8(byte(v >> (8 * j)))
	}
}

func (w *W) int(v int) { w.u64(uint64(v)) }

func (w *W) flag(b bool) {
	if b {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

// u256 writes the four words of z, or 0xdd for a nil pointer.
func (w *W) u256(z *uint256.Int) {
	if z == nil {
		w.u8(0xdd)
		return
	}
	for j := 0; j < 4; j++ {
		w.u64(z[j])
	}
}

func (w *W) i256(z *int256.Int) {
	if z == nil {
		w.u8(0xdd)
		return
	}
	for j := 0; j < 4; j++ {
		w.u64(z[j])
	}
}

// str writes the length (two bytes) and then the bytes of s.
func (w *W) str(s string) {
	w.u8(byte(len(s)))
	w.u8(byte(len(s) >> 8))
	for j := 0; j < len(s); j++ {
		w.u8(s[j])
	}
}

// bs writes a byte slice: nil-ness, length, capacity and bytes.
func (w *W) bs(b []byte) {
	w.flag(b == nil)
	w.int(len(b))
	w.int(cap(b))
	for _, c := range b {
		w.u8(c)
	}
}

// text writes a byte slice made by a string conversion ([]byte(s)):
// nil-ness, length and bytes, not its capacity. gc gives such a slice
// capacity len(s) when its escape analysis proves the bytes are never
// written (Go 1.22's zero-copy conversion; it depends on inlining), and
// the runtime's size-class capacity otherwise; goSVM always gives the
// latter (docs/GO_STRINGS.md#allocation).
func (w *W) text(b []byte) {
	w.flag(b == nil)
	w.int(len(b))
	for _, c := range b {
		w.u8(c)
	}
}

// err writes whether err is nil, and otherwise which of the libraries'
// sentinel errors it is (errors.Is, by index; 0xff for none) and its text.
func (w *W) err(err error) {
	if err == nil {
		w.u8(0xee)
		return
	}
	w.u8(1)
	id := byte(0xff)
	for j, s := range sentinels() {
		if errors.Is(err, s) {
			id = byte(j)
			break
		}
	}
	w.u8(id)
	w.str(err.Error())
}

func le64(b []byte) uint64 {
	var v uint64
	for j := 7; j >= 0; j-- {
		v = v<<8 | uint64(b[j])
	}
	return v
}

// Input holds the decoded vector.
type Input struct {
	op         int
	flags      byte
	n0, n1, n2 uint64
	a, b, c, d uint256.Int
	e          uint256.Int
	text       string
	raw        []byte
}

func word(in []byte, o int) uint256.Int {
	return uint256.Int{le64(in[o:]), le64(in[o+8:]), le64(in[o+16:]), le64(in[o+24:])}
}

// Groups Run dispatches on.
const Groups = 8

// Idle is the first byte of a vector Run returns for at once: on chain, the
// invocation then only initializes the packages.
const Idle = 0xff

func Process(s, i []byte) uint64 { return Run(s, i) }

// Run decodes the vector and runs one operation of one group.
func Run(out, in []byte) uint64 {
	if in[0] == Idle {
		return 0
	}
	w := &W{out: out}
	n := int(in[2] % 65)
	v := Input{op: int(in[1]), flags: in[3], n0: le64(in[8:]), n1: le64(in[16:]), n2: le64(in[24:]),
		a: word(in, 32), b: word(in, 64), c: word(in, 96), d: word(in, 128), e: word(in, 160),
		text: string(in[192 : 192+n]), raw: in}
	switch in[0] % Groups {
	case 0:
		u256Arith(w, &v)
	case 1:
		u256Unary(w, &v)
	case 2:
		u256Text(w, &v)
	case 3:
		i256Group(w, &v)
	case 4:
		tickGroup(w, &v)
	case 5:
		mathGroup(w, &v)
	case 6:
		u128Group(w, &v)
	case 7:
		benchGroup(w, &v)
	}
	return uint64(w.o)
}
