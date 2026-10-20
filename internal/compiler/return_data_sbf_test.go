package compiler

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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

type returnDataCase struct {
	name     string
	ixs      [][]byte // one transaction; the last instruction's read is asserted
	capacity int
	size     int    // expected full size
	copied   []byte // expected output bytes; the rest stay zero
	setter   bool   // expected setter is this program (else untouched zeros)
	trap     bool
}

func returnDataIx(mode byte, capacity, size int) []byte {
	return []byte{mode, byte(capacity), byte(capacity >> 8), byte(size), byte(size >> 8)}
}

func returnDataCases() []returnDataCase {
	pattern := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i*29 + 3)
		}
		return b
	}
	var cases []returnDataCase
	for _, size := range []int{0, 1, 32, 1024} {
		for _, capacity := range []int{0, 1, 32, 1024} {
			c := returnDataCase{name: "cpi-" + itoa(size) + "-into-" + itoa(capacity), capacity: capacity, size: size,
				ixs: [][]byte{returnDataIx(1, capacity, size)}}
			n := min(size, capacity)
			c.copied, c.setter = pattern(n), n != 0
			cases = append(cases, c)
		}
	}
	return append(cases,
		returnDataCase{name: "own-setter", capacity: 64, size: 20, copied: pattern(20), setter: true, ixs: [][]byte{returnDataIx(3, 64, 20)}},
		returnDataCase{name: "overlap-traps", capacity: 64, trap: true, ixs: [][]byte{returnDataIx(7, 64, 1)}},
		returnDataCase{name: "partial-overlap-traps", capacity: 64, trap: true, ixs: [][]byte{returnDataIx(8, 64, 4)}},
		returnDataCase{name: "empty-overlap-allowed", capacity: 64, ixs: [][]byte{returnDataIx(7, 64, 0)}},
		returnDataCase{name: "set-then-silent-cpi-clears", capacity: 64, ixs: [][]byte{returnDataIx(9, 64, 0)}},
		returnDataCase{name: "silent-callee", capacity: 64, ixs: [][]byte{returnDataIx(6, 64, 0)}},
		// The runtime clears return data before each top-level instruction:
		// the second reads size 0 and leaves the first's output and setter.
		returnDataCase{name: "fresh-instruction", capacity: 64, copied: pattern(9), setter: true, ixs: [][]byte{returnDataIx(3, 64, 9), returnDataIx(4, 64, 0)}},
		returnDataCase{name: "short-program-traps", capacity: 64, trap: true, ixs: [][]byte{returnDataIx(5, 64, 0)}},
	)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// TestGetReturnDataSBF builds testdata/returndata for fogo-v0 and solana-v3
// and reads return data set by a real self-CPI, the program itself, a silent
// callee and an earlier instruction, with prefix copies and a trap. It skips
// without the v1.57 backend or a runner. GOSVM_RETURN_DATA_RESULTS=<dir>
// keeps the fixtures, ELFs and reports.
func TestGetReturnDataSBF(t *testing.T) {
	t.Parallel()
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	keep := os.Getenv("GOSVM_RETURN_DATA_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			dir := t.TempDir()
			if keep != "" {
				dir = filepath.Join(keep, profile)
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.WriteFile(filepath.Join(dir, "program.go"), mustRead(t, filepath.Join("testdata", "returndata", "program.go")), 0644); err != nil {
				t.Fatal(err)
			}
			elf := filepath.Join(dir, "program.so")
			if err = Build(dir, elf, llvm, profile); err != nil {
				t.Fatal(err)
			}
			cases := returnDataCases()
			programKey := probeKey("gosvm return-data probe program; local tests only")
			program := base58(programKey)
			suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program,
				PayerSeed: hex.EncodeToString(probeKey("gosvm return-data probe payer; never fund"))}
			for _, c := range cases {
				before := make([]byte, 40+c.capacity)
				after := append([]byte(nil), before...)
				binary.LittleEndian.PutUint64(after, uint64(c.size))
				if c.setter {
					copy(after[8:40], programKey)
				}
				copy(after[40:], c.copied)
				name := "result-" + c.name
				suite.Accounts = append(suite.Accounts, sbftest.FixtureAccount{Name: name, Address: base58(probeKey("gosvm return-data " + c.name)),
					Initial: &sbftest.FixtureState{Data: hex.EncodeToString(before), Owner: program, Lamports: 1_000_000_000}})
				step := sbftest.FixtureStep{Expect: sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: name,
					State: &sbftest.FixtureState{Data: hex.EncodeToString(after), Owner: program, Lamports: 1_000_000_000}}}}}
				if c.trap {
					step.Expect.Error = json.RawMessage(`{"InstructionError":[0,"ProgramFailedToComplete"]}`)
					step.Expect.Accounts[0].State.Data = hex.EncodeToString(before)
				}
				for _, ix := range c.ixs {
					step.Instructions = append(step.Instructions, sbftest.FixtureInstruction{Program: "program", Data: hex.EncodeToString(ix),
						Accounts: []sbftest.FixtureMeta{{Account: name, Writable: true}, {Account: "program"}}})
				}
				suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: c.name, Steps: []sbftest.FixtureStep{step}})
			}
			fixtures, err := json.MarshalIndent(suite, "", "  ")
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
				t.Fatalf("%v\n%s", err, log.String())
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
			var summary bytes.Buffer
			for _, c := range parsed.Cases {
				summary.WriteString(c.Name + " " + itoa(int(c.Steps[0].CU)) + " CU\n")
			}
			t.Logf("%d return-data scenarios passed\n%s", len(cases), summary.String())
		})
	}
}
