package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gosvm/internal/sbftest"
	target "gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/sdk/fixture"
)

// splImage is an SPL program image bundled with the vendored LiteSVM 0.8.2.
// SDK probes declare it explicitly, so both runner profiles execute the same
// pinned ELF rather than whichever version a runner bundles.
type splImage struct{ name, address, elf, sha256 string }

var (
	splTokenImage     = splImage{"spl-token", "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "spl_token-3.5.0.so", "7b44674b9704fa7640fbebfac68389f338b500b8c2bc29175c27ed66838b5a3c"}
	splToken2022Image = splImage{"spl-token-2022", "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb", "spl_token_2022-8.0.0.so", "d2f8858d3204d0f47cef71a419048380dee8e88da4c63cf207d61d486555d240"}
	splATAImage       = splImage{"spl-ata", "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL", "spl_associated_token_account-1.1.1.so", "6804554e69fd3a58caa191dc4a58f4c67223d30ca28ab8987f39fc18d2f7374d"}
)

// probeAddress is create_program_address for one seed list, or false on curve.
func probeAddress(program fixture.Key, seeds ...[]byte) (fixture.Key, bool) {
	h := sha256.New()
	for _, s := range seeds {
		h.Write(s)
	}
	h.Write(program[:])
	h.Write([]byte("ProgramDerivedAddress"))
	var k fixture.Key
	copy(k[:], h.Sum(nil))
	return k, !fixture.OnCurve(k)
}

// splMintData is an initialized base mint (82 bytes, the same layout in Token
// and Token-2022) with an optional mint authority and no freeze authority.
func splMintData(authority *fixture.Key, supply uint64, decimals byte) []byte {
	b := make([]byte, 82)
	if authority != nil {
		b[0] = 1
		copy(b[4:], authority[:])
	}
	binary.LittleEndian.PutUint64(b[36:], supply)
	b[44], b[45] = decimals, 1
	return b
}

// splAccountData is an initialized base token account (165 bytes) without
// delegate, native amount or close authority.
func splAccountData(mint, owner fixture.Key, amount uint64) []byte {
	b := make([]byte, 165)
	copy(b, mint[:])
	copy(b[32:], owner[:])
	binary.LittleEndian.PutUint64(b[64:], amount)
	b[108] = 1
	return b
}

// tlvEntry is one Token-2022 extension: type and length (little-endian
// uint16), then the value.
func tlvEntry(kind uint16, value []byte) []byte {
	b := make([]byte, 4, 4+len(value))
	binary.LittleEndian.PutUint16(b, kind)
	binary.LittleEndian.PutUint16(b[2:], uint16(len(value)))
	return append(b, value...)
}

// withExtensions is Token-2022's extended layout: the base state zero-padded
// to 165 bytes, the account type (1 mint, 2 account) and TLV entries.
func withExtensions(base []byte, accountType byte, entries ...[]byte) []byte {
	b := append(append(make([]byte, 0, 166), base...), make([]byte, 165-len(base))...)
	b = append(b, accountType)
	for _, e := range entries {
		b = append(b, e...)
	}
	return b
}

// declare adds the image to a fixture written to examples/<name>/testdata.
func (s splImage) declare(f *fixture.Fixture) fixture.Account {
	return f.Program(s.name, fixture.MustParseKey(s.address), "../../../third_party/litesvm-0.8.2/src/programs/elf/"+s.elf, s.sha256)
}

// runSDKProbe checks that examples/<example>/testdata/svm.json is generate's
// output (GOSVM_UPDATE_FIXTURES=1 rewrites it), then builds the example for
// fogo-v0 and solana-v3 and runs every scenario through the pinned runner.
// The SBF part skips in -short mode or without the v1.57 backend or a runner.
// GOSVM_SDK_PROBE_RESULTS=<dir> keeps ELFs and reports.
func runSDKProbe(t *testing.T, example string, generate func() ([]byte, error)) {
	t.Helper()
	encoded, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("../../examples", example)
	path, err := filepath.Abs(filepath.Join(source, "testdata/svm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GOSVM_UPDATE_FIXTURES") != "" {
		if err = os.MkdirAll(filepath.Dir(path), 0755); err == nil {
			err = os.WriteFile(path, encoded, 0644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(mustRead(t, path), encoded) {
		t.Fatalf("%s is stale; regenerate with GOSVM_UPDATE_FIXTURES=1", path)
	}
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	var suite struct {
		Cases []json.RawMessage `json:"cases"`
	}
	if err = json.Unmarshal(encoded, &suite); err != nil {
		t.Fatal(err)
	}
	keep := os.Getenv("GOSVM_SDK_PROBE_RESULTS")
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			dir := t.TempDir()
			if keep != "" {
				dir = filepath.Join(keep, example, profile)
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
			}
			elf := filepath.Join(dir, "program.so")
			if err = BuildWithOptions(source, elf, llvm, profile, BuildOptions{}); err != nil {
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
			if err = sbftest.RunGeneral(ctx, elf, path, report, &log, sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
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
			for _, c := range parsed.Cases {
				units := []uint64{}
				for _, s := range c.Steps {
					units = append(units, s.CU)
				}
				t.Logf("%s: CU %v", c.Name, units)
			}
		})
	}
}
