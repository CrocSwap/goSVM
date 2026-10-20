package compiler

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// globalValueTypesSBFEntry gives examples/global-value-types a Context
// entrypoint: account 0 is the 1 KiB output and the instruction data is
// Run's input.
func globalValueTypesSBFEntry(t *testing.T) func(string) string {
	return func(source string) string {
		entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
		imp := "import \"gosvm/examples/global-value-types/wide\"\n"
		if !strings.Contains(source, entry) || !strings.Contains(source, imp) {
			t.Fatal("global-value-types entrypoint changed")
		}
		source = strings.Replace(source, imp, imp+"import \"gosvm/solana\"\n", 1)
		return strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1)
	}
}

// oracleCase turns one native oracle line into an SBF case: a completion
// must leave the output bytes; a panic must abort with rollback and, for a
// panic call, log its line once.
func oracleCase(t *testing.T, name string, in []byte, line string) allocCase {
	t.Helper()
	c := allocCase{name: name, in: in, units: globalUnits, err: abortError, log: abortLog}
	line = strings.TrimSuffix(line, "\n")
	if strings.HasPrefix(line, "L:") {
		logHex, rest, _ := strings.Cut(line[2:], " ")
		logLine, err := hex.DecodeString(logHex)
		if err != nil {
			t.Fatal(err)
		}
		c.panicLog = "Program log: " + string(logLine)
		line = rest
	}
	if strings.HasPrefix(line, "P:") {
		return c
	}
	code, out, ok := strings.Cut(line, ":")
	if _, err := strconv.Atoi(code); !ok || err != nil {
		t.Fatalf("%s: oracle line %q", name, line)
	}
	output, err := hex.DecodeString(out)
	if err != nil {
		t.Fatal(err)
	}
	c.output = output
	return c
}

// TestGlobalValueTypesSBF builds examples/global-value-types (two packages)
// for fogo-v0 and solana-v3 and runs its vectors through the pinned
// runners, each against a fresh native process; then each
// initialization-panic probe, which must abort before Process with its log
// line. Skips without the v1.57 backend or a runner (make test).
// GOSVM_GLOBAL_RESULTS=<dir> keeps fixtures, ELFs, reports and a summary.
func TestGlobalValueTypesSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	oracle := goBinary(t, "../..", "./examples/global-value-types/oracle")
	vectors := GlobalValueTypesVectorsForTest()
	want := nativeGlobals(t, oracle, vectors)
	var cases []allocCase
	aborts, logged := 0, 0
	for j, in := range vectors {
		c := oracleCase(t, fmt.Sprintf("gvt-%03d-mode%d", j, in[0]), in, want[j])
		if c.output == nil {
			aborts++
		}
		if c.panicLog != "" {
			logged++
		}
		cases = append(cases, c)
	}
	idle := make([]byte, 128)
	idle[0] = 255 // Run returns at once: the cost of initialization
	cases = append(cases, allocCase{name: "idle", in: idle, units: globalUnits, output: make([]byte, 1024)})
	program := globalValueTypesProgram(t, globalValueTypesSBFEntry(t))
	keep := os.Getenv("GOSVM_GLOBAL_RESULTS")
	if keep != "" {
		keep = filepath.Join(keep, "global-value-types")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runGlobalProgram(t, llvm, runner, profile, "global-value-types", program, cases, keep)
			maxCU := uint64(0)
			for _, c := range cases {
				if v, ok := result[c.name].(uint64); ok && c.output != nil && v > maxCU {
					maxCU = v
				}
			}
			probes := map[string]any{}
			for j, probe := range globalInitPanicProbes {
				c := allocCase{name: "init-panic", in: make([]byte, 8), err: abortError, log: abortLog}
				if probe.line != "" {
					c.panicLog = "Program log: " + probe.line
				}
				r := runGlobalProgram(t, llvm, runner, profile, fmt.Sprintf("init-panic-%d", j), initPanicProgram(probe.src, probe.lib, true), []allocCase{c}, keep)
				probes[probe.name] = map[string]any{"cu": r["init-panic"], "elf_sha256": r["elf_sha256"], "log_line": probe.line}
			}
			summary := map[string]any{
				"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases),
				"aborts": aborts, "panic_log_lines": logged, "max_completed_cu": maxCU,
				"idle_cu": result["idle"], "init_panic_probes": probes,
			}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases (%d aborts with rollback, %d with a panic log line) match fresh native processes; %d initialization-panic probes abort with their lines\n%s", profile, len(cases), aborts, logged, len(probes), encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
