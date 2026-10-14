package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A schema-2 project with uint16 state fields (scalar, named and array),
// uint16 argument fields and uint16 PDA seeds from both arguments and state.
// Generation runs the full compiler Check on the adapter; the generated codecs
// and seed encoding are then exercised natively.
func TestSchema2Uint16FieldsAndSeeds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "multi-state")
	if err := NewModuleSDK(dir, "example.org/multi-state", MultiSchema, 2); err != nil {
		t.Fatal(err)
	}
	replace := func(name, old, new string) {
		t.Helper()
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte(old)) {
			t.Fatalf("%s lacks %q", name, old)
		}
		if err = os.WriteFile(path, bytes.Replace(b, []byte(old), []byte(new), 1), 0600); err != nil {
			t.Fatal(err)
		}
	}
	replace("model/model.go", "Balance uint64", "Balance uint64\n\tFee uint16\n\tPorts [2]Port")
	replace("model/model.go", "type MoveArgs struct { Amount uint64 }", "type Port uint16\ntype MoveArgs struct { Amount uint64; Fee uint16; Port Port }")
	// The on-chain handler does wrapping uint16 arithmetic on decoded state.
	replace("program.go", "\taccounts.Source.Moves++", "\taccounts.Source.Moves++\n\taccounts.Source.Fee = accounts.Source.Fee + args.Fee\n\taccounts.Source.Ports[1]++\n\taccounts.Destination.Ports[0] = args.Port")
	editMultiConfig(t, dir, func(c *Config) {
		c.Layouts[0].Version, c.Layouts[0].Migration = 2, "Fresh v2 ledgers carry uint16 fees and ports."
		c.Instructions[0].Version, c.Instructions[0].Migration = 2, "Move v2 carries a uint16 fee and port."
		c.Instructions[0].Accounts = append(c.Instructions[0].Accounts, AccountDeclaration{Name: "Marker", Kind: "account", Access: "read",
			PDA: &PDADeclaration{Program: "program_id", Seeds: []SeedDeclaration{{Kind: "uint16", Field: "args.Fee"}, {Kind: "uint16", Field: "Source.Fee"}, {Kind: "uint16", Field: "args.Port"}, {Kind: "key", Account: "Policy"}}}})
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	m, err := resolveMulti(dir, mustLoad(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	vault := m.States[m.StateByName["Vault"]]
	fee, ports := vault.Fields[3], vault.Fields[4]
	if vault.Size != 67 || fee.Name != "Fee" || fee.Type != "uint16" || fee.Offset != 53 || fee.Size != 2 ||
		ports.Type != "[2]example.org/multi-state/model.Port" || ports.Offset != 55 || ports.Size != 4 || ports.Length != 2 || vault.Fields[5].Offset != 59 {
		t.Fatalf("uint16 state layout: %+v", vault)
	}
	if args := m.Arguments[0]; args.Size != 20 || args.Fields[1].Offset != 16 || args.Fields[2].Type != "example.org/multi-state/model.Port" || args.Fields[2].Offset != 18 {
		t.Fatalf("uint16 argument layout: %+v", args)
	}
	for _, name := range []string{"idl.json", "layouts.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Contains(b, []byte(`"type": "uint16"`)) {
			t.Fatalf("%s does not name uint16 fields: %v", name, err)
		}
	}
	adapter, err := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"seed0[0] = byte(uint16(args.Fee))", "seed0[1] = byte(uint16(args.Fee) >> 8)", "seeds.AddBytes(seed1[:])", "accounts.Source.Fee = uint16(gosvmRead(state0, 53, 2))", "gosvmWrite(state0, 53, 2, uint64(updated.Source.Fee))"} {
		if !bytes.Contains(adapter, []byte(want)) {
			t.Fatalf("adapter lacks %q:\n%s", want, adapter)
		}
	}
	source := `package program_test
import("bytes";"encoding/binary";"testing";program "example.org/multi-state";"example.org/multi-state/client";"example.org/multi-state/model";"gosvm/solana")
func TestUint16Codecs(t *testing.T) {
 for _, v := range []model.Vault{{}, {Fee:65535,Ports:[2]model.Port{0x1234,65535},Balance:7,Moves:9}, {Fee:0x0102,Ports:[2]model.Port{1,0x8000}}} {
  b:=client.EncodeAccountVault(v)
  if len(b)!=67 || binary.LittleEndian.Uint16(b[53:])!=v.Fee || binary.LittleEndian.Uint16(b[55:])!=uint16(v.Ports[0]) || binary.LittleEndian.Uint16(b[57:])!=uint16(v.Ports[1]) || binary.LittleEndian.Uint64(b[59:])!=v.Moves { t.Fatalf("%x",b) }
  got,e:=client.DecodeAccountVault(b); if e!=nil || got!=v { t.Fatal(got,e) }
 }
 a:=model.MoveArgs{Amount:3,Fee:0xfeff,Port:0x0201}
 b:=client.EncodeInstructionMove(a)
 if len(b)!=20 || !bytes.Equal(b[16:],[]byte{0xff,0xfe,0x01,0x02}) { t.Fatalf("%x",b) }
 got,e:=client.DecodeInstructionMove(b); if e!=nil || got!=a { t.Fatal(got,e) }
}
func TestUint16HandlerAndSeeds(t *testing.T) {
 id:=bytes.Repeat([]byte{7},32); var authority model.PublicKey; for i:=range authority { authority[i]=4 }
 marker:=bytes.Repeat([]byte{9},32); policyKey:=bytes.Repeat([]byte{3},32)
 var seen []byte
 c:=solana.Context{ID:id,InstructionData:client.EncodeInstructionMove(model.MoveArgs{Amount:10,Fee:2,Port:0xabcd}),Accounts:[]solana.Account{
  {Key:bytes.Repeat([]byte{1},32),Owner:id,Data:client.EncodeAccountVault(model.Vault{Authority:authority,Balance:100,Fee:65535,Ports:[2]model.Port{5,65535}}),Writable:true},
  {Key:bytes.Repeat([]byte{2},32),Owner:id,Data:client.EncodeAccountVault(model.Vault{Authority:authority,Balance:5}),Writable:true},
  {Key:policyKey,Owner:id,Data:client.EncodeAccountPolicy(model.Policy{Authority:authority,Limit:50})},
  {Key:append([]byte(nil),authority[:]...),Owner:make([]byte,32),Signer:true},
  {Key:marker,Owner:make([]byte,32)},
 },DeriveAddress:func(seeds,program,output []byte) uint64 { seen=append([]byte(nil),seeds...); copy(output,marker); return 0 }}
 if code:=program.Process(c); code!=0 { t.Fatal(code) }
 want:=append([]byte{4, 2,2,0, 2,0xff,0xff, 2,0xcd,0xab, 32}, policyKey...)
 if !bytes.Equal(seen,want) { t.Fatalf("seeds %x, want %x",seen,want) }
 source,e:=client.DecodeAccountVault(c.Accounts[0].Data)
 if e!=nil || source.Fee!=1 || source.Ports!=[2]model.Port{5,0} || source.Balance!=90 { t.Fatal(source,e) }
 destination,e:=client.DecodeAccountVault(c.Accounts[1].Data)
 if e!=nil || destination.Ports[0]!=0xabcd { t.Fatal(destination,e) }
 c.Accounts[4].Key=bytes.Repeat([]byte{8},32)
 if code:=program.Process(c); code!=6008 { t.Fatalf("PDA mismatch: %d",code) }
}
`
	if err = os.WriteFile(filepath.Join(dir, "program_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runNative(t, dir)
	// Seed kinds must match the field's underlying width.
	editMultiConfig(t, dir, func(c *Config) {
		ix := &c.Instructions[0]
		ix.Accounts[len(ix.Accounts)-1].PDA.Seeds[0] = SeedDeclaration{Kind: "uint16", Field: "args.Amount"}
	})
	if err := Generate(dir); err == nil || !strings.Contains(err.Error(), "seed args.Amount requires uint16, got uint64") {
		t.Fatalf("mismatched uint16 seed: %v", err)
	}
}
