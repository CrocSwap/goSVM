package compiler_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gosvm/internal/compiler"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/sdk/fixture"
)

// accounts64Module copies examples/accounts64 into a module with the SDK-2
// snapshot (from examples/tutorial), as a low-level build inside an SDK-2
// project sees it.
func accounts64Module(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source, err := os.ReadFile("../../examples/accounts64/program.go")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"program.go": source,
		"go.mod":     []byte("module example.org/accounts64\n\ngo 1.22\n\nrequire gosvm v0.0.0\n\nreplace gosvm => ./.gosvm/sdk\n"),
	}
	snapshot := "../../examples/tutorial/.gosvm"
	err = filepath.WalkDir(snapshot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel("../../examples/tutorial", path)
		files[rel], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range files {
		path := filepath.Join(dir, name)
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// legacyPacketBytes is the size of a legacy transaction with one signature
// (the payer) for one instruction over these accounts and data.
func legacyPacketBytes(accounts []fixture.AccountMeta, data []byte) int {
	keys := map[[32]byte]bool{}
	for _, a := range accounts {
		keys[a.Key] = true
	}
	// signatures, header, keys (plus the program), blockhash, one
	// instruction (program index, account indices, data); compact-u16
	// lengths are one byte here.
	return 1 + 64 + 3 + 1 + 32*(len(keys)+1) + 32 + 1 + 1 + 1 + len(accounts) + 1 + len(data)
}

// The 64-account loader runs on both modern targets: 33 unique accounts in
// one legacy packet (Hyperlane's largest instruction), 64 descriptors with
// duplicates at and beyond index 32, 65 rejected, a System CPI whose program,
// payer and destination sit at indices 32 and above, and rollback. The same
// program without the opt-in rejects 33 accounts. Skips without platform-tools
// v1.57, and skips the runs (after building) without the managed runners.
func TestAccounts64SBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := compiler.ModernLLVMForTest()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	f := fixture.New("accounts64")
	payer := f.Payer()
	var owned []fixture.Account
	for j := 0; j < 32; j++ {
		owned = append(owned, f.Account(fmt.Sprintf("owned-%02d", j), f.Owned([]byte{0})))
	}
	recipient := f.Account("recipient", fixture.Wallet(1_000_000))
	meta := func(a fixture.Account, signer, writable bool) fixture.AccountMeta {
		return fixture.AccountMeta{Key: a.Key, Signer: signer, Writable: writable}
	}
	// ix is one instruction that fits a legacy packet with the payer's
	// signature.
	ix := func(mode byte, metas []fixture.AccountMeta, args ...byte) fixture.Instruction {
		data := append([]byte{mode, byte(len(metas))}, args...)
		if n := legacyPacketBytes(metas, data); n > 1232 {
			t.Fatalf("a %d-account instruction needs %d bytes, above the 1,232-byte packet", len(metas), n)
		}
		return fixture.Instruction{Accounts: metas, Data: data}
	}
	// bumped expects each owned account's first byte to count its writable
	// descriptors.
	bumped := func(s *fixture.Step, metas []fixture.AccountMeta) *fixture.Step {
		count := map[[32]byte]byte{}
		for _, m := range metas {
			if m.Writable {
				count[m.Key]++
			}
		}
		for _, a := range owned {
			if n := count[a.Key]; n != 0 {
				s.ExpectData(a, []byte{n})
			}
		}
		return s
	}

	// 33 unique accounts: the payer and the 32 owned accounts.
	unique := []fixture.AccountMeta{meta(payer, true, true)}
	for _, a := range owned {
		unique = append(unique, meta(a, false, true))
	}
	// 64 descriptors over the payer and 8 owned accounts; the duplicates
	// include indices 32..63.
	wide := []fixture.AccountMeta{meta(payer, true, true)}
	for j := 1; j < 64; j++ {
		wide = append(wide, meta(owned[j%8], false, true))
	}
	// A System transfer from the payer (index 33) to the recipient (index 40)
	// through the System program at index 63.
	var cpi []fixture.AccountMeta
	for j := 0; j < 64; j++ {
		switch j {
		case 33:
			cpi = append(cpi, meta(payer, true, true))
		case 40:
			cpi = append(cpi, meta(recipient, false, true))
		case 63:
			cpi = append(cpi, fixture.AccountMeta{Key: fixture.SystemProgram})
		default:
			cpi = append(cpi, meta(owned[j%4], false, false))
		}
	}
	amount := make([]byte, 8)
	binary.LittleEndian.PutUint64(amount, 250_000)

	bumped(f.Scenario("thirty-three-unique").Step(ix(0, unique)), unique)
	bumped(f.Scenario("sixty-four-with-duplicates").Step(ix(0, wide)), wide)
	f.Scenario("sixty-five-fails").Step(ix(0, append(wide, meta(owned[1], false, true)))).Fails(1001)
	f.Scenario("system-transfer-beyond-32").Step(ix(1, cpi, append([]byte{63, 33, 40}, amount...)...)).
		ExpectLamports(recipient, 1_250_000).ExpectInvocations(fixture.SystemProgram, 1)
	f.Scenario("failure-rolls-back").Step(ix(2, wide)).Fails(7002)
	wideSuite, err := f.JSON()
	if err != nil {
		t.Fatal(err)
	}

	g := fixture.New("accounts64")
	payer = g.Payer()
	owned = owned[:0]
	for j := 0; j < 32; j++ {
		owned = append(owned, g.Account(fmt.Sprintf("owned-%02d", j), g.Owned([]byte{0})))
	}
	narrow := unique[:32]
	bumped(g.Scenario("thirty-two-unique").Step(ix(0, narrow)), narrow)
	g.Scenario("thirty-three-fails").Step(ix(0, unique)).Fails(1001)
	defaultSuite, err := g.JSON()
	if err != nil {
		t.Fatal(err)
	}

	// The suites are committed for gosvm svm-test; GOSVM_UPDATE_FIXTURES=1
	// rewrites them.
	for name, suite := range map[string][]byte{"svm.json": wideSuite, "default.json": defaultSuite} {
		path := filepath.Join("../../examples/accounts64/testdata", name)
		if os.Getenv("GOSVM_UPDATE_FIXTURES") != "" {
			if err := os.WriteFile(path, suite, 0644); err != nil {
				t.Fatal(err)
			}
		}
		if committed, err := os.ReadFile(path); err != nil || !bytes.Equal(committed, suite) {
			t.Fatalf("%s is stale (%v); rerun with GOSVM_UPDATE_FIXTURES=1", path, err)
		}
	}
	dir := accounts64Module(t)
	for _, profile := range []string{target.Fogo, target.Solana} {
		for _, build := range []struct {
			name  string
			max   int
			suite []byte
		}{{"max-accounts-64", compiler.WideAccounts, wideSuite}, {"default", 0, defaultSuite}} {
			t.Run(profile+"/"+build.name, func(t *testing.T) {
				out := t.TempDir()
				elf := filepath.Join(out, "program.so")
				if err := compiler.BuildWithOptions(dir, elf, llvm, profile, compiler.BuildOptions{MaxAccounts: build.max}); err != nil {
					t.Fatal(err)
				}
				runner, err := testvm.RunnerFor("", profile)
				if err != nil {
					t.Skipf("built the %s ELF; runner unavailable: %v", profile, err)
				}
				suite := filepath.Join(out, "svm.json")
				if err = os.WriteFile(suite, build.suite, 0644); err != nil {
					t.Fatal(err)
				}
				var log bytes.Buffer
				err = sbftest.RunFast(context.Background(), out, elf, &log, sbftest.FastOptions{Runner: runner, Profile: profile,
					PlatformTools: target.ModernTools, Fixtures: suite, ReportPath: filepath.Join(out, "report.json")})
				if err != nil {
					t.Fatalf("%v\n%s", err, log.String())
				}
				t.Logf("%s", log.String())
			})
		}
	}
}

// Without its own frame, Process inlined into the entrypoint beside the
// 3,584-byte table overflows 4 KiB in the tutorial (4,224 bytes) and escrow
// (4,672); with -max-accounts 64 both build for both targets.
func TestAccounts64Frames(t *testing.T) {
	t.Parallel()
	llvm := compiler.ModernLLVMForTest()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		for _, example := range []string{"tutorial", "escrow"} {
			elf := filepath.Join(t.TempDir(), "program.so")
			if err := compiler.BuildWithOptions("../../examples/"+example, elf, llvm, profile, compiler.BuildOptions{MaxAccounts: compiler.WideAccounts}); err != nil {
				t.Fatalf("%s %s: %v", profile, example, err)
			}
		}
	}
}
