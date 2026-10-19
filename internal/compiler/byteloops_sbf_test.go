package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	byteloopvalues "gosvm/examples/byte-loop-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// byteLoopCUSource is one compare function and a Process that calls it on
// input bytes 0..31 and 32..63 and writes one result byte, so each probe's
// CU covers only that loop.
const byteLoopCUSource = `package main

import "gosvm/solana"

%s

func Process(c solana.Context) uint64 {
	in := solana.Instruction(c)
	out := solana.Data(c, 0)
	%s
	return 0
}
`

// byteLoopProbes are the CU probes: each compare shape, and none (the
// probe's fixed cost). equal and differ are the result bytes.
var byteLoopProbes = []struct {
	name, fn, call string
	equal, differ  byte
}{
	{"none", "", "out[0] = in[63]", 7 * 31 % 256, 7 * 31 % 256},
	{"array32", "func f(a, b [32]byte) byte { for i := 0; i < 32; i++ { if a[i] != b[i] { if a[i] < b[i] { return 1 }; return 2 } }; return 3 }",
		"var x, y [32]byte; copy(x[:], in[:32]); copy(y[:], in[32:]); out[0] = f(x, y)", 3, 1},
	{"slice32", "func f(a, b []byte) bool { for i := 0; i < 32; i++ { if a[i] != b[i] { return false } }; return true }",
		"if f(in[:32], in[32:]) { out[0] = 1 }", 1, 0},
	{"pointer32", "func f(a []byte, b *[32]byte) bool { for i := 0; i < 32; i++ { if a[i] != b[i] { return false } }; return true }",
		"var y [32]byte; copy(y[:], in[32:]); if f(in[:32], &y) { out[0] = 1 }", 1, 0},
	{"slice8", "func f(a, b []byte) bool { for i := 0; i < 8; i++ { if a[i] != b[i] { return false } }; return true }",
		"if f(in[:32], in[32:]) { out[0] = 1 }", 1, 0},
	{"slice13", "func f(a, b []byte) bool { for i := 0; i < 13; i++ { if a[i] != b[i] { return false } }; return true }",
		"if f(in[:32], in[32:]) { out[0] = 1 }", 1, 0},
}

// TestByteLoopSBF builds examples/byte-loop-values for fogo-v0 and solana-v3
// and runs its vectors through the pinned runners: completions must leave
// native Go's output bytes, traps (short operands, nil pointers) must abort
// and roll back. It records each mode's CU with equal full-length operands
// and with the first and the last byte different, and per-shape CU probes
// (byteLoopProbes). Skips without the v1.57
// backend or a runner (make test). GOSVM_BYTELOOP_RESULTS=<dir> keeps the
// run.
func TestByteLoopSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	vectors := ByteLoopVectorsForTest()
	cases, aborts := comboCases("byteloops", vectors, byteloopvalues.Run, 0, nil, nil)
	source := comboSBFSource(t, byteLoopSource(t), "byteloopvalues")
	// byteLoopVectors starts each mode with the equal vector, then byte 0
	// flipped at bit 0 and bit 7, and so on to byte 31.
	perMode := len(vectors) / byteLoopModes
	keep := os.Getenv("GOSVM_BYTELOOP_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			// A fixture suite holds at most 1,024 cases.
			result := map[string]any{}
			for j := 0; j < len(cases); j += 1024 {
				part := runAllocProgram(t, llvm, runner, profile, fmt.Sprintf("byte-loops-%d", j/1024), source, 0, cases[j:min(j+1024, len(cases))], keep)
				for name, v := range part {
					result[name] = v
				}
			}
			modes := map[string]any{}
			for mode := 0; mode < byteLoopModes; mode++ {
				at := func(j int) any { return result[cases[mode*perMode+j].name] }
				modes[fmt.Sprint(mode)] = map[string]any{"equal": at(0), "first_differs": at(1), "last_differs": at(64)}
			}
			probes := map[string]any{}
			for _, p := range byteLoopProbes {
				src := fmt.Sprintf(byteLoopCUSource, p.fn, p.call)
				equal := make([]byte, 64)
				for j := range equal {
					equal[j] = byte(j % 32 * 7)
				}
				differ := append([]byte(nil), equal...)
				differ[32] ^= 1
				out := make([]byte, 1024)
				out[0] = p.equal
				differOut := make([]byte, 1024)
				differOut[0] = p.differ
				cu := runAllocProgram(t, llvm, runner, profile, "byte-loop-cu-"+p.name, []byte(src), 0, []allocCase{
					{name: "equal", in: equal, output: out}, {name: "first-differs", in: differ, output: differOut}}, keep)
				probes[p.name] = map[string]any{"equal": cu["equal"], "first_differs": cu["first-differs"]}
			}
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases), "aborts": aborts, "cu": modes, "probes": probes}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match native Go\n%s", profile, len(cases), aborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
