package project

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func starter(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "swap")
	if e := New(dir); e != nil {
		t.Fatal(e)
	}
	return dir
}
func TestStandaloneStarter(t *testing.T) {
	dir := starter(t)
	if e := New(dir); e == nil {
		t.Fatal("overwrote existing project")
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("standalone offline native tests: %v\n%s", e, out)
	}
	for _, p := range []string{"zz_gosvm.go", "client/zz_gosvm.go", "idl.json"} {
		path := filepath.Join(dir, p)
		before, e := os.Stat(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = Generate(dir); e != nil {
			t.Fatal(e)
		}
		after, _ := os.Stat(path)
		if !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("unchanged generation rewrites %s", p)
		}
	}
	b, e := os.ReadFile(filepath.Join(dir, "idl.json"))
	if e != nil {
		t.Fatal(e)
	}
	var idl struct {
		Account     layout
		Instruction layout
	}
	if e = json.Unmarshal(b, &idl); e != nil {
		t.Fatal(e)
	}
	if idl.Account.Size != 32 || idl.Instruction.Size != 24 || idl.Account.Fields[2].Offset != 24 {
		t.Fatalf("wrong wire layout: %+v", idl)
	}
}
func TestGenerationChecksBeforeWriting(t *testing.T) {
	dir := starter(t)
	path := filepath.Join(dir, "zz_gosvm.go")
	before, _ := os.ReadFile(path)
	if e := os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package program\nvar Mutable uint64\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Generate(dir); e == nil || !strings.Contains(e.Error(), "package scope") {
		t.Fatalf("bad source accepted: %v", e)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed generation replaced existing code")
	}
}
func TestSDKDriftRejected(t *testing.T) {
	dir := starter(t)
	path := filepath.Join(dir, ".gosvm/sdk/solana/api.go")
	if e := os.WriteFile(path, []byte("package solana"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(dir); e == nil || !strings.Contains(e.Error(), "SDK differs") {
		t.Fatalf("SDK drift accepted: %v", e)
	}
}
func TestWireFieldsAndSchemaChanges(t *testing.T) {
	dir := starter(t)
	path := filepath.Join(dir, "program.go")
	b, _ := os.ReadFile(path)
	// Test an unaligned mixed-width wire layout rather than relying on Go struct layout.
	b = regexp.MustCompile(`ReserveX[ \t]+uint64`).ReplaceAll(b, []byte("Tag byte\n Epoch uint32\n ReserveX uint64"))
	if !bytes.Contains(b, []byte("Tag byte")) {
		t.Fatal("template changed")
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if e := Generate(dir); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "idl.json"))
	var idl struct{ Account layout }
	json.Unmarshal(raw, &idl)
	if idl.Account.Size != 37 || idl.Account.Fields[2].Offset != 13 {
		t.Fatalf("wire padding leaked: %+v", idl.Account)
	}
	// The native client must still compile after adding byte and uint32 fields.
	cmd := exec.Command("go", "test", "./client")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("mixed-width client: %v\n%s", e, out)
	}
}

func TestCustomModuleAndDiscovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom-swap")
	if e := NewModule(dir, "example.org/protocol/swap/v2"); e != nil {
		t.Fatal(e)
	}
	found, e := Find(filepath.Join(dir, "client"))
	if e != nil || found != dir {
		t.Fatalf("find: %s %v", found, e)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("custom module: %v %s", e, b)
	}
	nested := filepath.Join(dir, "nested")
	os.Mkdir(nested, 0700)
	os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module separate\n"), 0600)
	if _, e = Find(nested); e == nil {
		t.Fatal("crossed nested module boundary")
	}
	for _, m := range []string{"gosvm", "../escape", "has space", "a//b", "/absolute", "a/../b", "a\\b"} {
		if e = NewModule(filepath.Join(t.TempDir(), "invalid"), m); e == nil {
			t.Fatalf("accepted invalid module %q", m)
		}
	}
}

func TestGoModuleSyntax(t *testing.T) {
	for _, text := range []string{
		"module example.org/swap\nreplace gosvm => ./.gosvm/sdk\n",
		"module \"example.org/swap\" // comment\nreplace (\n gosvm => \"./.gosvm/sdk\"\n example.org/host => ../host\n)\n",
	} {
		if e := checkModule(text, "example.org/swap"); e != nil {
			t.Fatal(e)
		}
	}
	for _, text := range []string{
		"module example.org/swap\n// replace gosvm => ./.gosvm/sdk\n",
		"module wrong\nreplace gosvm => ./.gosvm/sdk\n",
		"module example.org/swap\nreplace gosvm => ./.gosvm/sdk\nreplace gosvm v0.0.0 => ../other\n",
	} {
		if e := checkModule(text, "example.org/swap"); e == nil {
			t.Fatal("accepted conflicting module/SDK directives")
		}
	}
}
