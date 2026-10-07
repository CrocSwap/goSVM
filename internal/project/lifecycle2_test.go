package project

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func lifecycleExample(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "escrow")
	src := filepath.Join("..", "..", "examples", "escrow")
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "build" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dir, rel), 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0600)
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSchema2LifecycleGeneration(t *testing.T) {
	dir := lifecycleExample(t)
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	runNative(t, dir)
	b, err := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, required := range []string{".CreateSigned(c,", "solana.CloseAccount(c, 0, 2)", "accounts.Escrow.Beneficiary[j] != key2[j]", "args.Beneficiary[j] != key2[j]", "gosvmWrite(state0, 0, 8,"} {
		if !strings.Contains(text, required) {
			t.Fatal("missing lifecycle operation", required)
		}
	}
	claim := text[strings.Index(text, "func gosvmExecClaim("):strings.Index(text, "func gosvmPDA_Claim_")]
	if strings.Contains(claim, "updated.Escrow") {
		t.Fatal("serialized a closed account")
	}
	open := text[strings.Index(text, "func gosvmExecOpen("):strings.Index(text, "func gosvmPDA_Open_")]
	if strings.Index(open, "args.Beneficiary[j] != key2[j]") > strings.Index(open, "initCodeEscrow :=") {
		t.Fatal("moved rent before argument relationship validation")
	}
	before := append([]byte(nil), b...)
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if !bytes.Equal(b, before) {
		t.Fatal("nondeterministic lifecycle generation")
	}
	// A signed target uses Create directly; only the adapter imports system.
	editMultiConfig(t, dir, func(c *Config) { a := &c.Instructions[0].Accounts[0]; a.PDA = nil; a.Signer = true })
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if !strings.Contains(string(b), ".Create(c,") {
		t.Fatal("missing signed init")
	}
}

func TestSchema2LifecycleDiagnostics(t *testing.T) {
	tests := []struct {
		name, want string
		edit       func(*Config)
	}{
		{"init readonly", "writable state", func(c *Config) { c.Instructions[0].Accounts[0].Access = "read" }},
		{"init raw", "only state accounts", func(c *Config) { c.Instructions[0].Accounts[0].Kind = "account" }},
		{"init and close", "one lifecycle action", func(c *Config) { c.Instructions[0].Accounts[0].Close = &CloseDeclaration{} }},
		{"no payer", "init payer", func(c *Config) { c.Instructions[0].Accounts[0].Init.Payer = "Missing" }},
		{"readonly payer", "init payer", func(c *Config) { c.Instructions[0].Accounts[1].Access = "read" }},
		{"unsigned payer", "init payer", func(c *Config) { c.Instructions[0].Accounts[1].Kind = "account" }},
		{"non-System payer", "init payer", func(c *Config) { c.Instructions[0].Accounts[1].Owner = "program_id" }},
		{"missing System", "init system", func(c *Config) { c.Instructions[0].Accounts[0].Init.System = "Missing" }},
		{"wrong System", "init system", func(c *Config) { c.Instructions[0].Accounts[3].Address = strings.Repeat("01", 32) }},
		{"no target signer", "transaction signer or PDA", func(c *Config) { c.Instructions[0].Accounts[0].PDA = nil }},
		{"external PDA", "PDA under program_id", func(c *Config) { c.Instructions[0].Accounts[0].PDA.Program = "System" }},
		{"init alias", "cannot have account aliases", func(c *Config) {
			c.Instructions[0].Aliases = []AccountAlias{{Left: "Escrow", Right: "Beneficiary", Reason: "test"}}
		}},
		{"fresh field seed", "uninitialized state", func(c *Config) { c.Instructions[0].Accounts[0].PDA.Seeds[2].Field = "Escrow.Nonce" }},
		{"fresh relationship", "uninitialized state", func(c *Config) { c.Instructions[0].Relations[0].Field = "Escrow.Beneficiary" }},
		{"fresh target relationship", "uninitialized state", func(c *Config) {
			c.Instructions[0].Relations[0] = KeyRelation{Field: "args.Beneficiary", Equals: "Escrow.Beneficiary"}
		}},
		{"bad argument field", "unknown relationship field", func(c *Config) { c.Instructions[0].Relations[0].Field = "args.Unknown" }},
		{"scalar argument relationship", "[32]byte", func(c *Config) { c.Instructions[0].Relations[0].Field = "args.Amount" }},
		{"close readonly", "writable state", func(c *Config) { c.Instructions[1].Accounts[0].Access = "read" }},
		{"missing refund", "refund account", func(c *Config) { c.Instructions[1].Accounts[0].Close.To = "Missing" }},
		{"self refund", "refund account", func(c *Config) { c.Instructions[1].Accounts[0].Close.To = "Escrow" }},
		{"readonly refund", "refund account", func(c *Config) { c.Instructions[1].Accounts[2].Access = "read" }},
		{"missing authority", "transaction signer", func(c *Config) { c.Instructions[1].Accounts[0].Close.Authority = "Missing" }},
		{"unsigned authority", "transaction signer", func(c *Config) { c.Instructions[1].Accounts[2].Kind = "account" }},
		{"caller authority field", "this state's canonical key", func(c *Config) { c.Instructions[1].Accounts[0].Close.AuthorityField = "args.Preimage" }},
		{"missing authority field", "unknown relationship field", func(c *Config) { c.Instructions[1].Accounts[0].Close.AuthorityField = "Escrow.Unknown" }},
		{"scalar authority field", "[32]byte", func(c *Config) { c.Instructions[1].Accounts[0].Close.AuthorityField = "Escrow.Amount" }},
		{"close alias", "cannot have account aliases", func(c *Config) {
			c.Instructions[1].Aliases = []AccountAlias{{Left: "Escrow", Right: "Creator", Reason: "test"}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := lifecycleExample(t)
			before := map[string][]byte{}
			for _, name := range []string{"zz_gosvm.go", "client/zz_gosvm.go", "idl.json", "layouts.json"} {
				before[name], _ = os.ReadFile(filepath.Join(dir, name))
			}
			editMultiConfig(t, dir, test.edit)
			if err := Generate(dir); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %s", err, test.want)
			}
			for name, want := range before {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("invalid lifecycle changed artifact", name, err)
				}
			}
		})
	}
	c := mustLoad(t, lifecycleExample(t))
	c.SDKVersion = 1
	if err := validateMultiConfig(c); err == nil || !strings.Contains(err.Error(), "SDK 2") {
		t.Fatal("SDK1 accepted lifecycle", err)
	}
}

func TestSchema2AllInitTargetsValidatedBeforeCPI(t *testing.T) {
	dir := lifecycleExample(t)
	editMultiConfig(t, dir, func(c *Config) {
		c.Instructions[0].Accounts = append(c.Instructions[0].Accounts, AccountDeclaration{
			Name: "Reserve", Kind: "state", Layout: "Escrow", Owner: "program_id", Access: "write", Signer: true,
			Init: &InitDeclaration{Payer: "Creator", System: "System"},
		})
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	// The first target is valid, but the second has data. Every target's pure
	// preconditions must be checked before the first rent-funded creation runs.
	path := filepath.Join(dir, "program_test.go")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, []byte(`
func TestMultipleInitPreconditions(t *testing.T) {
 c,calls:=contextFor(t)
 c.Accounts=append(c.Accounts,solana.Account{Key:bytes.Repeat([]byte{55},32),Owner:make([]byte,32),Data:[]byte{1},Signer:true,Writable:true})
 before:=copyAccounts(c)
 if code:=program.Process(c);code!=6009 {t.Fatal(code)}
 if *calls!=0 || !reflect.DeepEqual(before,c.Accounts) {t.Fatal("invalid later target funded the first",*calls)}
}
`)...)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-run", "TestMultipleInitPreconditions", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("all-init preconditions: %v %s", err, out)
	}
}
