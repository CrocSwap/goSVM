package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkedSwap(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "full-swap")
	src := filepath.Join("..", "..", "examples", "full-swap")
	err := filepath.WalkDir(src, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if e.IsDir() {
			if e.Name() == "build" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dir, rel), 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSchema2CheckedTokenAndPDAGeneration(t *testing.T) {
	dir := checkedSwap(t)
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	m, err := resolveMulti(dir, mustLoad(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if m.States[0].Size != 145 || m.Arguments[0].Size != 24 {
		t.Fatal("baseline wire changed", m.States, m.Arguments)
	}
	runNative(t, dir)
	// Scalar and literal seeds must resolve against the actual argument types.
	editMultiConfig(t, dir, func(c *Config) {
		c.Instructions[0].Accounts[6].PDA.Seeds = []SeedDeclaration{
			{Kind: "bytes", Hex: "0058"}, {Kind: "bytes", Hex: ""}, {Kind: "key", Account: "Pool"},
			{Kind: "byte", Field: "Pool.Bump"}, {Kind: "uint64", Field: "args.AmountIn"},
		}
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("seeds.AddUint64(uint64(args.AmountIn))")) || !bytes.Contains(b, []byte("seeds.AddBytes(literal1[:])")) {
		t.Fatal("typed seeds were not emitted")
	}
	// Exercise both sides of token-field comparisons, plus derivation under an
	// explicitly declared program rather than the current program ID.
	editMultiConfig(t, dir, func(c *Config) {
		c.Instructions[0].Relations = append(c.Instructions[0].Relations, KeyRelation{Field: "UserX.Mint", Equals: "VaultX.Mint"})
		c.Instructions[0].Accounts[6].PDA.Program = "TokenProgram"
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
}

func TestSchema2PDAAdapterOnlyImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pda-ledger")
	if err := NewModuleSDK(dir, "example.org/pda-ledger", MultiSchema, 2); err != nil {
		t.Fatal(err)
	}
	editMultiConfig(t, dir, func(c *Config) {
		a := &c.Instructions[0].Accounts[3]
		a.Kind = "account"
		a.PDA = &PDADeclaration{Program: "program_id", Seeds: []SeedDeclaration{
			{Kind: "bytes", Hex: "0078"}, {Kind: "key", Account: "Source"},
			{Kind: "byte", Field: "Source.Audit.Tag"}, {Kind: "uint32", Field: "Source.Audit.Epoch"}, {Kind: "uint64", Field: "args.Amount"},
		}}
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("seeds.AddUint32(uint32(accounts.Source.Audit.Epoch))")) {
		t.Fatal("nested uint32 seed was not emitted")
	}
}

func TestSchema2CheckedConstraintDiagnostics(t *testing.T) {
	cases := []struct {
		name, want string
		edit       func(*Config)
	}{
		{"legacy token", "requires SDK 2", func(c *Config) { c.SDKVersion = 1 }},
		{"token owner override", "owner override", func(c *Config) { c.Instructions[0].Accounts[2].Owner = "program_id" }},
		{"token layout", "no owned layout", func(c *Config) { c.Instructions[0].Accounts[2].Layout = "Pool" }},
		{"token signer", "no signer", func(c *Config) { c.Instructions[0].Accounts[2].Signer = true }},
		{"reference collision", "distinct exported", func(c *Config) { c.Instructions[0].Accounts[0].Ref = "UserX" }},
		{"duplicate reference", "distinct exported", func(c *Config) { c.Instructions[0].Accounts[2].Ref = "PoolRef" }},
		{"raw reference field", "state/token", func(c *Config) { c.Instructions[0].Accounts[1].Ref = "UserRef" }},
		{"PDA signer", "non-transaction-signer", func(c *Config) { c.Instructions[0].Accounts[6].Signer = true }},
		{"PDA program", "declared program account", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Program = "User" }},
		{"many seeds", "at most 16", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds = make([]SeedDeclaration, 17) }},
		{"unknown seed", "unsupported PDA seed", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds[0].Kind = "expression" }},
		{"unknown key", "declared account", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds[0].Account = "Missing" }},
		{"oversized seed", "at most 32", func(c *Config) {
			c.Instructions[0].Accounts[6].PDA.Seeds = []SeedDeclaration{{Kind: "bytes", Hex: strings.Repeat("ff", 33)}}
		}},
		{"bad hex", "hex-encoded", func(c *Config) {
			c.Instructions[0].Accounts[6].PDA.Seeds = []SeedDeclaration{{Kind: "bytes", Hex: "zz"}}
		}},
		{"unknown scalar", "unknown relationship field", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds[1].Field = "Pool.Missing" }},
		{"wrong scalar width", "requires byte", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds[1].Field = "Pool.Swaps" }},
		{"bad scalar selector", "args or state", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds[1].Field = "Authority.Key" }},
		{"mixed seed inputs", "declared account only", func(c *Config) { c.Instructions[0].Accounts[6].PDA.Seeds[0].Hex = "00" }},
		{"both relation targets", "invalid key relationship", func(c *Config) { c.Instructions[0].Relations[0].Equals = "Pool.MintX" }},
		{"missing relation target", "invalid key relationship", func(c *Config) { c.Instructions[0].Relations[0].Account = "" }},
		{"wrong token field", "unknown relationship field", func(c *Config) { c.Instructions[0].Relations[2].Field = "UserX.Unknown" }},
		{"scalar token relation", "requires a [32]byte", func(c *Config) { c.Instructions[0].Relations[2].Field = "UserX.Amount" }},
		{"scalar target", "requires a [32]byte", func(c *Config) { c.Instructions[0].Relations[2].Equals = "Pool.Swaps" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := checkedSwap(t)
			editMultiConfig(t, dir, tc.edit)
			before := map[string][]byte{}
			for _, name := range []string{"zz_gosvm.go", "client/zz_gosvm.go", "idl.json", "layouts.json"} {
				before[name], _ = os.ReadFile(filepath.Join(dir, name))
			}
			if err := Generate(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; wanted %q", err, tc.want)
			}
			for name, want := range before {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("failed generation changed %s: %v", name, err)
				}
			}
		})
	}
}
