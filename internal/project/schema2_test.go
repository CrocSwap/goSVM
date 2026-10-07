package project

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func multiStarter(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "multi-state")
	if err := NewModuleSchema(dir, "example.org/multi-state", MultiSchema); err != nil {
		t.Fatal(err)
	}
	return dir
}

func editMultiConfig(t *testing.T, dir string, edit func(*Config)) {
	t.Helper()
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	edit(&c)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gosvm.json"), append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func runNative(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native tests: %v\n%s", err, out)
	}
}

func TestSchema2StandaloneCanonicalLayouts(t *testing.T) {
	dir := multiStarter(t)
	runNative(t, dir)
	model, err := resolveMulti(dir, mustLoad(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if model.States[0].Type != "example.org/multi-state/model.Vault" || model.States[0].Size != 61 || model.States[0].Fields[1].Fields[1].Offset != 41 || model.Arguments[0].Size != 16 {
		t.Fatalf("packed canonical model: %+v", model.States)
	}
	for _, name := range []string{"zz_gosvm.go", "client/zz_gosvm.go", "idl.json", "layouts.json"} {
		path := filepath.Join(dir, name)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := Generate(dir); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("rewrote unchanged %s", name)
		}
	}
	client, err := os.ReadFile(filepath.Join(dir, "client/zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(client, []byte("gosvm/solana")) || bytes.Contains(client, []byte("\"example.org/multi-state\"")) || !bytes.Contains(client, []byte("model0.Vault")) {
		t.Fatalf("client lost canonical pure imports: %s", client)
	}
	// An independent consumer has only a normal replacement for this unpublished
	// module, no replacement for gosvm or access to its compiler/runtime package.
	service := filepath.Join(t.TempDir(), "service")
	if err := os.Mkdir(service, 0700); err != nil {
		t.Fatal(err)
	}
	mod := "module independent.example/service\n\ngo 1.22\n\nrequire example.org/multi-state v0.0.0\nreplace example.org/multi-state => " + dir + "\n"
	if err := os.WriteFile(filepath.Join(service, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	source := `package service
import("testing";"example.org/multi-state/model";"example.org/multi-state/client")
func TestReuse(t *testing.T) { v:=model.Vault{Balance:123}; got,e:=client.DecodeAccountVault(client.EncodeAccountVault(v)); if e!=nil || got!=v || !model.CanMove(got.Balance,5,10,50) { t.Fatal(got,e) }; a:=model.MoveArgs{Amount:10}; b,e:=client.DecodeInstructionMove(client.EncodeInstructionMove(a)); if e!=nil || a!=b {t.Fatal(b,e)} }
`
	if err := os.WriteFile(filepath.Join(service, "service_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runNative(t, service)
}

func mustLoad(t *testing.T, dir string) Config {
	t.Helper()
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSchema2LayoutVersionsAreGuarded(t *testing.T) {
	dir := multiStarter(t)
	path := filepath.Join(dir, "model/model.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.Replace(b, []byte("Tag byte"), []byte("Tag byte\n Extra uint32"), 1)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, p := range []string{"zz_gosvm.go", "client/zz_gosvm.go", "idl.json", "layouts.json"} {
		before[p], _ = os.ReadFile(filepath.Join(dir, p))
	}
	if err := Generate(dir); err == nil || !strings.Contains(err.Error(), "layout changed") {
		t.Fatalf("accepted unversioned change: %v", err)
	}
	for p, b := range before {
		got, _ := os.ReadFile(filepath.Join(dir, p))
		if !bytes.Equal(b, got) {
			t.Fatalf("failed generation replaced %s", p)
		}
	}
	editMultiConfig(t, dir, func(c *Config) { c.Layouts[0].Version = 2 })
	if err := Generate(dir); err == nil || !strings.Contains(err.Error(), "migration policy") {
		t.Fatalf("unrecorded migration: %v", err)
	}
	editMultiConfig(t, dir, func(c *Config) {
		c.Layouts[0].Migration = "Create new v2 accounts; existing v1 accounts remain on the prior program."
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "layouts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock layoutLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	if len(lock.Layouts) != 5 {
		t.Fatalf("lost history: %+v", lock)
	}
	var old, new WireLayout
	for _, l := range lock.Layouts {
		if l.Name == "Vault" {
			if l.Version == 1 {
				old = l
			} else {
				new = l
			}
		}
	}
	if old.Size != 61 || new.Size != 65 || discWord(old.Discriminator) == discWord(new.Discriminator) {
		t.Fatalf("version layout history: %+v %+v", old, new)
	}
	editMultiConfig(t, dir, func(c *Config) { c.Layouts[0].Version = 1 })
	if err := Generate(dir); err == nil {
		t.Fatal("accepted old version with changed layout")
	}
	editMultiConfig(t, dir, func(c *Config) { c.Layouts[0].Version = 2 })
	if err := os.Remove(filepath.Join(dir, "layouts.json")); err != nil {
		t.Fatal(err)
	}
	if err := Generate(dir); err == nil || !strings.Contains(err.Error(), "lock is missing") {
		t.Fatalf("silently blessed missing layout history: %v", err)
	}
}

func TestSchema2InvalidDeclarations(t *testing.T) {
	cases := []struct {
		name, want string
		edit       func(*Config)
	}{
		{"legacy", "schema-1", func(c *Config) { c.State = "Vault" }},
		{"unknown layout", "known layout", func(c *Config) { c.Instructions[0].Accounts[0].Layout = "Missing" }},
		{"owner", "program_id owner", func(c *Config) { c.Instructions[0].Accounts[0].Owner = "" }},
		{"unknown access", "read/write", func(c *Config) { c.Instructions[0].Accounts[0].Access = "mutable" }},
		{"unknown kind", "unsupported account kind", func(c *Config) { c.Instructions[0].Accounts[0].Kind = "unknown" }},
		{"zero version", "zero version", func(c *Config) { c.Layouts[0].Version = 0 }},
		{"collision", "discriminator collision", func(c *Config) {
			c.Instructions[0].Discriminator = "0001020304050607"
			c.Instructions[1].Discriminator = "0001020304050607"
		}},
		{"alias absent", "invalid/duplicate alias", func(c *Config) {
			c.Instructions[0].Aliases = []AccountAlias{{Left: "Source", Right: "Missing", Reason: "test"}}
		}},
		{"alias reason", "missing reason", func(c *Config) { c.Instructions[0].Aliases = []AccountAlias{{Left: "Source", Right: "Destination"}} }},
		{"mutable alias", "mutable state", func(c *Config) {
			c.Instructions[0].Aliases = []AccountAlias{{Left: "Source", Right: "Destination", Reason: "test"}}
		}},
		{"relation account", "invalid key relationship", func(c *Config) { c.Instructions[0].Relations[0].Account = "Missing" }},
		{"relation field", "unknown relationship field", func(c *Config) { c.Instructions[0].Relations[0].Field = "Source.Missing" }},
		{"relation type", "requires a [32]byte", func(c *Config) { c.Instructions[0].Relations[0].Field = "Source.Balance" }},
		{"handler", "handler Missing not found", func(c *Config) { c.Instructions[0].Handler = "Missing" }},
		{"signature", "must accept", func(c *Config) { c.Instructions[0].Handler = "SetLimit" }},
		{"ref", "declared shared-package", func(c *Config) { c.Layouts[0].Type = "Vault" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := multiStarter(t)
			editMultiConfig(t, dir, tc.edit)
			before, _ := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
			if err := Generate(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %q", err, tc.want)
			}
			after, _ := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
			if !bytes.Equal(before, after) {
				t.Fatal("failed generation changed adapter")
			}
		})
	}
}

func TestSchema2SourceDiagnosticsBeforePublication(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"global", "package program\nvar Global uint64\n", "package scope"},
		{"reserved error", "package program\nconst ErrReserved uint64 = 6000\n", "1..5999"},
		{"evaluated error", "package program\nconst ErrReserved = 5999 + 1\n", "1..5999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := multiStarter(t)
			before, _ := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
			if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Generate(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
			after, _ := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
			if !bytes.Equal(before, after) {
				t.Fatal("published invalid adapter")
			}
		})
	}
}

func TestSchema2HistoricalManifestValidation(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		edit       func(*layoutLock)
	}{
		{"long discriminator", "historical layout", func(l *layoutLock) { l.Layouts[0].Discriminator = append(l.Layouts[0].Discriminator, 0) }},
		{"invalid discriminator byte", "historical discriminator", func(l *layoutLock) { l.Layouts[0].Discriminator[0] = 256 }},
		{"duplicate history", "historical layout", func(l *layoutLock) { l.Layouts = append(l.Layouts, l.Layouts[0]) }},
		{"historical collision", "historical discriminator collision", func(l *layoutLock) { l.Layouts[1].Discriminator = l.Layouts[0].Discriminator }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := multiStarter(t)
			path := filepath.Join(dir, "layouts.json")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var lock layoutLock
			if err := json.Unmarshal(b, &lock); err != nil {
				t.Fatal(err)
			}
			tc.edit(&lock)
			b, err = json.Marshal(lock)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			if err := Generate(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
}

func TestSchema2ReadonlyAliasAndProgramIdentity(t *testing.T) {
	dir := multiStarter(t)
	editMultiConfig(t, dir, func(c *Config) {
		ix := &c.Instructions[0]
		ix.Accounts[0].Access, ix.Accounts[1].Access = "read", "read"
		ix.Aliases = []AccountAlias{{Left: "Source", Right: "Destination", Reason: "two readonly views of the same ledger"}}
		ix.Accounts = append(ix.Accounts, AccountDeclaration{Name: "External", Kind: "program", Access: "read", Address: strings.Repeat("08", 32)})
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	// Replace the scaffold's write-oriented tests for this explicit readonly
	// variant. Duplicate state references are legal only for this declared pair.
	source := `package program_test
import("bytes";"testing";program "example.org/multi-state";"example.org/multi-state/model";"example.org/multi-state/client";"gosvm/solana")
func TestAlias(t *testing.T) {
 id:=bytes.Repeat([]byte{7},32);var authority model.PublicKey;for i:=range authority{authority[i]=4}
 data:=client.EncodeAccountVault(model.Vault{Authority:authority,Balance:100})
 c:=solana.Context{ID:id,InstructionData:client.EncodeInstructionMove(model.MoveArgs{Amount:10}),Accounts:[]solana.Account{
 {Key:bytes.Repeat([]byte{1},32),Owner:id,Data:data},{Key:bytes.Repeat([]byte{1},32),Owner:id,Data:data},
 {Key:bytes.Repeat([]byte{3},32),Owner:id,Data:client.EncodeAccountPolicy(model.Policy{Limit:50})},
 {Key:authority[:],Signer:true},{Key:bytes.Repeat([]byte{8},32),Executable:true},
 }}
 before:=append([]byte(nil),data...);if code:=program.Process(c);code!=0 || !bytes.Equal(before,data){t.Fatal(code,data)}
 c.Accounts[4].Key[31]=9;if code:=program.Process(c);code!=6005{t.Fatal(code)}
 c.Accounts[4].Key[31]=8;c.Accounts[4].Executable=false;if code:=program.Process(c);code!=6001{t.Fatal(code)}
}
`
	if err := os.WriteFile(filepath.Join(dir, "program_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runNative(t, dir)
}

func TestSchema2NamedScalarArrayWire(t *testing.T) {
	dir := multiStarter(t)
	path := filepath.Join(dir, "model/model.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte("\ntype Epoch uint32\ntype Epochs [3]Epoch\n")...)
	b = bytes.Replace(b, []byte("Balance uint64"), []byte("Balance uint64\n Epochs Epochs\n Empty [0]byte"), 1)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	editMultiConfig(t, dir, func(c *Config) {
		c.Layouts[0].Version = 2
		c.Layouts[0].Migration = "Use fresh v2 ledgers with epoch history."
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	source := `package program_test
import("encoding/binary";"testing";"example.org/multi-state/model";"example.org/multi-state/client")
func TestArrayWire(t *testing.T) {v:=model.Vault{Balance:5,Epochs:model.Epochs{1,0x12345678,0xffffffff},Moves:9};b:=client.EncodeAccountVault(v);if len(b)!=73 || binary.LittleEndian.Uint32(b[57:])!=0x12345678 || binary.LittleEndian.Uint64(b[65:])!=9{t.Fatal(b)};got,e:=client.DecodeAccountVault(b);if e!=nil || got!=v{t.Fatal(got,e)}}
`
	if err := os.WriteFile(filepath.Join(dir, "program_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runNative(t, dir)
}
