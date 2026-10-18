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

// An SDK-2 scaffold ships the host fixture builder, a generator whose output
// is the committed testdata/svm.json (runNative runs its staleness test), and
// typed client instructions. Older snapshots without the builder still load,
// and generation adds it.
func TestSDK2FixtureScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fixtures")
	if err := NewModuleSDK(dir, "example.org/fixtures", MultiSchema, 2); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"cmd/fixtures/main.go", "cmd/fixtures/main_test.go", "testdata/svm.json", ".gosvm/sdk/sdk/fixture/fixture.go"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatal(err)
		}
	}
	client, err := os.ReadFile(filepath.Join(dir, "client/zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func MoveInstruction(keys MoveKeys, args model0.MoveArgs) Instruction", "{Key: keys.Authority, Signer: true, Writable: false}", "ErrAccountRelation uint64 = 6007", "ErrMove uint64 = 1"} {
		if !strings.Contains(strings.Join(strings.Fields(string(client)), " "), want) {
			t.Fatalf("client lacks %q:\n%s", want, client)
		}
	}
	runNative(t, dir)
	builder := filepath.Join(dir, ".gosvm/sdk/sdk/fixture/fixture.go")
	if err := os.Remove(builder); err != nil {
		t.Fatal(err)
	}
	mustLoad(t, dir)
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(builder); err != nil || !bytes.Equal(b, sdkFiles(2)["sdk/fixture/fixture.go"]) {
		t.Fatalf("generation did not restore the fixture builder: %v", err)
	}
	if err := os.WriteFile(builder, []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "SDK differs") {
		t.Fatalf("fixture builder drift accepted: %v", err)
	}
	legacy := multiStarter(t)
	if _, err := os.Stat(filepath.Join(legacy, "cmd/fixtures")); !os.IsNotExist(err) {
		t.Fatalf("SDK-1 scaffold has a fixture generator: %v", err)
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
