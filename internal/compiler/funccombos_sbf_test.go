package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	funcmaps "gosvm/examples/func-maps"
	funcstrings "gosvm/examples/func-strings"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// comboSBFSource gives a combination fixture a Context entrypoint: account 0
// is the 1 KiB output and the instruction data is Run's input.
func comboSBFSource(t *testing.T, source []byte, pkg string) []byte {
	s := strings.Replace(string(source), "package "+pkg+"\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(s, entry) {
		t.Fatalf("%s entrypoint changed", pkg)
	}
	return []byte(strings.Replace(s, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1))
}

// comboUnits is the compute unit limit of every combination transaction:
// the heap boundaries run thousands of closures or deferred calls.
const comboUnits = 1_400_000

// comboCases runs run natively on each vector (a panic is an abort with
// rollback) and adds each heap boundary: the last value that fits
// completes with native Go's output, the next aborts out of memory.
func comboCases(prefix string, vectors [][]byte, run func(out, in []byte) uint64, kinds int, limit func(kind byte) int, input func(kind byte, n int) []byte) ([]allocCase, int) {
	var cases []allocCase
	aborts := 0
	native := func(name string, in []byte) allocCase {
		c := allocCase{name: name, in: in, units: comboUnits, err: abortError, log: abortLog}
		out := make([]byte, 1024)
		failed := func() (failed bool) {
			defer func() { failed = recover() != nil }()
			run(out, append([]byte(nil), in...))
			return false
		}()
		if failed {
			aborts++
		} else {
			c.output = out
		}
		return c
	}
	for j, in := range vectors {
		cases = append(cases, native(fmt.Sprintf("%s-%03d-mode%d", prefix, j, in[0]), in))
	}
	for kind := byte(0); kind < byte(kinds); kind++ {
		n := limit(kind)
		if n < 0 {
			cases = append(cases, native(fmt.Sprintf("envs-kind%d-5000-fits", kind), input(kind, 5000)))
			continue
		}
		cases = append(cases, native(fmt.Sprintf("envs-kind%d-%d-fits", kind, n), input(kind, n)),
			allocCase{name: fmt.Sprintf("envs-kind%d-%d-oom", kind, n+1), in: input(kind, n+1), units: comboUnits, err: abortError, log: abortLog})
		aborts++
	}
	return cases, aborts
}

func funcStringLimit(kind byte) int {
	if funcStringBytes[kind] == 0 {
		return -1
	}
	return (DefaultHeapBytes - 8 - funcStringUpfront) / funcStringBytes[kind]
}

// TestFuncCombinationsSBF builds the combination fixtures
// examples/func-strings (function values with strings and errors) and
// examples/func-maps (function values with maps) for fogo-v0 and solana-v3
// with the default 32 KiB heap and runs their vectors and exact heap
// boundaries through the pinned runners: completions must leave native Go's
// output bytes; panics and out-of-memory must abort and roll back. Skips
// without the v1.57 backend or a runner (make test). GOSVM_COMBO_RESULTS=<dir>
// keeps fixtures, ELFs, reports and a summary.
func TestFuncCombinationsSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	stringCases, stringAborts := comboCases("funcstrings", funcStringVectors(20261016, 40), funcstrings.Run, 6, funcStringLimit, funcStringInput)
	mapCases, mapAborts := comboCases("funcmaps", FuncMapVectorsForTest(), funcmaps.Run, 5, funcMapLimit, funcMapInput)
	stringSource := comboSBFSource(t, funcStringSource(t), "funcstrings")
	mapSource := comboSBFSource(t, funcMapSource(t), "funcmaps")
	keep := os.Getenv("GOSVM_COMBO_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			strs := runAllocProgram(t, llvm, runner, profile, "func-strings", stringSource, 0, stringCases, keep)
			maps := runAllocProgram(t, llvm, runner, profile, "func-maps", mapSource, 0, mapCases, keep)
			summary := map[string]any{
				"profile":                 profile,
				"func_strings_elf_sha256": strs["elf_sha256"], "func_strings_cases": len(stringCases), "func_strings_aborts": stringAborts,
				"func_maps_elf_sha256": maps["elf_sha256"], "func_maps_cases": len(mapCases), "func_maps_aborts": mapAborts,
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: func-strings %d cases (%d aborts with rollback), func-maps %d cases (%d aborts with rollback) match native Go and the heap model\n%s",
				profile, len(stringCases), stringAborts, len(mapCases), mapAborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
