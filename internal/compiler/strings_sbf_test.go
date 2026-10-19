package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	stringvalues "gosvm/examples/string-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// stringSBFSource is examples/string-values with a Context entrypoint:
// account 0 is the 1 KiB output and the instruction data is Run's input.
func stringSBFSource(t *testing.T) []byte {
	source := string(stringSource(t))
	source = strings.Replace(source, "package stringvalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("string-values entrypoint changed")
	}
	// Run returns the bytes written; the on-chain entrypoint returns 0.
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1))
}

// heapUnits is the compute unit limit of the heap boundary transactions:
// thousands of allocations exceed the default 200,000.
const heapUnits = 1_400_000

// stringSBFCases are the vector cases (native expectations; a panic is an
// abort with rollback) and the heap boundaries of every allocating kind.
func stringSBFCases() ([]allocCase, int) {
	var cases []allocCase
	aborts := 0
	native := func(name string, in []byte) allocCase {
		c := allocCase{name: name, in: in, err: abortError, log: abortLog}
		if in[0] == 8 {
			c.units = heapUnits
		}
		out := make([]byte, 1024)
		failed := func() (failed bool) {
			defer func() { failed = recover() != nil }()
			stringvalues.Run(out, append([]byte(nil), in...)) // Run may write its input
			return false
		}()
		if failed {
			aborts++
		} else {
			c.output = out
		}
		return c
	}
	for j, in := range StringVectorsForTest() {
		cases = append(cases, native(fmt.Sprintf("string-%03d-mode%d", j, in[0]), in))
	}
	for kind := byte(0); kind < 8; kind++ {
		for _, size := range [][2]int{{5, 3}, {32, 32}} {
			la, lb := size[0], size[1]
			limit := stringHeapLimit(kind, la, lb)
			if limit < 0 {
				cases = append(cases, native(fmt.Sprintf("heap-kind%d-%d-%d-6000-fits", kind, la, lb), stringHeapInput(kind, 6000, la, lb)))
				continue
			}
			cases = append(cases, native(fmt.Sprintf("heap-kind%d-%d-%d-%d-fits", kind, la, lb, limit), stringHeapInput(kind, limit, la, lb)),
				allocCase{name: fmt.Sprintf("heap-kind%d-%d-%d-%d-oom", kind, la, lb, limit+1), in: stringHeapInput(kind, limit+1, la, lb), units: heapUnits, err: abortError, log: abortLog})
			aborts++
		}
	}
	return cases, aborts
}

// TestStringSBF builds examples/string-values for fogo-v0 and solana-v3 with
// the default 32 KiB heap and runs the vectors and heap boundaries through
// the pinned runners: completions must leave native Go's output bytes;
// panics and out-of-memory must abort and roll back. Skips without the v1.57
// backend or a runner (make test). GOSVM_STRING_RESULTS=<dir> keeps
// fixtures, ELFs, reports and a summary.
func TestStringSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases, aborts := stringSBFCases()
	source := stringSBFSource(t)
	keep := os.Getenv("GOSVM_STRING_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "strings", source, 0, cases, keep)
			vectors, vectorAborts := len(StringVectorsForTest()), 0
			for _, c := range cases[:vectors] {
				if c.output == nil {
					vectorAborts++
				}
			}
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases), "aborts": aborts,
				"vectors": vectors, "vector_aborts": vectorAborts, "heap_cases": len(cases) - vectors, "heap_out_of_memory": aborts - vectorAborts}
			maxCU := uint64(0)
			for name, v := range result {
				if strings.HasPrefix(name, "heap-") {
					summary[name+"_cu"] = v
				}
				if strings.HasPrefix(name, "string-") && v.(uint64) > maxCU {
					maxCU = v.(uint64)
				}
			}
			summary["max_vector_cu"] = maxCU
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match native Go and the heap model\n%s", profile, len(cases), aborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
