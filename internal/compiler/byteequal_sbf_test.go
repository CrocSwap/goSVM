package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// byteEqualSource compares two arrays built from the input with ==, for
// lengths 8, 13, 20, 32 and 64 of byte and of int8, and for [32]bool and
// [8]uint16, which keep the element loop. Input: [kind][len a bytes][len b
// bytes]. Account 0 gets 1 when they're equal and 2 when not, plus the
// struct comparison (a field array next to a scalar) in byte 1.
const byteEqualSource = `package main

import "gosvm/solana"

type keyed struct {
	Tag uint16
	Key [32]byte
}

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	k, d := in[0], in[1:]
	eq := false
	switch k {
	case 0:
		var a, b [8]byte
		copy(a[:], d)
		copy(b[:], d[8:])
		eq = a == b
	case 1:
		var a, b [13]byte
		copy(a[:], d)
		copy(b[:], d[13:])
		eq = a == b
	case 2:
		var a, b [20]byte
		copy(a[:], d)
		copy(b[:], d[20:])
		eq = a == b
	case 3:
		var a, b [32]byte
		copy(a[:], d)
		copy(b[:], d[32:])
		eq = a == b
		x, y := keyed{Tag: 7, Key: a}, keyed{Tag: 7, Key: b}
		if x == y {
			out[1] = 1
		} else {
			out[1] = 2
		}
	case 4:
		var a, b [64]byte
		copy(a[:], d)
		copy(b[:], d[64:])
		eq = a == b
	case 5:
		var a, b [13]int8
		for i := range a {
			a[i], b[i] = int8(d[i]), int8(d[13+i])
		}
		eq = a == b
	case 6:
		var a, b [32]int8
		for i := range a {
			a[i], b[i] = int8(d[i]), int8(d[32+i])
		}
		eq = a == b
	case 7:
		var a, b [32]bool
		for i := range a {
			a[i], b[i] = d[i] != 0, d[32+i] != 0
		}
		eq = a == b
	case 8:
		var a, b [8]uint16
		for i := range a {
			a[i], b[i] = uint16(d[2*i])|uint16(d[2*i+1])<<8, uint16(d[16+2*i])|uint16(d[17+2*i])<<8
		}
		eq = a == b
	}
	if eq {
		out[0] = 1
	} else {
		out[0] = 2
	}
	return 0
}
`

var byteEqualLens = []int{8, 13, 20, 32, 64, 13, 32, 32, 16}

// byteEqualCases: equal pairs, then one bit flipped (low and high) at every
// position of b, for every kind.
func byteEqualCases() []allocCase {
	var cases []allocCase
	for kind, n := range byteEqualLens {
		a := make([]byte, n)
		for i := range a {
			a[i] = byte(i*37 + kind + 1)
		}
		if kind == 7 {
			for i := range a {
				a[i] &= 1
			}
		}
		add := func(name string, b []byte, equal bool) {
			out := make([]byte, 1024)
			out[0] = 2
			if equal {
				out[0] = 1
			}
			if kind == 3 {
				out[1] = out[0]
			}
			in := append(append([]byte{byte(kind)}, a...), b...)
			cases = append(cases, allocCase{name: fmt.Sprintf("kind%d-len%d-%s", kind, n, name), in: in, output: out})
		}
		add("equal", append([]byte(nil), a...), true)
		for i := 0; i < n; i++ {
			for _, bit := range []byte{1, 0x80} {
				if kind == 7 && bit == 0x80 {
					continue // bools are 0 or 1
				}
				b := append([]byte(nil), a...)
				b[i] ^= bit
				add(fmt.Sprintf("diff%02d-%02x", i, bit), b, false)
			}
		}
	}
	return cases
}

// TestByteArrayEqualSBF runs byteEqualSource on fogo-v0 and solana-v3: every
// comparison must match Go. GOSVM_BYTEEQ_RESULTS=<dir> keeps the run with
// each case's CU.
func TestByteArrayEqualSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases := byteEqualCases()
	keep := os.Getenv("GOSVM_BYTEEQ_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "byte-equal", []byte(byteEqualSource), 0, cases, keep)
			equalCU := map[string]any{}
			for name, v := range result {
				if strings.HasSuffix(name, "-equal") {
					equalCU[name] = v
				}
			}
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases), "equal_cu": equalCU}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d comparisons match Go\n%s", profile, len(cases), encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestByteArrayEqualC checks which arrays get the word comparison: one-byte
// integer elements and at least 8 of them, on SBF only.
func TestByteArrayEqualC(t *testing.T) {
	src := `package main
import "gosvm/solana"
func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	var a, b [32]byte
	var e, f [7]byte
	var g, h [32]bool
	var i, j [8]uint16
	copy(a[:], in)
	e[0], g[0], i[0] = in[0], in[1] != 0, uint16(in[2])
	if a == b || e == f || g == h || i == j {
		return 1
	}
	return 0
}
`
	c, err := Compile("program.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Count(string(c), "__builtin_memcpy(&x,a.items+i,8)")
	if words != 1 {
		t.Fatalf("%d word comparisons, want 1 ([32]byte only)\n", words)
	}
}
