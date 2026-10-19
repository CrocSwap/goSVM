package compiler

import (
	"context"
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

// runMemorySuite runs a fixture suite against elf and fails the test with
// the runner's output on any mismatch.
func runMemorySuite(t *testing.T, dir, elfPath, runner, profile string, suite sbftest.FixtureSuite) []sbftest.FixtureCaseResult {
	t.Helper()
	fixtures, err := json.MarshalIndent(suite, "", " ")
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
	if err = sbftest.RunGeneral(ctx, elfPath, fixturePath, report, io.Writer(&log), sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
		t.Fatalf("%s: %v\n%s", elfPath, err, tail(log.String(), 4000))
	}
	var parsed struct {
		Cases []sbftest.FixtureCaseResult `json:"cases"`
	}
	if err = json.Unmarshal(mustRead(t, report), &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Cases
}

// TestMemoryRuntimeSBF covers the SDK runtime's bulk moves on both
// profiles, built with the chosen threshold and with loops only: the
// entrypoint's copy of a duplicate account descriptor and ResizeAccount's
// zero fill after a shrink (stale bytes must not reappear), up to the
// 10 KiB growth limit and past it.
func TestMemoryRuntimeSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	source := mustRead(t, filepath.Join("testdata", "memresize", "program.go"))
	program := base58(probeKey("gosvm memory runtime probe program; local tests only"))
	const length = 200
	initial := make([]byte, length)
	for j := range initial {
		initial[j] = byte(j*29 + 1)
	}
	state := func(d []byte) *sbftest.FixtureState {
		return &sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000_000}
	}
	suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program,
		PayerSeed: hex.EncodeToString(probeKey("gosvm memory runtime probe payer; never fund")),
		Accounts:  []sbftest.FixtureAccount{{Name: "value", Address: base58(probeKey("gosvm memory runtime value")), Initial: state(initial)}}}
	for _, sg := range [][2]uint64{{length, length}, {10, length}, {0, 1}, {0, 0}, {50, 51}, {199, 1000}, {0, length + 10240}, {length, length + 10240}, {3, 4096}, {length, length + 10241}} {
		shrink, grow := sg[0], sg[1]
		want := make([]byte, grow)
		copy(want, initial[:min(shrink, length)])
		expect := sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "value", State: state(want)}}}
		if grow > length+10240 {
			expect = sbftest.FixtureExpectation{Error: json.RawMessage(`{"InstructionError":[0,{"Custom":2010}]}`), Accounts: []sbftest.FixtureCheck{{Account: "value", State: state(initial)}}}
		}
		data := []byte{byte(shrink), byte(shrink >> 8), byte(grow), byte(grow >> 8)}
		suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: fmt.Sprintf("shrink-%d-grow-%d", shrink, grow), Steps: []sbftest.FixtureStep{{
			Instructions: []sbftest.FixtureInstruction{{Program: "program", Data: hex.EncodeToString(data),
				Accounts: []sbftest.FixtureMeta{{Account: "value", Writable: true}, {Account: "value", Writable: true}}}},
			Expect: expect}}})
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			cu := map[string][]uint64{}
			for _, v := range []struct {
				name      string
				threshold uint64
			}{{"chosen", memorySyscallMin}, {"loops", 1 << 62}} {
				dir := filepath.Join(t.TempDir(), v.name)
				elfPath := buildMemoryVariant(t, dir, llvm, profile, source, v.threshold)
				for _, c := range runMemorySuite(t, dir, elfPath, runner, profile, suite) {
					cu[c.Name] = append(cu[c.Name], c.Steps[0].CU)
				}
			}
			t.Logf("CU (chosen, loops): %v", cu)
		})
	}
}

// TestMemoryReadOnlyWritesSBF: copying into a read-only account's data
// fails with ReadonlyDataModified, and copying into an account's serialized
// key succeeds without effect, whether the move is a loop or a syscall, on
// both profiles.
func TestMemoryReadOnlyWritesSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	source := []byte(`package program

import "gosvm/solana"

// Instruction: target (0 data of account 0, 1 key of account 0), count.
func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	src := solana.Data(c, 1)
	dst := solana.Data(c, 0)
	if ix[0] == 1 {
		dst = solana.Key(c, 0)
	}
	return uint64(copy(dst[:ix[1]], src)) - uint64(ix[1])
}
`)
	program := base58(probeKey("gosvm memory read-only probe program; local tests only"))
	data := make([]byte, 64)
	for j := range data {
		data[j] = byte(j + 1)
	}
	state := func(d []byte) *sbftest.FixtureState {
		return &sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000}
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			results := map[string][]string{}
			for _, v := range []struct {
				name      string
				threshold uint64
			}{{"chosen", memorySyscallMin}, {"loops", 1 << 62}} {
				dir := filepath.Join(t.TempDir(), v.name)
				elfPath := buildMemoryVariant(t, dir, llvm, profile, source, v.threshold)
				suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program,
					PayerSeed: hex.EncodeToString(probeKey("gosvm memory read-only probe payer; never fund")),
					Accounts: []sbftest.FixtureAccount{
						{Name: "ro", Address: base58(probeKey("gosvm memory read-only target")), Initial: state(make([]byte, 64))},
						{Name: "src", Address: base58(probeKey("gosvm memory read-only source")), Initial: state(data)}}}
				for _, target := range []byte{0, 1} {
					for _, n := range []byte{1, 8, 32} {
						suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: fmt.Sprintf("target%d-n%d", target, n), Steps: []sbftest.FixtureStep{{
							Instructions: []sbftest.FixtureInstruction{{Program: "program", Data: hex.EncodeToString([]byte{target, n}),
								Accounts: []sbftest.FixtureMeta{{Account: "ro"}, {Account: "src"}}}},
							Expect: sbftest.FixtureExpectation{Error: readOnlyWriteError(profile, target), Accounts: []sbftest.FixtureCheck{{Account: "ro", State: state(make([]byte, 64))}}}}}})
					}
				}
				for _, c := range runMemorySuite(t, dir, elfPath, runner, profile, suite) {
					results[c.Name] = append(results[c.Name], string(c.Steps[0].Error))
				}
			}
			t.Logf("errors (chosen, loops): %v", results)
		})
	}
}

// readOnlyWriteError is the runtime's verdict on a write to a read-only
// account's data (target 0) or key (target 1), as the loops get it.
func readOnlyWriteError(profile string, target byte) json.RawMessage {
	if target == 0 {
		return json.RawMessage(`{"InstructionError":[0,"ReadonlyDataModified"]}`)
	}
	return json.RawMessage("null") // the runtime ignores the serialized key
}
