package project

import (
	"bytes"
	"fmt"
	"gosvm/internal/compiler"
	"gosvm/solana"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitSDK2AndLegacySnapshot(t *testing.T) {
	legacy := starter(t)
	b, err := os.ReadFile(filepath.Join(legacy, ".gosvm/sdk/solana/api.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, []byte(solana.LegacySource)) {
		t.Fatal("SDK 1 changed")
	}
	modern := filepath.Join(t.TempDir(), "sdk-two")
	if err := NewModuleSDK(modern, "example.org/sdk-two", MultiSchema, 2); err != nil {
		t.Fatal(err)
	}
	c := mustLoad(t, modern)
	if c.SDKVersion != 2 {
		t.Fatal(c.SDKVersion)
	}
	runNative(t, modern)
	for _, dir := range []string{legacy, modern} {
		p, err := compiler.ReadProgram(dir)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if dir == modern {
			want = 2
		}
		if p.SDK != want {
			t.Fatal("wrong API universe", p.SDK, want)
		}
	}
	probe := `package program
import("gosvm/solana";"gosvm/sdk/pda";"gosvm/sdk/cpi";"gosvm/sdk/token")
func Probe(c solana.Context)uint64{var seeds pda.Seeds;if seeds.AddUint64(42)!=0{return 1};var signer cpi.SingleSigner;if signer.AddByte(42)!=0{return 1};var metas cpi.Metas;if metas.Add(0,false,false)!=0{return 2};ref,code:=token.Load(c,0,false);if code!=0{return code};state,status:=token.Read(c,ref);if status!=0{return status};return state.Amount}
`
	if err := os.WriteFile(filepath.Join(modern, "probe.go"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Generate(modern); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(modern, ".gosvm/sdk/sdk/pda/pda.go")
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("\n// drift\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(modern); err == nil || !strings.Contains(err.Error(), "SDK differs") {
		t.Fatalf("helper snapshot drift accepted: %v", err)
	}
}

func TestSDK1RejectsNewBoundaryCalls(t *testing.T) {
	dir := starter(t)
	if err := os.WriteFile(filepath.Join(dir, "probe.go"), []byte("package program\nimport \"gosvm/solana\"\nfunc Probe(c solana.Context)uint64{return solana.Lamports(c,0)}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := compiler.ReadProgram(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.CompileProgram(p); err == nil || !strings.Contains(err.Error(), "Lamports") {
		t.Fatalf("legacy compiler admitted new API: %v", err)
	}
}

func TestSchema2RejectsCountsBeyondLoaderABI(t *testing.T) {
	dir := multiStarter(t)
	editMultiConfig(t, dir, func(c *Config) {
		for j := len(c.Instructions[0].Accounts); j < 17; j++ {
			c.Instructions[0].Accounts = append(c.Instructions[0].Accounts, AccountDeclaration{Name: fmt.Sprintf("Extra%d", j), Kind: "account", Access: "read"})
		}
	})
	if err := Generate(dir); err == nil || !strings.Contains(err.Error(), "1..16 declared accounts") {
		t.Fatal(err)
	}
}
