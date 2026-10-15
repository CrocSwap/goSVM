package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// globalUnits is the compute unit limit of every globals transaction.
const globalUnits = 1_400_000

// globalSBFEntry gives examples/global-values a Context entrypoint: account
// 0 is the 1 KiB output and the instruction data is Run's input.
func globalSBFEntry(t *testing.T) func(string) string {
	return func(source string) string {
		entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
		if !strings.Contains(source, entry) || !strings.Contains(source, "import (\n") {
			t.Fatal("global-values entrypoint changed")
		}
		source = strings.Replace(source, "import (\n", "import (\n\t\"gosvm/solana\"\n", 1)
		return strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1)
	}
}

// globalProbes are small programs whose only work is initialization:
// Process returns 0 at once, so the difference from the baseline is the
// cost of initializing their package-level variables on every invocation.
var globalProbes = []struct{ name, decls string }{
	{"baseline", ``},
	{"readonly-tables", `
var pows = [...]uint64{1, 10, 100, 1000, 10000, 100000, 1000000, 10000000}
var tbl = []uint32{3, 5, 7, 11, 13}
var msg = "constant"
func Lookup(i uint8) uint64 { return pows[i&7] + uint64(tbl[i%5]) + uint64(len(msg)) }`},
	{"one-counter", `
var counter uint64 = 5
func Bump() { counter++ }`},
	{"zero-block", `
var hits [16]uint64
func Hit(i uint8) { hits[i&15]++ }`},
	{"sentinel-errors", `
type E struct{ msg string }
func (e *E) Error() string { return e.msg }
var ErrA error = &E{"a"}
var ErrB error = &E{"b"}
var ErrC error = &E{"c"}`},
	{"map-literal", `
var registry = map[string]uint64{"a": 1, "b": 2, "c": 3, "d": 4}`},
	{"init-table", `
var squares [64]uint64
func init() {
	for i := range squares {
		squares[i] = uint64(i * i)
	}
}`},
}

func globalProbeSource(decls string) string {
	return "package main\n\nimport \"gosvm/solana\"\n" + decls + "\n\nfunc Process(c solana.Context) uint64 { return 0 }\n"
}

// runGlobalProgram builds program for the profile, runs the cases and
// returns each case's CU and the ELF hash.
func runGlobalProgram(t *testing.T, llvm, runner, profile, name string, program *Program, cases []allocCase, keep string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	if keep != "" {
		dir = filepath.Join(keep, profile, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	elf := filepath.Join(dir, "program.so")
	if err := buildProgram(program, elf, llvm, profile); err != nil {
		t.Fatal(err)
	}
	fixtures, err := allocFixtures(cases)
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(dir, "fixtures.json")
	if err = os.WriteFile(fixturePath, fixtures, 0644); err != nil {
		t.Fatal(err)
	}
	p, err := target.Resolve(profile)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "svm-results.json")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var log strings.Builder
	if err = sbftest.RunGeneral(ctx, elf, fixturePath, report, io.Writer(&log), sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, log.String())
	}
	var parsed struct {
		Cases []sbftest.FixtureCaseResult `json:"cases"`
	}
	if err = json.Unmarshal(mustRead(t, report), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Cases) != len(cases) {
		t.Fatalf("%s: ran %d of %d cases", name, len(parsed.Cases), len(cases))
	}
	out := map[string]any{"elf_sha256": fmt.Sprintf("%x", sha256.Sum256(mustRead(t, elf)))}
	for _, c := range parsed.Cases {
		out[c.Name] = c.Steps[0].CU
	}
	return out
}

// TestGlobalsSBF builds examples/global-values (five packages) for fogo-v0
// and solana-v3 and runs the vectors through the pinned runners: every
// transaction is a fresh invocation, so each must reproduce the output of a
// fresh native process; Go panics must abort and roll back. It also
// measures the per-invocation cost of initialization with the probes.
// Skips without the v1.57 backend or a runner (make test).
// GOSVM_GLOBAL_RESULTS=<dir> keeps fixtures, ELFs, reports and a summary.
func TestGlobalsSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	oracle := globalOracle(t)
	vectors := GlobalVectorsForTest()
	idle := make([]byte, 128)
	idle[0] = globalModes // Run returns at once: initialization only
	vectors = append(vectors, idle)
	want := nativeGlobals(t, oracle, vectors)
	var cases []allocCase
	aborts := 0
	for j, in := range vectors {
		c := allocCase{name: fmt.Sprintf("global-%03d-mode%d", j, in[0]), in: in, units: globalUnits, err: abortError, log: abortLog}
		if strings.HasPrefix(want[j], "P:") {
			aborts++
		} else {
			_, hexOut, _ := strings.Cut(strings.TrimSpace(want[j]), ":")
			out, err := hex.DecodeString(hexOut)
			if err != nil || len(out) != 1024 {
				t.Fatalf("vector %d: native output %q: %v", j, want[j], err)
			}
			c.output = out
		}
		cases = append(cases, c)
	}
	program := globalProgramSources(t, globalSBFEntry(t))
	probeCases := []allocCase{{name: "idle", in: make([]byte, 8), output: make([]byte, 1024)}}
	keep := os.Getenv("GOSVM_GLOBAL_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runGlobalProgram(t, llvm, runner, profile, "global-values", program, cases, keep)
			idleName := cases[len(cases)-1].name
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases), "aborts": aborts,
				"idle_cu": result[idleName]}
			maxCU := uint64(0)
			for name, v := range result {
				if strings.HasPrefix(name, "global-") && v.(uint64) > maxCU {
					maxCU = v.(uint64)
				}
			}
			summary["max_vector_cu"] = maxCU
			probes := map[string]any{}
			var baseline uint64
			for _, probe := range globalProbes {
				p := &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte(globalProbeSource(probe.decls))}}}}}
				r := runGlobalProgram(t, llvm, runner, profile, "probe-"+probe.name, p, probeCases, keep)
				cu := r["idle"].(uint64)
				if probe.name == "baseline" {
					baseline = cu
				}
				probes[probe.name] = map[string]any{"cu": cu, "init_cu": int64(cu) - int64(baseline), "elf_sha256": r["elf_sha256"]}
			}
			summary["probes"] = probes
			summary["global_values_init_cu"] = int64(result[idleName].(uint64)) - int64(baseline)
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match fresh native processes\n%s", profile, len(cases), aborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
