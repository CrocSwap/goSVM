package compiler

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// The standalone memmove helper must compile on its own: it once used u64
// without defining it, so every library-free program that LLVM lowered to a
// memmove call failed to build.
func TestMemmoveHelperCompiles(t *testing.T) {
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang required")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "memmove.c")
	if err = os.WriteFile(file, []byte(memmoveHelper()), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-std=c11", "-ffreestanding", "-Werror", "-c", file, "-o", filepath.Join(dir, "memmove.o")).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

// TestMemmoveSBF builds testdata/memmove, whose large pointer-to-pointer
// copies LLVM lowers to memmove, for fogo-v0 and solana-v3 and checks the
// copied account bytes, including a self-assignment, through the runners.
func TestMemmoveSBF(t *testing.T) {
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			dir := t.TempDir()
			if err = os.WriteFile(filepath.Join(dir, "program.go"), mustRead(t, filepath.Join("testdata", "memmove", "program.go")), 0644); err != nil {
				t.Fatal(err)
			}
			elf := filepath.Join(dir, "program.so")
			if err = Build(dir, elf, llvm, profile); err != nil {
				t.Fatal(err)
			}
			program := base58(probeKey("gosvm memmove probe program; local tests only"))
			before := make([]byte, 537)
			for i := range before[:529] {
				before[i] = byte(i*7 + 3)
			}
			binary.LittleEndian.PutUint64(before[529:], 41)
			state := func(d []byte) *sbftest.FixtureState {
				return &sbftest.FixtureState{Data: hex.EncodeToString(d), Owner: program, Lamports: 1_000_000_000}
			}
			suite := sbftest.FixtureSuite{Format: sbftest.FixtureFormat, ProgramID: program,
				PayerSeed: hex.EncodeToString(probeKey("gosvm memmove probe payer; never fund")),
				Accounts:  []sbftest.FixtureAccount{{Name: "value", Address: base58(probeKey("gosvm memmove value")), Initial: state(before)}}}
			for _, add := range []byte{0, 1, 200} {
				after := append([]byte(nil), before...)
				after[0] += add
				binary.LittleEndian.PutUint64(after[529:], 42)
				suite.Cases = append(suite.Cases, sbftest.FixtureCase{Name: "add-" + strconv.Itoa(int(add)), Steps: []sbftest.FixtureStep{{
					Instructions: []sbftest.FixtureInstruction{{Program: "program", Data: hex.EncodeToString([]byte{add}),
						Accounts: []sbftest.FixtureMeta{{Account: "value", Writable: true}}}},
					Expect: sbftest.FixtureExpectation{Error: json.RawMessage("null"), Accounts: []sbftest.FixtureCheck{{Account: "value", State: state(after)}}}}}})
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
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			var log strings.Builder
			if err = sbftest.RunGeneral(ctx, elf, fixturePath, filepath.Join(dir, "svm-results.json"), io.Writer(&log), sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
		})
	}
}
