package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	allocvalues "gosvm/examples/alloc-values"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// allocSBFSource is examples/alloc-values with a Context entrypoint: account
// 0 is the 1 KiB output and the instruction data is Run's input.
func allocSBFSource(t *testing.T) []byte {
	source := string(mustRead(t, "../../examples/alloc-values/program.go"))
	source = strings.Replace(source, "package allocvalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("alloc-values entrypoint changed")
	}
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { return Run(solana.Data(c, 0), solana.Instruction(c)) }", 1))
}

// allocCase is one transaction: optional RequestHeapFrame, then Run(in).
type allocCase struct {
	name   string
	in     []byte
	frame  uint32 // RequestHeapFrame bytes; 0 sends none
	units  uint32 // SetComputeUnitLimit; 0 sends none (the default limit)
	output []byte // expected account data; nil when the transaction fails
	err    string // expected error JSON when output is nil
	log    string // a log line the failure must produce exactly once
	// panicLog is a panic's log line, which the failure must also produce
	// exactly once ("" for none).
	panicLog string
	// noPanicLog requires a failure without any panic log line.
	noPanicLog bool
}

// Both the abort trap and a heap access violation fail the instruction with
// ProgramFailedToComplete; their logs tell them apart.
const (
	abortError   = `"ProgramFailedToComplete"`
	abortLog     = "failed: SBF program panicked"
	violationLog = "failed: Access violation in heap section at address 0x30000ffff of size 1"
)

func touchInput(n uint64) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = 8, byte(n), byte(n>>8), byte(n>>16)
	return in
}

func nativeOutput(in []byte) []byte {
	out := make([]byte, 1024)
	failed := func() (failed bool) {
		defer func() { failed = recover() != nil }()
		allocvalues.Run(out, in)
		return false
	}()
	if failed {
		return nil
	}
	return out
}

func allocFixtures(cases []allocCase) ([]byte, error) {
	program := base58(probeKey("gosvm allocation probe program; local tests only"))
	data := base58(probeKey("gosvm allocation probe output"))
	state := func(d []byte) sbftest.FixtureState {
		return sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000_000}
	}
	initial := state(make([]byte, 1024))
	suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program, PayerSeed: hex.EncodeToString(probeKey("gosvm allocation probe payer; never fund")),
		NativePrograms: []sbftest.FixtureNativeProgram{{Name: "compute_budget", Address: "ComputeBudget111111111111111111111111111111"}},
		Accounts:       []sbftest.FixtureAccount{{Name: "out", Address: data, Initial: &initial}}}
	for _, c := range cases {
		step := sbftest.FixtureStep{Expect: sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "out"}}}}
		index := 0
		if c.frame != 0 {
			frame := make([]byte, 5)
			frame[0] = 1 // RequestHeapFrame(u32)
			binary.LittleEndian.PutUint32(frame[1:], c.frame)
			step.Instructions = append(step.Instructions, sbftest.FixtureInstruction{Program: "compute_budget", Data: hex.EncodeToString(frame)})
			index++
		}
		if c.units != 0 {
			limit := make([]byte, 5)
			limit[0] = 2 // SetComputeUnitLimit(u32)
			binary.LittleEndian.PutUint32(limit[1:], c.units)
			step.Instructions = append(step.Instructions, sbftest.FixtureInstruction{Program: "compute_budget", Data: hex.EncodeToString(limit)})
			index++
		}
		step.Instructions = append(step.Instructions, sbftest.FixtureInstruction{Program: "program", Accounts: []sbftest.FixtureMeta{{Account: "out", Writable: true}}, Data: hex.EncodeToString(c.in)})
		expected := initial
		if c.output != nil {
			expected = state(c.output)
		} else {
			step.Expect.Error = json.RawMessage(fmt.Sprintf(`{"InstructionError":[%d,%s]}`, index, c.err))
			if c.log != "" {
				step.Expect.LogCounts = map[string]int{c.log: 1}
			}
			if c.panicLog != "" {
				step.Expect.LogCounts[c.panicLog] = 1
			}
			if c.noPanicLog {
				step.Expect.LogCounts["Program log: panic: "] = 0
			}
		}
		step.Expect.Accounts[0].State = &expected
		suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: c.name, Steps: []sbftest.FixtureStep{step}})
	}
	return json.MarshalIndent(suite, "", " ")
}

// allocSBFCases are the vector cases (native expectations; a panic is an
// abort with rollback) and the exact 32 KiB out-of-memory boundary.
func allocSBFCases() []allocCase {
	var cases []allocCase
	for j, in := range AllocVectorsForTest() {
		c := allocCase{name: fmt.Sprintf("vector-%03d-mode%d", j, in[0]), in: in, output: nativeOutput(in), err: abortError, log: abortLog}
		cases = append(cases, c)
	}
	// 8 bytes of allocator state + 32760 = 32768: the last byte that fits.
	cases = append(cases,
		allocCase{name: "heap32-fits-32760", in: touchInput(32760), output: nativeOutput(touchInput(32760))},
		allocCase{name: "heap32-oom-32761", in: touchInput(32761), err: abortError, log: abortLog},
		allocCase{name: "heap32-oom-40000", in: touchInput(40000), err: abortError, log: abortLog})
	return cases
}

// allocLargeHeapCases run against a build with heap_bytes 65536.
func allocLargeHeapCases() []allocCase {
	return []allocCase{
		{name: "heap64-frame-fits-65528", in: touchInput(65528), frame: 65536, output: nativeOutput(touchInput(65528))},
		{name: "heap64-frame-fits-40000", in: touchInput(40000), frame: 65536, output: nativeOutput(touchInput(40000))},
		{name: "heap64-frame-oom-65529", in: touchInput(65529), frame: 65536, err: abortError, log: abortLog},
		{name: "heap64-larger-frame-fits", in: touchInput(65528), frame: 128 * 1024, output: nativeOutput(touchInput(65528))},
		// Without a large enough frame the first allocation faults on the
		// budget's last byte, even for a small allocation.
		{name: "heap64-no-frame-faults", in: touchInput(8), err: abortError, log: violationLog},
		{name: "heap64-small-frame-faults", in: touchInput(8), frame: 48 * 1024, err: abortError, log: violationLog},
	}
}

// TestAllocationSBF builds the allocation probe for fogo-v0 and solana-v3
// (32 KiB and 64 KiB budgets) and runs the native-Go vectors, the 32 KiB
// out-of-memory boundary and RequestHeapFrame cases through the pinned
// runners. Skips without the v1.57 backend or a runner (make test).
// GOSVM_ALLOC_RESULTS=<dir> keeps fixtures, ELFs, reports and a summary.
func TestAllocationSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	// The SBF vectors must fit the default heap: check them on the host first.
	source := allocSBFSource(t)
	hostSource := string(mustRead(t, "../../examples/alloc-values/program.go"))
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: []byte(hostSource)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	for _, in := range append(AllocVectorsForTest(), touchInput(32760)) {
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", in)), nativeAllocCall(in); got != want {
			t.Fatalf("vector %x does not fit a 32 KiB heap on the host: %q", in, got[:10])
		}
	}
	keep := os.Getenv("GOSVM_ALLOC_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			summary := map[string]any{"profile": profile}
			cases := allocSBFCases()
			small := runAllocProgram(t, llvm, runner, profile, "heap32", source, 0, cases, keep)
			large := runAllocProgram(t, llvm, runner, profile, "heap64", source, 65536, allocLargeHeapCases(), keep)
			aborts := 0
			for _, c := range cases {
				if c.output == nil {
					aborts++
				}
			}
			summary["heap32_elf_sha256"], summary["heap64_elf_sha256"] = small["elf_sha256"], large["elf_sha256"]
			summary["heap32_cases"], summary["heap32_aborts"], summary["heap64_cases"] = len(cases), aborts, len(allocLargeHeapCases())
			for _, name := range []string{"heap32-fits-32760", "heap64-frame-fits-65528", "heap64-frame-fits-40000"} {
				if v, ok := small[name]; ok {
					summary[name+"_cu"] = v
				}
				if v, ok := large[name]; ok {
					summary[name+"_cu"] = v
				}
			}
			maxCU := uint64(0)
			for name, v := range small {
				if strings.HasPrefix(name, "vector-") && v.(uint64) > maxCU {
					maxCU = v.(uint64)
				}
			}
			summary["max_vector_cu"] = maxCU
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s", encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(keep, profile, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// runAllocProgram builds the probe with the heap budget, runs the cases and
// returns each case's CU and the ELF hash.
func runAllocProgram(t *testing.T, llvm, runner, profile, name string, source []byte, heap int, cases []allocCase, keep string) map[string]any {
	t.Helper()
	dir := t.TempDir()
	if keep != "" {
		dir = filepath.Join(keep, profile, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "program.go"), source, 0644); err != nil {
		t.Fatal(err)
	}
	elf := filepath.Join(dir, "program.so")
	if err := BuildWithOptions(dir, elf, llvm, profile, BuildOptions{HeapBytes: heap}); err != nil {
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
