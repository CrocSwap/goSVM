package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	intvalues "gosvm/examples/int-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

func intPanics(v intVector) bool { return intReference(v) == "P" }

// intProbeCases samples the differential vectors for the SBF probe (every
// 48th vector that does not panic in Go, in 400-record transactions), adds
// MinInt / -1 and MinInt % -1 for every signed type, and one transaction per
// trap: division and remainder by zero, and negative shift counts of every
// signed count type. A trap after successful records in the same transaction
// must roll those records back too.
func intProbeCases() []probeCase {
	ref := func(r probeRow) (uint64, uint64) { return intvalues.Eval(r.op, r.a, r.b), 0 }
	var rows []probeRow
	for j, v := range intVectors(300) {
		if j%40 == 0 && !intPanics(v) {
			rows = append(rows, probeRow{v.code, v.a, v.b, 0})
		}
	}
	var cases []probeCase
	for first := 0; first < len(rows); first += 400 {
		end := min(first+400, len(rows))
		cases = append(cases, probeCase{name: fmt.Sprintf("vectors-%03d", len(cases)), rows: rows[first:end], mode: 0, perIx: 50, expectRows: ref})
	}
	var minRows []probeRow
	trap := func(name string, rows ...probeRow) {
		if !intPanics(intVector{rows[len(rows)-1].op, rows[len(rows)-1].a, rows[len(rows)-1].b}) {
			panic(fmt.Sprintf("%s does not panic in Go", name))
		}
		cases = append(cases, probeCase{name: "trap-" + name, rows: rows, mode: 0, perIx: len(rows), abort: true})
	}
	for n, ty := range intTypes {
		code := func(op int) uint64 { return uint64(n)<<8 | uint64(op) }
		trap(ty.name+"-div0", probeRow{code(3), 7, 0, 0})
		trap(ty.name+"-rem0", probeRow{code(4), 7, 0, 0})
		trap(ty.name+"-divmod0", probeRow{code(intOpDivMod), 7, 0, 0})
		// Negative counts: -1 in each signed count type, and MinInt64.
		for k, count := range intCountTypes[:5] {
			trap(fmt.Sprintf("%s-shl-%s", ty.name, count), probeRow{code(intOpShl + k), 1, ^uint64(0), 0})
			trap(fmt.Sprintf("%s-shr-%s", ty.name, count), probeRow{code(intOpShr + k), 1, ^uint64(127), 0})
		}
		trap(ty.name+"-shr-int64-min", probeRow{code(intOpShr + 3), 1, 1 << 63, 0})
		if ty.signed {
			minimum := ^uint64(0) << (ty.width - 1)
			minRows = append(minRows, probeRow{code(3), minimum, ^uint64(0), 0}, probeRow{code(4), minimum, ^uint64(0), 0}, probeRow{code(intOpDivMod), minimum, ^uint64(0), 0})
		}
	}
	cases = append(cases, probeCase{name: "min-by-minus-one", rows: minRows, mode: 0, perIx: len(minRows), expectRows: ref})
	// Earlier records of the same transaction (and instruction) are rolled back.
	trap("after-success", probeRow{3, 9, 2, 0}, probeRow{intOpShl, 5, 3, 0}, probeRow{3, 1, 0, 0})
	return cases
}

// TestIntValuesSBF builds the probe with examples/int-values/ops.go for
// fogo-v0 and solana-v3 and runs sampled differential vectors, MinInt / -1
// and trap transactions (with rollback) through the pinned runners. It skips
// without the v1.57 backend or a runner. GOSVM_INT_RESULTS=<dir> keeps the
// fixtures, ELFs and runner reports.
func TestIntValuesSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	ops := bytes.Replace(mustRead(t, intValuesOps), []byte("\npackage intvalues\n"), []byte("\npackage main\n"), 1)
	files := map[string][]byte{"program.go": mustRead(t, filepath.Join("testdata", "intprobe", "program.go")), "ops.go": ops}
	cases := intProbeCases()
	vectors, traps := 0, 0
	for _, c := range cases {
		if c.abort {
			traps++
		} else {
			vectors += len(c.rows)
		}
	}
	keep := os.Getenv("GOSVM_INT_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runProbeProgram(t, llvm, runner, profile, "intprobe", files, cases, keep)
			summary, _ := json.Marshal(map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "vectors": vectors, "trap_cases": traps, "transactions": len(cases)})
			t.Logf("%s", summary)
		})
	}
}
