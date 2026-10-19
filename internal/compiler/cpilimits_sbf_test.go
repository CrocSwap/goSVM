package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cpilimits "gosvm/examples/cpi-limits"
	"gosvm/internal/sbftest"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
	"gosvm/sdk/fixture"
)

// cpiLimitPlain is the number of distinct plain accounts after the callee and
// the four signer PDAs: 29 incoming accounts in all, within the SDK-2
// decoder's 32, and 22 distinct metas fit without repeating one.
const cpiLimitPlain = 24

// cpiLimitCase is one CPI: mode, metas and signer groups. code is the
// caller's expected custom error (0: the CPI succeeds and the callee, which
// asserts its account count and signers, runs once).
type cpiLimitCase struct {
	name          string
	mode          byte
	metas, groups int
	code          uint64
}

// cpiLimitCases are the SBF cases: the old limits, Hyperlane's 17 and 22
// metas, the 64-meta and 4-group maxima, and one past each through the
// builders (3002, 3003) and the raw boundary (2006, 2007).
func cpiLimitCases() []cpiLimitCase {
	b, r := cpilimits.ModeBuilder, cpilimits.ModeRaw
	return []cpiLimitCase{
		{"builder-3-metas", b, 3, 0, 0},
		{"builder-3-metas-1-group", b, 3, 1, 0},
		{"builder-16-metas", b, 16, 0, 0},
		{"builder-16-metas-2-groups", b, 16, 2, 0},
		{"builder-17-metas", b, 17, 0, 0},
		{"builder-17-metas-2-groups", b, 17, 2, 0},
		{"builder-22-metas", b, 22, 0, 0},
		{"builder-22-metas-4-groups", b, 22, 4, 0},
		{"builder-32-metas-4-groups", b, 32, 4, 0},
		{"builder-64-metas", b, 64, 0, 0},
		{"builder-64-metas-4-groups", b, 64, 4, 0},
		{"builder-65-metas", b, 65, 0, 3002},
		{"builder-5-groups", b, 8, 5, 3003},
		{"raw-3-metas", r, 3, 0, 0},
		{"raw-16-metas-2-groups", r, 16, 2, 0},
		{"raw-22-metas-4-groups", r, 22, 4, 0},
		{"raw-64-metas-4-groups", r, 64, 4, 0},
		{"raw-65-metas", r, 65, 0, 2006},
		{"raw-5-groups", r, 8, 5, 2007},
	}
}

// cpiLimitFixtures writes the suite: the caller is the program under test and
// the callee is loaded from callee.so beside the fixture file.
func cpiLimitFixtures(cases []cpiLimitCase, calleeSHA string) ([]byte, error) {
	f := fixture.New("cpi limits")
	callee := f.Program("callee", f.KeyFor("callee"), "callee.so", calleeSHA)
	var pdas []fixture.Account
	var bumps []byte
	for g := 0; g < cpilimits.MaxRawGroups; g++ {
		seeds := [][]byte{[]byte("cpi-limits"), {byte(g)}}
		if g < 4 {
			pdas = append(pdas, f.PDA(fmt.Sprintf("signer-%d", g), seeds...))
			bumps = append(bumps, pdas[g].Bump)
			continue
		}
		_, bump, err := fixture.FindProgramAddress(f.ProgramID(), seeds...)
		if err != nil {
			return nil, err
		}
		bumps = append(bumps, bump)
	}
	accounts := []fixture.AccountMeta{{Key: callee.Key}}
	for _, p := range pdas {
		accounts = append(accounts, fixture.AccountMeta{Key: p.Key})
	}
	for j := 0; j < cpiLimitPlain; j++ {
		a := f.Account(fmt.Sprintf("plain-%02d", j), fixture.Wallet(1_000_000))
		accounts = append(accounts, fixture.AccountMeta{Key: a.Key})
	}
	for _, c := range cases {
		data := append([]byte{c.mode, byte(c.metas), byte(c.groups)}, bumps[:c.groups]...)
		step := f.Scenario(c.name).Step(fixture.Instruction{Accounts: accounts, Data: data})
		if c.code != 0 {
			step.Fails(c.code).ExpectInvocations(callee.Key, 0)
		} else {
			step.ExpectInvocations(callee.Key, 1)
		}
	}
	// The callee's own check: three accounts where it was told four.
	f.Scenario("callee-rejects-wrong-count").Step().
		Invoke(callee.Key, fixture.Instruction{Accounts: accounts[1:4], Data: []byte{4, 0, 3}}).
		FailsWith(fixture.InstructionError(0, fixture.Custom(5004)))
	// Three references to one account where three distinct ones are expected.
	f.Scenario("callee-rejects-wrong-duplicate").Step().
		Invoke(callee.Key, fixture.Instruction{Accounts: []fixture.AccountMeta{accounts[5], accounts[5], accounts[5]}, Data: []byte{3, 0, 3}}).
		FailsWith(fixture.InstructionError(0, fixture.Custom(5601)))
	return f.JSON()
}

// buildCPICallee compiles examples/cpi-limits/callee.c.txt (no syscalls) with
// the profile's backend flags and linker script.
func buildCPICallee(t *testing.T, llvm, profile, dir string) string {
	t.Helper()
	p, err := target.Resolve(profile)
	if err != nil {
		t.Fatal(err)
	}
	cpu, script := "generic", linkerScript
	if p.Arch == "v3" {
		cpu, script = "v3", linkerScriptModernV3
	}
	source, object, elf := filepath.Join(dir, "callee.c"), filepath.Join(dir, "callee.o"), filepath.Join(dir, "callee.so")
	if err = os.WriteFile(source, mustRead(t, "../../examples/cpi-limits/callee.c.txt"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "callee.ld"), script, 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{filepath.Join(llvm, "bin", "clang"), "-target", "sbf", "-mcpu=" + cpu, "-O2", "-fno-builtin", "-fPIC", "-fno-stack-protector", "-std=c11", "-Werror", "-c", source, "-o", object},
		{filepath.Join(llvm, "bin", "ld.lld"), "-z", "notext", "-shared", "--Bdynamic", "--strip-all", "--no-undefined", "--entry", "entrypoint", "--script", filepath.Join(dir, "callee.ld"), "-o", elf, object},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
	return elf
}

// TestCPILimitsSBF builds examples/cpi-limits for fogo-v0 and solana-v3 and
// runs every limit case against a callee that asserts its account count and
// signers. Skips without the v1.57 backend or a runner (make test).
// GOSVM_CPI_RESULTS=<dir> keeps the ELFs, fixtures, reports and per-case CU.
func TestCPILimitsSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	keep := os.Getenv("GOSVM_CPI_RESULTS")
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
			elf := filepath.Join(dir, "caller.so")
			// The repository has no SDK snapshot; select SDK 2 (32 incoming
			// accounts) as an SDK-2 project's snapshot would.
			program, err := readProgramWith("../../examples/cpi-limits", BuildOptions{})
			if err != nil {
				t.Fatal(err)
			}
			program.SDK = 2
			if err = buildProgram(program, elf, llvm, profile); err != nil {
				t.Fatal(err)
			}
			callee := buildCPICallee(t, llvm, profile, dir)
			cases := cpiLimitCases()
			fixtures, err := cpiLimitFixtures(cases, fmt.Sprintf("%x", sha256.Sum256(mustRead(t, callee))))
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
			if err = sbftest.RunGeneral(ctx, elf, fixturePath, report, &log, sbftest.FastOptions{Runner: runner, Profile: p.Name, PlatformTools: p.Tools}); err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			var parsed struct {
				Cases []sbftest.FixtureCaseResult `json:"cases"`
			}
			if err = json.Unmarshal(mustRead(t, report), &parsed); err != nil {
				t.Fatal(err)
			}
			if len(parsed.Cases) != len(cases)+2 {
				t.Fatalf("ran %d of %d cases", len(parsed.Cases), len(cases)+2)
			}
			summary := map[string]any{"profile": profile, "caller_elf_sha256": fmt.Sprintf("%x", sha256.Sum256(mustRead(t, elf)))}
			cu := map[string]uint64{}
			for _, c := range parsed.Cases {
				cu[c.Name] = c.Steps[0].CU
			}
			summary["cu"] = cu
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s", encoded)
			if keep != "" {
				if err = os.WriteFile(filepath.Join(dir, "summary.json"), append(encoded, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
