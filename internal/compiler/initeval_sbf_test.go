package compiler

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	target "gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// Compile-time evaluated initialization on SBF (fogo-v0 and solana-v3,
// through the pinned runners): examples/const-init must reproduce a fresh
// native process per vector both with its initialization image and with
// every initializer at run time, and the CU of initialization is measured
// both ways, for the example and for the real libraries. Results:
// results/compiler/2026-10-16-const-init.

// patchedEntry is a program whose entry file's bytes Process is replaced by
// a Context entry (account 0: the 1 KiB output; instruction data: Run's
// input).
func patchedEntry(t *testing.T, p *Program, file string) *Program {
	t.Helper()
	out := *p
	out.Packages = append([]PackageSources(nil), p.Packages...)
	patched := false
	for i, unit := range out.Packages {
		if unit.Path != out.Entry {
			continue
		}
		sources := append([]Source(nil), unit.Sources...)
		for j, src := range sources {
			if filepath.Base(src.Name) != file {
				continue
			}
			source := string(src.Data)
			entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
			if !strings.Contains(source, entry) || !strings.Contains(source, "import (\n") {
				t.Fatalf("%s: entrypoint changed", file)
			}
			source = strings.Replace(source, "import (\n", "import (\n\t\"gosvm/solana\"\n", 1)
			source = strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1)
			sources[j].Data = []byte(source)
			patched = true
		}
		out.Packages[i].Sources = sources
	}
	if !patched {
		t.Fatalf("%s not found in the entry package", file)
	}
	return &out
}

// sbfCases turns native oracle results into SBF cases.
func sbfCases(t *testing.T, prefix string, vectors [][]byte, want []string) ([]allocCase, int) {
	t.Helper()
	var cases []allocCase
	aborts := 0
	for j, in := range vectors {
		c := allocCase{name: fmt.Sprintf("%s-%03d-mode%d", prefix, j, in[0]), in: in, units: globalUnits, err: abortError, log: abortLog}
		if strings.HasPrefix(want[j], "P:") || strings.HasPrefix(want[j], "L:") {
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
	return cases, aborts
}

func keepDir(t *testing.T, keep, profile string) string {
	if keep == "" {
		return ""
	}
	if err := os.MkdirAll(filepath.Join(keep, profile), 0755); err != nil {
		t.Fatal(err)
	}
	return keep
}

// TestConstInitSBF runs examples/const-init's vectors on both targets with
// and without compile-time evaluation, against fresh native processes, and
// measures initialization (the idle vector) both ways.
// GOSVM_CONSTINIT_RESULTS=<dir> keeps fixtures, ELFs, reports and a summary.
func TestConstInitSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	oracle := buildOracle(t, "./examples/const-init/oracle")
	vectors := ConstInitVectorsForTest()
	idle := make([]byte, 128)
	idle[0] = 3 // Run returns at once: initialization only
	vectors = append(vectors, idle)
	want := nativeGlobals(t, oracle, vectors)
	cases, aborts := sbfCases(t, "const-init", vectors, want)
	keep := os.Getenv("GOSVM_CONSTINIT_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Fatalf("runner unavailable: %v (install the pinned runners; see docs/DEVELOPMENT.md)", err)
			}
			summary := map[string]any{"profile": profile, "cases": len(cases), "aborts": aborts}
			baseline := runGlobalProgram(t, llvm, runner, profile, "baseline", &Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte(globalProbeSource(""))}}}}},
				[]allocCase{{name: "idle", in: make([]byte, 8), output: make([]byte, 1024)}}, keepDir(t, keep, profile))["idle"].(uint64)
			summary["baseline_cu"] = baseline
			for _, runtimeInit := range []bool{false, true} {
				program := patchedEntry(t, constInitProgram(t), "program.go")
				program.RuntimeInit = runtimeInit
				name := "image"
				if runtimeInit {
					name = "runtime-init"
				}
				r := runGlobalProgram(t, llvm, runner, profile, "const-init-"+name, program, cases, keepDir(t, keep, profile))
				idleCU := r[cases[len(cases)-1].name].(uint64)
				maxCU := uint64(0)
				for k, v := range r {
					if strings.HasPrefix(k, "const-init-") && v.(uint64) > maxCU {
						maxCU = v.(uint64)
					}
				}
				summary[name] = map[string]any{"elf_sha256": r["elf_sha256"], "elf_bytes": r["elf_bytes"], "idle_cu": idleCU,
					"init_cu": int64(idleCU) - int64(baseline), "max_vector_cu": maxCU}
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback) match fresh native processes with and without compile-time evaluation\n%s", profile, len(cases), aborts, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "const-init.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestInitEvalEdgesSBF checks, on both targets and both ways, that a panic
// after an evaluated prefix still aborts every invocation with its log
// line, and that an image under a heap budget above 32 KiB faults at entry
// without a large enough heap frame, as the first allocation does.
func TestInitEvalEdgesSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	panicSource := globalProbeSource(`
var a = triple(5)
var b = check(a)

func triple(x uint64) uint64 { return 3 * x }
func check(x uint64) uint64 {
	if x > 5 {
		panic("too big")
	}
	return x
}`)
	frameSource := `package main

import "gosvm/solana"

var table = make([]uint64, 64)

func Process(c solana.Context) uint64 {
	out := solana.Data(c, 0)
	out[0] = byte(len(table))
	return 0
}
`
	program := func(source string, heap int, runtimeInit bool) *Program {
		return &Program{Entry: "program", HeapBytes: heap, RuntimeInit: runtimeInit,
			Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte(source)}}}}}
	}
	checked, err := Check(program(panicSource, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	if img := checked.InitImage; img == nil || img.Steps != 1 || img.Total != 2 || img.Stop == nil || img.Stop.Reason != "panic" {
		t.Fatalf("panic program image %+v", checked.InitImage)
	}
	fits := make([]byte, 1024)
	fits[0] = 64
	frameCases := []allocCase{
		{name: "frame-64k", in: make([]byte, 8), frame: 65536, output: fits},
		{name: "no-frame", in: make([]byte, 8), err: abortError, log: violationLog},
	}
	panicCases := []allocCase{{name: "panics", in: make([]byte, 8), err: abortError, log: abortLog, panicLog: "Program log: panic: too big"}}
	keep := os.Getenv("GOSVM_CONSTINIT_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Fatalf("runner unavailable: %v (install the pinned runners; see docs/DEVELOPMENT.md)", err)
			}
			summary := map[string]any{"profile": profile}
			for _, runtimeInit := range []bool{false, true} {
				mode := "image"
				if runtimeInit {
					mode = "runtime-init"
				}
				p := runGlobalProgram(t, llvm, runner, profile, mode+"-init-panic", program(panicSource, 0, runtimeInit), panicCases, keepDir(t, keep, profile))
				f := runGlobalProgram(t, llvm, runner, profile, mode+"-heap-frame", program(frameSource, 65536, runtimeInit), frameCases, keepDir(t, keep, profile))
				summary[mode] = map[string]any{"init_panic_cu": p["panics"], "frame_64k_cu": f["frame-64k"], "no_frame_cu": f["no-frame"]}
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: the panic and the heap-frame fault happen as without compile-time evaluation\n%s", profile, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "edges.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// realLibsInitMeasured are the realLibsInitProbes measured both ways.
var realLibsInitMeasured = []string{"none", "uint256", "int256", "uint128", "constants (and uint256)", "utils (all four packages)", "app: ComputeSwapStep only", "app: tick math only"}

// TestRealLibrariesInitSBF measures the real libraries' initialization with
// and without compile-time evaluation on both targets: the probes that
// import each library (idle invocation), and examples/real-libraries'
// ComputeSwapStep and tick-math bench vectors, whose total per-invocation
// CU includes initialization. Outputs must match the libraries run
// natively both ways. GOSVM_CONSTINIT_RESULTS=<dir> keeps the reports.
func TestRealLibrariesInitSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	env := realLibs(t)
	var vectors [][]byte
	var names []string
	for _, b := range realLibsBenches() {
		if b.op == 0 || b.op == 16 || b.op == 17 || b.op == 21 {
			vectors = append(vectors, b.in)
			names = append(names, b.name)
		}
	}
	idle := make([]byte, 256)
	idle[0] = 0xff
	vectors = append(vectors, idle)
	names = append(names, "idle")
	want := nativeRealLibs(t, env.oracle, vectors)
	var cases []allocCase
	for j, in := range vectors {
		_, hexOut, _ := strings.Cut(strings.TrimSpace(want[j]), ":")
		out, err := hex.DecodeString(hexOut)
		if err != nil || len(out) != 1024 {
			t.Fatalf("%s: native output %q: %v", names[j], want[j], err)
		}
		cases = append(cases, allocCase{name: fmt.Sprintf("b%02d", j), in: in, units: globalUnits, output: out})
	}
	keep := os.Getenv("GOSVM_CONSTINIT_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Fatalf("runner unavailable: %v (install the pinned runners; see docs/DEVELOPMENT.md)", err)
			}
			modes := map[string]any{}
			for _, runtimeInit := range []bool{false, true} {
				mode := "image"
				if runtimeInit {
					mode = "runtime-init"
				}
				probes := []map[string]any{}
				initCU := map[string]uint64{}
				for j, probe := range realLibsInitProbes {
					measured := false
					for _, name := range realLibsInitMeasured {
						measured = measured || name == probe.name
					}
					if !measured {
						continue
					}
					dir := filepath.Join(env.onchain, "probes", fmt.Sprintf("p%02d", j))
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "program.go"), []byte(realLibsInitSource(probe.imports, probe.decls, probe.use)), 0644); err != nil {
						t.Fatal(err)
					}
					p, err := ReadProgram(dir)
					if err != nil {
						t.Fatal(err)
					}
					p.RuntimeInit = runtimeInit
					r := runGlobalProgram(t, llvm, runner, profile, fmt.Sprintf("%s-init-%02d", mode, j), p,
						[]allocCase{{name: "idle", in: make([]byte, 8), output: make([]byte, 1024)}}, keepDir(t, keep, profile))
					cu := r["idle"].(uint64)
					initCU[probe.name] = cu
					probes = append(probes, map[string]any{"probe": probe.name, "cu": cu, "init_cu": int64(cu) - int64(initCU["none"]),
						"elf_bytes": r["elf_bytes"], "elf_sha256": r["elf_sha256"]})
				}
				program := realLibsSBFProgram(t, env)
				program.RuntimeInit = runtimeInit
				r := runGlobalProgram(t, llvm, runner, profile, mode+"-real-libraries", program, cases, keepDir(t, keep, profile))
				benches := []map[string]any{}
				for j, c := range cases {
					benches = append(benches, map[string]any{"operation": names[j], "cu": r[c.name]})
				}
				modes[mode] = map[string]any{"probes": probes, "example": map[string]any{"elf_bytes": r["elf_bytes"], "elf_sha256": r["elf_sha256"], "bench": benches}}
			}
			summary := map[string]any{"profile": profile, "modes": modes}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: real-library outputs match native Go with and without compile-time evaluation\n%s", profile, encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "real-libraries.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
