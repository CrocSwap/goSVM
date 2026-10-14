package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"gosvm/internal/project"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// fakeProgramHeader returns a 64-byte ELF64 program header accepted by
// Profile.ValidateProgram for the named profile; it is not runnable.
func fakeProgramHeader(t *testing.T, name string) []byte {
	t.Helper()
	p, err := target.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	copy(b, "\x7fELF")
	b[4], b[5] = 2, 1
	binary.LittleEndian.PutUint16(b[16:], 3)
	machine := uint16(263)
	if p.Name == target.Solana {
		machine = 247
	}
	binary.LittleEndian.PutUint16(b[18:], machine)
	binary.LittleEndian.PutUint32(b[48:], p.Flags)
	if err = p.ValidateProgram(b); err != nil {
		t.Fatalf("fake header rejected: %v", err)
	}
	return b
}

func newTargetProject(t *testing.T, targets string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "swap")
	if e := project.NewModuleTarget(dir, "", project.Schema, 1, targets); e != nil {
		t.Fatal(e)
	}
	return dir
}

func TestExternalELFRequiresSVM(t *testing.T) {
	e := projectCommand([]string{"test", "-elf", "/absent.so"})
	if e == nil || !strings.Contains(e.Error(), "require -svm") {
		t.Fatalf("-elf without -svm: %v", e)
	}
}

func TestExternalELFRequiresOneTarget(t *testing.T) {
	dir := newTargetProject(t, "fogo,solana")
	elf := filepath.Join(t.TempDir(), "other.so")
	if e := os.WriteFile(elf, fakeProgramHeader(t, target.Solana), 0644); e != nil {
		t.Fatal(e)
	}
	e := projectCommand([]string{"test", "--svm", "-dir", dir, "-elf", elf})
	if e == nil || !strings.Contains(e.Error(), "add -target") || !strings.Contains(e.Error(), target.Fogo) {
		t.Fatalf("multi-target -elf without -target: %v", e)
	}
}

func TestExternalELFHeaderMustMatchTarget(t *testing.T) {
	dir := newTargetProject(t, "fogo,solana")
	elf := filepath.Join(t.TempDir(), "fogo.so")
	if e := os.WriteFile(elf, fakeProgramHeader(t, target.Fogo), 0644); e != nil {
		t.Fatal(e)
	}
	e := projectCommand([]string{"test", "--svm", "-dir", dir, "-target", "solana", "-elf", elf})
	if e == nil || !strings.Contains(e.Error(), "does not match target "+target.Solana) {
		t.Fatalf("fogo-v0 ELF accepted as solana-v3: %v", e)
	}
	// The header check runs before generation, native tests, and any build.
	if _, err := os.Stat(filepath.Join(dir, "build", target.Solana)); !os.IsNotExist(err) {
		t.Fatalf("mismatched ELF still started a build: %v", err)
	}
}

// TestExternalELFMatchesBuiltProgram builds a single-target project normally,
// then tests a copy of its program through -elf and compares the reports.
func TestExternalELFMatchesBuiltProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs an SBF program")
	}
	p, _ := target.Resolve(target.Solana)
	if e := p.CheckBackend(llvmForTools(p.Tools)); e != nil {
		t.Skipf("solana-v3 backend unavailable: %v", e)
	}
	if _, e := testvm.RunnerFor("", p.Name); e != nil {
		t.Skipf("solana-v3 runner unavailable: %v", e)
	}
	dir := newTargetProject(t, "solana")
	var goArgs []string
	if runtime.GOOS == "darwin" {
		goArgs = []string{"--", "-ldflags=-linkmode=external"}
	}
	if e := projectCommand(append([]string{"test", "--svm", "-dir", dir}, goArgs...)); e != nil {
		t.Fatal(e)
	}
	built, err := filepath.Glob(filepath.Join(dir, "build", p.Name, "*", "*", "deps-*", "svm-results.json"))
	if err != nil || len(built) != 1 {
		t.Fatalf("built report: %v %v", built, err)
	}
	want := readReport(t, built[0])
	program, err := os.ReadFile(filepath.Join(dir, "build/solana/program.so"))
	if err != nil {
		t.Fatal(err)
	}
	elf := filepath.Join(t.TempDir(), "other.so")
	if err = os.WriteFile(elf, program, 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(filepath.Join(dir, "build")); err != nil {
		t.Fatal(err)
	}
	if e := projectCommand(append([]string{"test", "--svm", "-dir", dir, "-elf", elf}, goArgs...)); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dir, "build/solana/program.so")); !os.IsNotExist(e) {
		t.Fatalf("-elf compiled the project: %v", e)
	}
	got := readReport(t, externalReportPath(dir, p))
	if got["elf_source"] != "external" || want["elf_source"] != nil {
		t.Fatalf("elf_source: built %v external %v", want["elf_source"], got["elf_source"])
	}
	for _, field := range []string{"elf_sha256", "cases", "target_profile", "runner_sha256", "fixtures_sha256"} {
		if !reflect.DeepEqual(want[field], got[field]) {
			t.Fatalf("%s differs: built %v external %v", field, want[field], got[field])
		}
	}
}

func readReport(t *testing.T, path string) map[string]any {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	return m
}
