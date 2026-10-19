package compiler

import (
	"bytes"
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

// memorySBFSource is examples/memory-values with a Context entrypoint:
// account 0 is out and the instruction data is in.
func memorySBFSource(t *testing.T) []byte {
	source := string(memoryValuesSource(t))
	source = strings.Replace(source, "package memoryvalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("memory-values entrypoint changed")
	}
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { return Run(solana.Data(c, 0), solana.Instruction(c)) }", 1))
}

// memoryVariants are the thresholds each SBF test builds: the chosen one
// (every move a syscall), a mixed one whose threshold edges the vectors
// straddle (loops below 64 bytes, syscalls from 64 in the same program), and
// every move a loop. All must give native Go's results; their CU differ.
var memoryVariants = []struct {
	name      string
	threshold uint64
}{
	{"chosen", memorySyscallMin},
	{"mixed64", 64},
	{"loops", 1 << 62},
}

// buildMemoryVariant builds source with the given thresholds.
func buildMemoryVariant(t *testing.T, dir, llvm, profile string, source []byte, threshold uint64) string {
	t.Helper()
	saved := memorySyscallMin
	memorySyscallMin = threshold
	defer func() { memorySyscallMin = saved }()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "program.go"), source, 0644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "program.so")
	if err := Build(dir, output, llvm, profile); err != nil {
		t.Fatal(err)
	}
	return output
}

type memoryCase struct {
	name   string
	in     []byte
	output []byte // nil: the transaction aborts and rolls back
}

func memoryFixtures(cases []memoryCase) ([]byte, error) {
	program := base58(probeKey("gosvm memory probe program; local tests only"))
	state := func(d []byte) *sbftest.FixtureState {
		return &sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000_000}
	}
	initial := memoryPattern()
	suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program, PayerSeed: hex.EncodeToString(probeKey("gosvm memory probe payer; never fund")),
		Accounts: []sbftest.FixtureAccount{{Name: "out", Address: base58(probeKey("gosvm memory probe output")), Initial: state(initial)}}}
	for _, c := range cases {
		step := sbftest.FixtureStep{
			Instructions: []sbftest.FixtureInstruction{{Program: "program", Accounts: []sbftest.FixtureMeta{{Account: "out", Writable: true}}, Data: hex.EncodeToString(c.in)}},
			Expect:       sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "out", State: state(c.output)}}},
		}
		if c.output == nil {
			step.Expect.Error = json.RawMessage(`{"InstructionError":[0,"ProgramFailedToComplete"]}`)
			step.Expect.LogCounts = map[string]int{"failed: SBF program panicked": 1}
			step.Expect.Accounts[0].State = state(initial)
		}
		suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: c.name, Steps: []sbftest.FixtureStep{step}})
	}
	return json.Marshal(suite)
}

// memoryChunk keeps each fixture file under the runner's 16 MiB limit.
const memoryChunk = 600

// runMemoryFixtures runs the cases against elf, in fixture files of at most
// memoryChunk cases, and returns each case's CU.
func runMemoryFixtures(t *testing.T, dir, elfPath, runner, profile string, cases []memoryCase) map[string]uint64 {
	t.Helper()
	cu := map[string]uint64{}
	for start := 0; start < len(cases); start += memoryChunk {
		part := cases[start:min(start+memoryChunk, len(cases))]
		for name, n := range runMemoryChunk(t, dir, fmt.Sprintf("%02d", start/memoryChunk), elfPath, runner, profile, part) {
			cu[name] = n
		}
	}
	return cu
}

func runMemoryChunk(t *testing.T, dir, chunk, elfPath, runner, profile string, cases []memoryCase) map[string]uint64 {
	t.Helper()
	fixtures, err := memoryFixtures(cases)
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(dir, "fixtures-"+chunk+".json")
	if err = os.WriteFile(fixturePath, fixtures, 0644); err != nil {
		t.Fatal(err)
	}
	p, err := target.Resolve(profile)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "svm-results-"+chunk+".json")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var log strings.Builder
	if err = sbftest.RunGeneral(ctx, elfPath, fixturePath, report, io.Writer(&log), sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
		t.Fatalf("%s: %v\n%s", elfPath, err, tail(log.String(), 4000))
	}
	var parsed struct {
		Cases []sbftest.FixtureCaseResult `json:"cases"`
	}
	if err = json.Unmarshal(mustRead(t, report), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Cases) != len(cases) {
		t.Fatalf("ran %d of %d cases", len(parsed.Cases), len(cases))
	}
	cu := map[string]uint64{}
	for _, c := range parsed.Cases {
		cu[c.Name] = c.Steps[0].CU
	}
	return cu
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// v0Imports lists an SBPFv0 ELF's dynamic (syscall) imports.
func v0Imports(t *testing.T, path string) []string {
	t.Helper()
	names, err := undefinedSymbols(path, true)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// TestMemorySBF runs examples/memory-values on fogo-v0 and solana-v3, built
// with the chosen thresholds, with every move a syscall and with every move
// a loop: the same fixtures (native Go's output for each vector, or an abort
// that rolls back) must pass for all three. GOSVM_MEMORY_RESULTS=<dir> keeps
// the fixtures, ELFs, reports and a per-case CU summary.
func TestMemorySBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	var cases []memoryCase
	aborts := 0
	for j, in := range MemoryVectorsForTest() {
		out := nativeMemory(in)
		if out == nil {
			aborts++
		}
		cases = append(cases, memoryCase{name: fmt.Sprintf("v%04d-mode%d", j, in[0]), in: in, output: out})
	}
	if aborts < 10 {
		t.Fatalf("only %d aborting vectors", aborts)
	}
	source := memorySBFSource(t)
	keep := os.Getenv("GOSVM_MEMORY_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			summary := map[string]any{"profile": profile, "cases": len(cases), "aborts": aborts}
			perCase := map[string]map[string]uint64{}
			for _, v := range memoryVariants {
				dir := filepath.Join(t.TempDir(), v.name)
				if keep != "" {
					dir = filepath.Join(keep, profile, v.name)
				}
				elfPath := buildMemoryVariant(t, dir, llvm, profile, source, v.threshold)
				data := mustRead(t, elfPath)
				if profile == target.Fogo && v.name != "loops" {
					// (The loops variant keeps its unreachable syscall branches.)
					imports := strings.Join(v0Imports(t, elfPath), ",")
					if !strings.Contains(imports, "sol_memmove_") || !strings.Contains(imports, "sol_memset_") {
						t.Fatalf("%s imports only %s", v.name, imports)
					}
				}
				cu := runMemoryFixtures(t, dir, elfPath, runner, profile, cases)
				perCase[v.name] = cu
				total := uint64(0)
				for _, n := range cu {
					total += n
				}
				summary[v.name] = map[string]any{"elf_sha256": fmt.Sprintf("%x", sha256.Sum256(data)), "elf_bytes": len(data), "total_cu": total}
			}
			// Record the vector where the chosen threshold costs most
			// against the other two.
			worst, worstName := int64(0), ""
			for name, chosen := range perCase["chosen"] {
				best := min(perCase["mixed64"][name], perCase["loops"][name])
				if d := int64(chosen) - int64(best); d > worst {
					worst, worstName = d, name
				}
			}
			summary["chosen_worst_excess_cu"], summary["chosen_worst_excess_case"] = worst, worstName
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s", encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
				rows, _ := json.Marshal(perCase)
				if err = os.WriteFile(filepath.Join(keep, profile, "case-cu.json"), rows, 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// TestMemoryLegacyUnchanged: target-less legacy builds keep their loops, so
// their ELF does not depend on the thresholds at all.
func TestMemoryLegacyUnchanged(t *testing.T) {
	llvm := filepath.Join(os.Getenv("HOME"), ".cache/solana/v1.51/platform-tools/llvm")
	if _, err := os.Stat(filepath.Join(llvm, "bin", "clang")); err != nil {
		t.Skip("platform-tools v1.51 required")
	}
	source := memorySBFSource(t)
	var elves [][]byte
	for _, v := range memoryVariants {
		elves = append(elves, mustRead(t, buildMemoryVariant(t, filepath.Join(t.TempDir(), v.name), llvm, target.Legacy, source, v.threshold)))
	}
	for j := range elves[1:] {
		if !bytes.Equal(elves[0], elves[j+1]) {
			t.Fatalf("legacy ELF depends on the thresholds (%s)", memoryVariants[j+1].name)
		}
	}
}
