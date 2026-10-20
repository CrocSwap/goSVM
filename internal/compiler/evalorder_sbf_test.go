package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// TestEvalOrderSBF builds examples/eval-order for fogo-v0 and solana-v3 and
// runs three vectors of every case through the pinned runners: completions
// must leave native Go's output bytes; aborts must roll back, logging
// native Go's panic line when a call panicked first, and no panic line when
// an operand trapped first (docs/ERROR_CONTRACT.md#evaluation-order). Skips
// without the v1.57 backend or a runner (make test). GOSVM_INT_RESULTS=<dir>
// keeps fixtures, ELFs, reports and a summary under eval-order/.
func TestEvalOrderSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	var cases []allocCase
	aborts, logged := 0, 0
	for j, in := range EvalOrderVectorsForTest() {
		k := int(in[1]) | int(in[2])<<8
		c := allocCase{name: fmt.Sprintf("eval-order-%04d-case%03d", j, k), in: in, units: comboUnits}
		out := make([]byte, 1024)
		var line string
		failed := func() (failed bool) {
			defer func() {
				if r := recover(); r != nil {
					failed = true
					line = ExpectedPanicLine(r, "main")
				}
			}()
			evalOrderRun(out, append([]byte(nil), in...))
			return false
		}()
		if failed {
			aborts++
			c.err, c.log = abortError, abortLog
			if line != "" {
				logged++
				c.panicLog = "Program log: " + line
			} else {
				c.noPanicLog = true
			}
		} else {
			c.output = out
		}
		cases = append(cases, c)
	}
	source := comboSBFSource(t, evalOrderSource(t), "evalorder")
	keep := os.Getenv("GOSVM_INT_RESULTS")
	if keep != "" {
		keep = filepath.Join(keep, "eval-order")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			// A fixture suite holds at most 1,024 cases.
			var elf any
			for start, part := 0, 0; start < len(cases); start, part = start+1000, part+1 {
				end := min(start+1000, len(cases))
				result := runAllocProgram(t, llvm, runner, profile, fmt.Sprintf("eval-order-%d", part), source, 0, cases[start:end], keep)
				if elf != nil && result["elf_sha256"] != elf {
					t.Fatalf("ELF changed between builds")
				}
				elf = result["elf_sha256"]
			}
			summary := map[string]any{"profile": profile, "elf_sha256": elf, "cases": len(cases), "aborts": aborts, "panic_lines": logged}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback, %d with a panic line) match native Go\n%s", profile, len(cases), aborts, logged, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
