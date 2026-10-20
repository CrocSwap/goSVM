package compiler

import (
	"context"
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

	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// TestKeccak256SBF builds testdata/keccak for fogo-v0 and solana-v3 and
// compares the runtime's Keccak-256 of whole inputs and subranges (0 to 10,240
// bytes, across the 136-byte rate boundary) with the reference, plus
// out-of-bounds requests that return 2003 without a syscall. It skips without
// the v1.57 backend or a runner. GOSVM_KECCAK_RESULTS=<dir> keeps the
// fixtures, ELFs and reports.
func TestKeccak256SBF(t *testing.T) {
	t.Parallel()
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	keep := os.Getenv("GOSVM_KECCAK_RESULTS")
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
			if err = os.WriteFile(filepath.Join(dir, "program.go"), mustRead(t, filepath.Join("testdata", "keccak", "program.go")), 0644); err != nil {
				t.Fatal(err)
			}
			elf := filepath.Join(dir, "program.so")
			if err = Build(dir, elf, llvm, profile); err != nil {
				t.Fatal(err)
			}
			program := base58(probeKey("gosvm keccak probe program; local tests only"))
			suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program,
				PayerSeed: hex.EncodeToString(probeKey("gosvm keccak probe payer; never fund"))}
			const resultBytes = 8 + 72
			state := func(d []byte) *sbftest.FixtureState {
				return &sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000}
			}
			preset := make([]byte, resultBytes)
			for i := 8; i < resultBytes; i++ {
				preset[i] = 0x5b
			}
			suite.Accounts = append(suite.Accounts, sbftest.FixtureAccount{Name: "result", Address: base58(probeKey("gosvm keccak result")), Initial: state(preset)})
			for _, size := range []int{0, 1, 64, 135, 136, 137, 1024, 10240} {
				input := make([]byte, size)
				for i := range input {
					input[i] = byte(i*13 + 1)
				}
				name := fmt.Sprintf("input-%d", size)
				suite.Accounts = append(suite.Accounts, sbftest.FixtureAccount{Name: name, Address: base58(probeKey("gosvm keccak " + name)), Initial: state(input)})
				add := func(label string, offset, length, outOffset uint64) {
					var ix [24]byte
					binary.LittleEndian.PutUint64(ix[:], offset)
					binary.LittleEndian.PutUint64(ix[8:], length)
					binary.LittleEndian.PutUint64(ix[16:], outOffset)
					after := append([]byte(nil), preset...)
					if offset > uint64(size) || length > uint64(size)-offset || outOffset > 72-32 {
						binary.LittleEndian.PutUint64(after, 2003)
					} else {
						copy(after[8+outOffset:], keccak256(input[offset:offset+length]))
					}
					suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: name + "-" + label, Steps: []sbftest.FixtureStep{{
						Instructions: []sbftest.FixtureInstruction{{Program: "program", Data: hex.EncodeToString(ix[:]),
							Accounts: []sbftest.FixtureMeta{{Account: name}, {Account: "result", Writable: true}}}},
						Expect: sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "result", State: state(after)}}}}}})
				}
				add("whole", 0, uint64(size), 0)
				if size > 2 {
					add("inner", 1, uint64(size-2), 40)
				}
				add("past-end", uint64(size), 1, 0)
				add("output-overflow", 0, uint64(size), 41)
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
			if len(parsed.Cases) != len(suite.Cases) {
				t.Fatalf("ran %d of %d cases", len(parsed.Cases), len(suite.Cases))
			}
			var summary strings.Builder
			for _, c := range parsed.Cases {
				fmt.Fprintf(&summary, "%s %d CU\n", c.Name, c.Steps[0].CU)
			}
			t.Logf("%d Keccak-256 scenarios passed\n%s", len(parsed.Cases), summary.String())
		})
	}
}
