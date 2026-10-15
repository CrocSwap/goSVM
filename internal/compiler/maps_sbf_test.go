package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mapvalues "gosvm/examples/map-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// mapSBFSource is examples/map-values with a Context entrypoint: account 0
// is the 1 KiB output and the instruction data is Run's input.
func mapSBFSource(t *testing.T) []byte {
	source := string(mapSource(t))
	source = strings.Replace(source, "package mapvalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("map-values entrypoint changed")
	}
	// Run returns the bytes written; the on-chain entrypoint returns 0.
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1))
}

// mapUnits is the compute unit limit of every map transaction: scripts,
// growth and heap boundaries exceed the default 200,000.
const mapUnits = 1_400_000

// mapProbeSizes and mapProbeOps are the CU probe's map sizes and operation
// counts (mode 8); string keys are distinct only below 56.
var (
	mapProbeSizes       = []int{8, 64, 384}
	mapProbeStringSizes = []int{8, 48}
)

const mapProbeOps = 256

func mapProbeInput(kind, size, n int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3], in[4], in[5] = 8, byte(kind), byte(size), byte(size>>8), byte(n), byte(n>>8)
	return in
}

func mapProbeName(kind, size, n int) string {
	return fmt.Sprintf("probe-kind%d-size%d-n%d", kind, size, n)
}

// mapSBFCases are the vector cases (native expectations; a panic is an abort
// with rollback), the heap boundaries and the CU probes.
func mapSBFCases() ([]allocCase, int) {
	var cases []allocCase
	aborts := 0
	native := func(name string, in []byte) allocCase {
		c := allocCase{name: name, in: in, units: mapUnits, err: abortError, log: abortLog}
		out := make([]byte, 1024)
		failed := func() (failed bool) {
			defer func() { failed = recover() != nil }()
			mapvalues.Run(out, append([]byte(nil), in...))
			return false
		}()
		if failed {
			aborts++
		} else {
			c.output = out
		}
		return c
	}
	for j, in := range MapVectorsForTest() {
		cases = append(cases, native(fmt.Sprintf("map-%03d-mode%d", j, in[0]), in))
	}
	for _, kh := range mapHeapKinds {
		kind, hint := byte(kh[0]), kh[1]
		limit := mapHeapLimit(kind, hint, DefaultHeapBytes)
		if limit == mapNoLimit {
			// 2,000 iterations stay within the compute limit.
			cases = append(cases, native(fmt.Sprintf("heap-kind%d-hint%d-2000-fits", kind, hint), mapHeapInput(kind, 2000, hint)))
			continue
		}
		if limit >= 0 {
			cases = append(cases, native(fmt.Sprintf("heap-kind%d-hint%d-%d-fits", kind, hint, limit), mapHeapInput(kind, limit, hint)))
		}
		cases = append(cases, allocCase{name: fmt.Sprintf("heap-kind%d-hint%d-%d-oom", kind, hint, limit+1), in: mapHeapInput(kind, limit+1, hint), units: mapUnits, err: abortError, log: abortLog})
		aborts++
	}
	probe := func(kind int, sizes []int) {
		for _, size := range sizes {
			for _, n := range []int{0, mapProbeOps} {
				cases = append(cases, native(mapProbeName(kind, size, n), mapProbeInput(kind, size, n)))
			}
		}
	}
	for kind := 0; kind < 4; kind++ {
		probe(kind, mapProbeSizes)
	}
	probe(4, mapProbeStringSizes)
	probe(5, mapProbeSizes[:1])
	return cases, aborts
}

// TestMapSBF builds examples/map-values for fogo-v0 and solana-v3 with the
// default 32 KiB heap and runs the vectors, heap boundaries and CU probes
// through the pinned runners: completions must leave native Go's output
// bytes; panics and out-of-memory must abort and roll back. Skips without
// the v1.57 backend or a runner (make test). GOSVM_MAP_RESULTS=<dir> keeps
// fixtures, ELFs, reports and a summary.
func TestMapSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	cases, aborts := mapSBFCases()
	source := mapSBFSource(t)
	keep := os.Getenv("GOSVM_MAP_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "maps", source, 0, cases, keep)
			vectors, vectorAborts := len(MapVectorsForTest()), 0
			for _, c := range cases[:vectors] {
				if c.output == nil {
					vectorAborts++
				}
			}
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases), "aborts": aborts,
				"vectors": vectors, "vector_aborts": vectorAborts}
			heapCases, maxCU := 0, uint64(0)
			for name, v := range result {
				switch {
				case strings.HasPrefix(name, "heap-"):
					summary[name+"_cu"] = v
					heapCases++
				case strings.HasPrefix(name, "map-"):
					if v.(uint64) > maxCU {
						maxCU = v.(uint64)
					}
				}
			}
			summary["heap_cases"], summary["max_vector_cu"] = heapCases, maxCU
			// CU per operation: the difference between mapProbeOps
			// operations and none, divided by mapProbeOps; the baseline is
			// the same loop without a map.
			perOp := func(kind, size int) float64 {
				hi, lo := result[mapProbeName(kind, size, mapProbeOps)].(uint64), result[mapProbeName(kind, size, 0)].(uint64)
				return float64(hi-lo) / mapProbeOps
			}
			baseline := perOp(5, mapProbeSizes[0])
			cu := map[string]any{"loop_baseline": baseline}
			names := []string{"insert", "lookup_hit", "lookup_miss", "delete_reinsert", "string_lookup_hit"}
			for kind, name := range names {
				sizes := mapProbeSizes
				if kind == 4 {
					sizes = mapProbeStringSizes
				}
				for _, size := range sizes {
					cu[fmt.Sprintf("%s_size%d", name, size)] = perOp(kind, size)
				}
			}
			summary["cu_per_operation"] = cu
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
