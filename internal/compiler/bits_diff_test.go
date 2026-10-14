package compiler

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math/bits"
	"math/rand"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// bitsOp is one differential operation: Go source for a switch case that
// stores up to two results, and the same computation with the host's real
// math/bits as the oracle. A panicking oracle means the generated C must trap.
type bitsOp struct {
	name   string
	source string
	ref    func(a, b, c uint64) (uint64, uint64)
	args   int // 1..3 meaningful inputs, for vector generation
	width  int // 64, 32 or 8 bits per input
}

var bitsRotations = []int64{-1 << 63, -1000, -129, -128, -65, -64, -63, -33, -32, -31, -9, -8, -7, -1, 0, 1, 7, 8, 9, 31, 32, 33, 63, 64, 65, 127, 128, 1000, 1<<63 - 1}

func bitsOps() []bitsOp {
	two := func(name, call string, ref func(a, b, c uint64) (uint64, uint64), args, width int) bitsOp {
		return bitsOp{name, "r0, r1 := " + call + "\nstore(o, 0, uint64(r0))\nstore(o, 8, uint64(r1))", ref, args, width}
	}
	one := func(name, value string, ref func(a, b, c uint64) (uint64, uint64), args, width int) bitsOp {
		return bitsOp{name, "store(o, 0, uint64(" + value + "))", ref, args, width}
	}
	ops := []bitsOp{
		two("Add64", "bits.Add64(a, b, c)", func(a, b, c uint64) (uint64, uint64) { return bits.Add64(a, b, c) }, 3, 64),
		two("Sub64", "bits.Sub64(a, b, c)", func(a, b, c uint64) (uint64, uint64) { return bits.Sub64(a, b, c) }, 3, 64),
		two("Mul64", "bits.Mul64(a, b)", func(a, b, c uint64) (uint64, uint64) { return bits.Mul64(a, b) }, 2, 64),
		two("Div64", "bits.Div64(a, b, c)", func(a, b, c uint64) (uint64, uint64) { return bits.Div64(a, b, c) }, 3, 64),
		one("Rem64", "bits.Rem64(a, b, c)", func(a, b, c uint64) (uint64, uint64) { return bits.Rem64(a, b, c), 0 }, 3, 64),
		two("Add32", "bits.Add32(uint32(a), uint32(b), uint32(c))", func(a, b, c uint64) (uint64, uint64) {
			x, y := bits.Add32(uint32(a), uint32(b), uint32(c))
			return uint64(x), uint64(y)
		}, 3, 32),
		two("Sub32", "bits.Sub32(uint32(a), uint32(b), uint32(c))", func(a, b, c uint64) (uint64, uint64) {
			x, y := bits.Sub32(uint32(a), uint32(b), uint32(c))
			return uint64(x), uint64(y)
		}, 3, 32),
		two("Mul32", "bits.Mul32(uint32(a), uint32(b))", func(a, b, c uint64) (uint64, uint64) {
			x, y := bits.Mul32(uint32(a), uint32(b))
			return uint64(x), uint64(y)
		}, 2, 32),
		two("Div32", "bits.Div32(uint32(a), uint32(b), uint32(c))", func(a, b, c uint64) (uint64, uint64) {
			x, y := bits.Div32(uint32(a), uint32(b), uint32(c))
			return uint64(x), uint64(y)
		}, 3, 32),
		one("Rem32", "bits.Rem32(uint32(a), uint32(b), uint32(c))", func(a, b, c uint64) (uint64, uint64) {
			return uint64(bits.Rem32(uint32(a), uint32(b), uint32(c))), 0
		}, 3, 32),
		one("LeadingZeros64", "bits.LeadingZeros64(a)", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.LeadingZeros64(a)), 0 }, 1, 64),
		one("LeadingZeros32", "bits.LeadingZeros32(uint32(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.LeadingZeros32(uint32(a))), 0 }, 1, 32),
		one("LeadingZeros8", "bits.LeadingZeros8(uint8(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.LeadingZeros8(uint8(a))), 0 }, 1, 8),
		one("TrailingZeros64", "bits.TrailingZeros64(a)", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.TrailingZeros64(a)), 0 }, 1, 64),
		one("TrailingZeros32", "bits.TrailingZeros32(uint32(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.TrailingZeros32(uint32(a))), 0 }, 1, 32),
		one("TrailingZeros8", "bits.TrailingZeros8(uint8(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.TrailingZeros8(uint8(a))), 0 }, 1, 8),
		one("OnesCount64", "bits.OnesCount64(a)", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.OnesCount64(a)), 0 }, 1, 64),
		one("OnesCount32", "bits.OnesCount32(uint32(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.OnesCount32(uint32(a))), 0 }, 1, 32),
		one("OnesCount8", "bits.OnesCount8(uint8(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.OnesCount8(uint8(a))), 0 }, 1, 8),
		one("Len64", "bits.Len64(a)", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.Len64(a)), 0 }, 1, 64),
		one("Len32", "bits.Len32(uint32(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.Len32(uint32(a))), 0 }, 1, 32),
		one("Len8", "bits.Len8(uint8(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.Len8(uint8(a))), 0 }, 1, 8),
		one("Reverse64", "bits.Reverse64(a)", func(a, b, c uint64) (uint64, uint64) { return bits.Reverse64(a), 0 }, 1, 64),
		one("Reverse32", "bits.Reverse32(uint32(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.Reverse32(uint32(a))), 0 }, 1, 32),
		one("Reverse8", "bits.Reverse8(uint8(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.Reverse8(uint8(a))), 0 }, 1, 8),
		one("ReverseBytes64", "bits.ReverseBytes64(a)", func(a, b, c uint64) (uint64, uint64) { return bits.ReverseBytes64(a), 0 }, 1, 64),
		one("ReverseBytes32", "bits.ReverseBytes32(uint32(a))", func(a, b, c uint64) (uint64, uint64) { return uint64(bits.ReverseBytes32(uint32(a))), 0 }, 1, 32),
		// Results used in place, as len's are: comparison, shift count, switch tag.
		one("int results", "cmp(bits.Len64(a) > bits.TrailingZeros32(uint32(b))) + a<<bits.OnesCount8(uint8(c)) + tag(a)", func(a, b, c uint64) (uint64, uint64) {
			v := a << bits.OnesCount8(uint8(c))
			if bits.Len64(a) > bits.TrailingZeros32(uint32(b)) {
				v++
			}
			switch bits.LeadingZeros64(a) {
			case 0, 64:
				v += 5
			}
			return v, 0
		}, 3, 64),
	}
	for _, k := range bitsRotations {
		k := k
		ops = append(ops,
			one(fmt.Sprintf("RotateLeft64(%d)", k), fmt.Sprintf("bits.RotateLeft64(a, %d)", k), func(a, b, c uint64) (uint64, uint64) { return bits.RotateLeft64(a, int(k)), 0 }, 1, 64),
			one(fmt.Sprintf("RotateLeft32(%d)", k), fmt.Sprintf("bits.RotateLeft32(uint32(a), %d)", k), func(a, b, c uint64) (uint64, uint64) { return uint64(bits.RotateLeft32(uint32(a), int(k))), 0 }, 1, 32),
			one(fmt.Sprintf("RotateLeft8(%d)", k), fmt.Sprintf("bits.RotateLeft8(uint8(a), %d)", k), func(a, b, c uint64) (uint64, uint64) { return uint64(bits.RotateLeft8(uint8(a), int(k))), 0 }, 1, 8),
		)
	}
	return ops
}

func bitsDifferentialProgram(ops []bitsOp) string {
	var b strings.Builder
	b.WriteString(`package main

import "math/bits"

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
func cmp(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}
func tag(a uint64) uint64 {
	switch bits.LeadingZeros64(a) {
	case 0, 64:
		return 5
	}
	return 0
}

// Input: op (2 bytes), then a, b, c. Output: two result words.
func Process(i, o []byte) uint64 {
	op := uint64(i[0]) | uint64(i[1])<<8
	a, b, c := load(i, 2), load(i, 10), load(i, 18)
	_, _, _ = a, b, c
	switch op {
`)
	for n, op := range ops {
		fmt.Fprintf(&b, "\tcase %d: // %s\n%s\n", n, op.name, op.source)
	}
	b.WriteString("\t}\n\treturn 0\n}\n")
	return b.String()
}

// The driver continues after a trap: abort longjmps back and prints "P".
const bitsHostDriver = `
#include <setjmp.h>
#include <stdio.h>
#include <stdlib.h>
static jmp_buf bits_trap;
void abort(void) { longjmp(bits_trap, 1); }
static u8 nibble(char c){return c<='9'?c-'0':c-'a'+10;}
int main(void) {
    char line[64]; u8 in[26], out[16];
    while (scanf("%52s", line) == 1) {
        for (int j = 0; j < 26; j++) in[j] = (nibble(line[j*2])<<4)|nibble(line[j*2+1]);
        for (int j = 0; j < 16; j++) out[j] = 0;
        if (setjmp(bits_trap)) { puts("P"); continue; }
        go_Process(GOSVM_SLICE(in, 26), GOSVM_SLICE(out, 16));
        u64 r0 = 0, r1 = 0;
        for (int j = 7; j >= 0; j--) { r0 = r0<<8 | out[j]; r1 = r1<<8 | out[8+j]; }
        printf("%016llx %016llx\n", r0, r1);
    }
    return 0;
}`

func bitsReference(op bitsOp, a, b, c uint64) (out string) {
	defer func() {
		if recover() != nil {
			out = "P"
		}
	}()
	r0, r1 := op.ref(a, b, c)
	return fmt.Sprintf("%016x %016x", r0, r1)
}

// bitsEdges are boundary values of a width: 0, 1, max, powers of two and
// their neighbours, alternating patterns.
func bitsEdges(width int) []uint64 {
	mask := ^uint64(0) >> (64 - width)
	set := map[uint64]bool{}
	for _, v := range []uint64{0, 1, 2, 3, mask, mask - 1, mask >> 1, mask>>1 + 1, 0x5555555555555555 & mask, 0xaaaaaaaaaaaaaaaa & mask, 0x0123456789abcdef & mask} {
		set[v&mask] = true
	}
	for s := 0; s < width; s++ {
		set[uint64(1)<<s] = true
		set[(uint64(1)<<s-1)&mask] = true
		set[(uint64(1)<<s+1)&mask] = true
	}
	var out []uint64
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestBitsDifferentialAgainstGo(t *testing.T) {
	ops := bitsOps()
	program := bitsDifferentialProgram(ops)
	bin := hostProgram(t, program, bitsHostDriver)
	rng := rand.New(rand.NewSource(20261014))
	type vector struct {
		op      int
		a, b, c uint64
	}
	var vectors []vector
	intrinsicCarry := runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	small := []uint64{0, 1, 2, 3, ^uint64(0), ^uint64(0) - 1, 1 << 63, 1 << 32}
	for n, op := range ops {
		mask := ^uint64(0) >> (64 - op.width)
		edges := bitsEdges(op.width)
		random := func() uint64 {
			switch rng.Intn(4) {
			case 0:
				return rng.Uint64() & mask
			case 1:
				return (rng.Uint64() >> uint(rng.Intn(op.width))) & mask
			case 2:
				return edges[rng.Intn(len(edges))]
			}
			return (rng.Uint64() & mask) >> uint(rng.Intn(op.width))
		}
		add := func(a, b, c uint64) {
			// Add64/Sub64 carries other than 0/1 are undefined by the package
			// documentation; the compiled oracle treats them as nonzero=1 on
			// amd64/arm64, which goSVM matches.
			if !intrinsicCarry && (op.name == "Add64" || op.name == "Sub64") && c > 1 {
				return
			}
			vectors = append(vectors, vector{n, a, b, c})
		}
		switch op.args {
		case 1:
			for _, a := range edges {
				add(a, 0, 0)
			}
			for j := 0; j < 2000; j++ {
				add(random(), 0, 0)
			}
		case 2:
			for _, a := range edges {
				for _, b := range edges {
					add(a, b, 0)
				}
			}
			for j := 0; j < 20000; j++ {
				add(random(), random(), 0)
			}
		case 3:
			core := []uint64{0, 1, 2, 3, mask, mask - 1, mask >> 1, mask>>1 + 1, 0x5555555555555555 & mask, 0xaaaaaaaaaaaaaaaa & mask, 1<<(op.width/2) - 1, 1 << (op.width / 2), 1<<(op.width/2) + 1}
			if strings.HasPrefix(op.name, "Div") || strings.HasPrefix(op.name, "Rem") {
				// Every divisor edge y against lo edges, with hi = y-1 (largest
				// valid), hi = y (overflow), hi = y+1, hi = 0, and y = 0, 1.
				for _, y := range edges {
					for _, lo := range core {
						add((y-1)&mask, lo, y)
						add(y, lo, y)
						add((y+1)&mask, lo, y)
						add(0, lo, y)
						add(y, lo, 0)
						add(y, lo, 1)
						add(lo, y, mask)
					}
				}
			} else {
				// Sums and differences: all core pairs with carries 0, 1, 2,
				// max and other values beyond the documented 0/1.
				carries := append([]uint64{0, 1, 2, 3, mask, mask - 1, 1 << (op.width - 1)}, small...)
				for _, a := range core {
					for _, b := range core {
						for _, c := range carries {
							add(a, b, c&mask)
						}
					}
				}
			}
			for j := 0; j < 20000; j++ {
				a, b, c := random(), random(), random()
				if strings.HasPrefix(op.name, "Div") || strings.HasPrefix(op.name, "Rem") {
					switch j % 4 {
					case 0: // valid: hi < y
						if c == 0 {
							c = 1
						}
						a %= c
					case 1: // largest valid hi
						if c == 0 {
							c = mask
						}
						a = c - 1
					case 2: // divisor with its top bit set (no normalization shift)
						c |= 1 << (op.width - 1)
						a %= c
					}
				}
				add(a, b, c)
			}
		}
	}
	var input, want strings.Builder
	panics := 0
	for _, v := range vectors {
		in := make([]byte, 26)
		binary.LittleEndian.PutUint16(in, uint16(v.op))
		binary.LittleEndian.PutUint64(in[2:], v.a)
		binary.LittleEndian.PutUint64(in[10:], v.b)
		binary.LittleEndian.PutUint64(in[18:], v.c)
		fmt.Fprintf(&input, "%x\n", in)
		expected := bitsReference(ops[v.op], v.a, v.b, v.c)
		if expected == "P" {
			panics++
		}
		want.WriteString(expected + "\n")
	}
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input.String())
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("host binary: %v", err)
	}
	gotLines := bufio.NewScanner(strings.NewReader(string(got)))
	wantLines := strings.Split(strings.TrimSuffix(want.String(), "\n"), "\n")
	mismatches := 0
	for j := 0; gotLines.Scan(); j++ {
		if j >= len(wantLines) {
			t.Fatalf("extra output line %q", gotLines.Text())
		}
		if gotLines.Text() != wantLines[j] {
			v := vectors[j]
			if mismatches++; mismatches < 20 {
				t.Errorf("%s(%#x, %#x, %#x): C %s, Go %s", ops[v.op].name, v.a, v.b, v.c, gotLines.Text(), wantLines[j])
			}
		}
		if j == len(wantLines)-1 && mismatches == 0 {
			t.Logf("%d vectors over %d operations agree with math/bits (%d traps where Go panics)", len(vectors), len(ops), panics)
			return
		}
	}
	if mismatches == 0 {
		t.Fatalf("output truncated")
	}
}
