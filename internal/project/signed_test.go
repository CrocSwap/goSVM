package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A schema-2 project with signed state fields (int64, int8, a named int16
// array and int32), signed argument fields and signed PDA seeds from both
// arguments and state. Generation runs the full compiler Check on the
// adapter; the generated codecs (two's complement, little-endian), the
// handler's signed arithmetic and the seed encoding are then exercised
// natively.
func TestSchema2SignedFieldsAndSeeds(t *testing.T) {
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
	replace("model/model.go", "Balance uint64", "Balance uint64\n\tDelta int64\n\tTick int8\n\tTemps [2]Temp\n\tSkew int32")
	replace("model/model.go", "type MoveArgs struct { Amount uint64 }", "type Temp int16\ntype MoveArgs struct { Amount uint64; Change int32; Level int8; Temp Temp; Offset int64 }")
	// The on-chain handler does wrapping signed arithmetic on decoded state,
	// including MinInt32 / -1 when Level is -1.
	replace("program.go", "\taccounts.Source.Moves++", `	accounts.Source.Moves++
	accounts.Source.Delta = accounts.Source.Delta - int64(args.Amount) + args.Offset
	accounts.Source.Tick = accounts.Source.Tick + args.Level
	accounts.Source.Temps[1]--
	accounts.Source.Skew = accounts.Source.Skew>>3 + args.Change
	accounts.Destination.Temps[0] = args.Temp
	accounts.Destination.Skew = accounts.Destination.Skew / int32(args.Level|1)`)
	editMultiConfig(t, dir, func(c *Config) {
		c.Layouts[0].Version, c.Layouts[0].Migration = 2, "Fresh v2 ledgers carry signed deltas, ticks, temperatures and skew."
		c.Instructions[0].Version, c.Instructions[0].Migration = 2, "Move v2 carries signed change, level, temperature and offset."
		c.Instructions[0].Accounts = append(c.Instructions[0].Accounts, AccountDeclaration{Name: "Marker", Kind: "account", Access: "read",
			PDA: &PDADeclaration{Program: "program_id", Seeds: []SeedDeclaration{{Kind: "int8", Field: "args.Level"}, {Kind: "int16", Field: "args.Temp"},
				{Kind: "int32", Field: "args.Change"}, {Kind: "int64", Field: "Source.Delta"}, {Kind: "int32", Field: "Source.Skew"}, {Kind: "key", Account: "Policy"}}}})
	})
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	m, err := resolveMulti(dir, mustLoad(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	vault := m.States[m.StateByName["Vault"]]
	f := vault.Fields
	if vault.Size != 78 || f[3].Name != "Delta" || f[3].Type != "int64" || f[3].Offset != 53 || f[3].Size != 8 ||
		f[4].Type != "int8" || f[4].Offset != 61 || f[4].Size != 1 ||
		f[5].Type != "[2]example.org/multi-state/model.Temp" || f[5].Offset != 62 || f[5].Size != 4 || f[5].Length != 2 ||
		f[6].Type != "int32" || f[6].Offset != 66 || f[6].Size != 4 || f[7].Offset != 70 {
		t.Fatalf("signed state layout: %+v", vault)
	}
	if args := m.Arguments[0]; args.Size != 31 || args.Fields[1].Offset != 16 || args.Fields[2].Offset != 20 ||
		args.Fields[3].Type != "example.org/multi-state/model.Temp" || args.Fields[3].Offset != 21 || args.Fields[4].Offset != 23 {
		t.Fatalf("signed argument layout: %+v", args)
	}
	for _, name := range []string{"idl.json", "layouts.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"type": "int64"`, `"type": "int8"`, `"type": "int32"`} {
			if !bytes.Contains(b, []byte(want)) {
				t.Fatalf("%s does not name %s fields", name, want)
			}
		}
	}
	adapter, err := os.ReadFile(filepath.Join(dir, "zz_gosvm.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"seeds.AddByte(byte(int8(args.Level)))", "seed1[0] = byte(uint16(int16(args.Temp)))", "seed1[1] = byte(uint16(int16(args.Temp)) >> 8)",
		"seeds.AddUint32(uint32(int32(args.Change)))", "seeds.AddUint64(uint64(int64(accounts.Source.Delta)))",
		"accounts.Source.Delta = int64(gosvmRead(state0, 53, 8))", "accounts.Source.Tick = int8(gosvmRead(state0, 61, 1))",
		"gosvmWrite(state0, 66, 4, uint64(updated.Source.Skew))",
	} {
		if !bytes.Contains(adapter, []byte(want)) {
			t.Fatalf("adapter lacks %q:\n%s", want, adapter)
		}
	}
	source := `package program_test
import("bytes";"encoding/binary";"math";"testing";program "example.org/multi-state";"example.org/multi-state/client";"example.org/multi-state/model";"gosvm/solana")
func TestSignedCodecs(t *testing.T) {
 for _, v := range []model.Vault{{}, {Delta:-1,Tick:-128,Temps:[2]model.Temp{-32768,32767},Skew:math.MinInt32,Balance:7,Moves:9}, {Delta:math.MinInt64,Tick:127,Temps:[2]model.Temp{-2,0x0102},Skew:-5}, {Delta:math.MaxInt64,Skew:math.MaxInt32}} {
  b:=client.EncodeAccountVault(v)
  if len(b)!=78 || int64(binary.LittleEndian.Uint64(b[53:]))!=v.Delta || int8(b[61])!=v.Tick || model.Temp(binary.LittleEndian.Uint16(b[62:]))!=v.Temps[0] || model.Temp(binary.LittleEndian.Uint16(b[64:]))!=v.Temps[1] || int32(binary.LittleEndian.Uint32(b[66:]))!=v.Skew || binary.LittleEndian.Uint64(b[70:])!=v.Moves { t.Fatalf("%x",b) }
  got,e:=client.DecodeAccountVault(b); if e!=nil || got!=v { t.Fatal(got,e) }
 }
 a:=model.MoveArgs{Amount:3,Change:-2,Level:-1,Temp:-300,Offset:math.MinInt64}
 b:=client.EncodeInstructionMove(a)
 if len(b)!=31 || !bytes.Equal(b[16:],[]byte{0xfe,0xff,0xff,0xff, 0xff, 0xd4,0xfe, 0,0,0,0,0,0,0,0x80}) { t.Fatalf("%x",b) }
 got,e:=client.DecodeInstructionMove(b); if e!=nil || got!=a { t.Fatal(got,e) }
}
func TestSignedHandlerAndSeeds(t *testing.T) {
 id:=bytes.Repeat([]byte{7},32); var authority model.PublicKey; for i:=range authority { authority[i]=4 }
 marker:=bytes.Repeat([]byte{9},32); policyKey:=bytes.Repeat([]byte{3},32)
 var seen []byte
 c:=solana.Context{ID:id,InstructionData:client.EncodeInstructionMove(model.MoveArgs{Amount:10,Change:-7,Level:-1,Temp:-2,Offset:-5}),Accounts:[]solana.Account{
  {Key:bytes.Repeat([]byte{1},32),Owner:id,Data:client.EncodeAccountVault(model.Vault{Authority:authority,Balance:100,Delta:math.MinInt64+3,Tick:-128,Temps:[2]model.Temp{5,-32768},Skew:-17}),Writable:true},
  {Key:bytes.Repeat([]byte{2},32),Owner:id,Data:client.EncodeAccountVault(model.Vault{Authority:authority,Balance:5,Skew:math.MinInt32}),Writable:true},
  {Key:policyKey,Owner:id,Data:client.EncodeAccountPolicy(model.Policy{Authority:authority,Limit:50})},
  {Key:append([]byte(nil),authority[:]...),Owner:make([]byte,32),Signer:true},
  {Key:marker,Owner:make([]byte,32)},
 },DeriveAddress:func(seeds,program,output []byte) uint64 { seen=append([]byte(nil),seeds...); copy(output,marker); return 0 }}
 if code:=program.Process(c); code!=0 { t.Fatal(code) }
 want:=append([]byte{6, 1,0xff, 2,0xfe,0xff, 4,0xf9,0xff,0xff,0xff, 8,3,0,0,0,0,0,0,0x80, 4,0xef,0xff,0xff,0xff, 32}, policyKey...)
 if !bytes.Equal(seen,want) { t.Fatalf("seeds %x, want %x",seen,want) }
 source,e:=client.DecodeAccountVault(c.Accounts[0].Data)
 // Delta wraps: MinInt64+3-10-5 = MaxInt64-11; Tick: -128 + -1 wraps to 127;
 // Temps[1]-- wraps to 32767; Skew: -17>>3 = -3, -3 + -7 = -10.
 if e!=nil || source.Delta!=math.MaxInt64-11 || source.Tick!=127 || source.Temps!=[2]model.Temp{5,32767} || source.Skew!=-10 || source.Balance!=90 { t.Fatal(source,e) }
 destination,e:=client.DecodeAccountVault(c.Accounts[1].Data)
 // MinInt32 / -1 == MinInt32 in Go; no panic.
 if e!=nil || destination.Temps[0]!=-2 || destination.Skew!=math.MinInt32 { t.Fatal(destination,e) }
 c.Accounts[4].Key=bytes.Repeat([]byte{8},32)
 if code:=program.Process(c); code!=6008 { t.Fatalf("PDA mismatch: %d",code) }
}
`
	if err = os.WriteFile(filepath.Join(dir, "program_test.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	runNative(t, dir)
	// Seed kinds must match the field's underlying type.
	editMultiConfig(t, dir, func(c *Config) {
		ix := &c.Instructions[0]
		ix.Accounts[len(ix.Accounts)-1].PDA.Seeds[0] = SeedDeclaration{Kind: "int32", Field: "args.Offset"}
	})
	if err := Generate(dir); err == nil || !strings.Contains(err.Error(), "seed args.Offset requires int32, got int64") {
		t.Fatalf("mismatched int32 seed: %v", err)
	}
}

// int and uint have no fixed width: wire layouts reject them with a message
// naming the 64-bit type to use.
func TestSchema2RejectsPlatformSizedIntegers(t *testing.T) {
	for _, tc := range []struct{ field, want string }{
		{"Bad int", "wire field Bad: int is platform-sized; use int64"},
		{"Bad uint", "wire field Bad: uint is platform-sized; use uint64"},
		{"Bad [2]int", "wire field Bad[]: int is platform-sized; use int64"},
	} {
		dir := filepath.Join(t.TempDir(), "multi-state")
		if err := NewModuleSDK(dir, "example.org/multi-state", MultiSchema, 2); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "model/model.go")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, bytes.Replace(b, []byte("Balance uint64"), []byte("Balance uint64\n\t"+tc.field), 1), 0600); err != nil {
			t.Fatal(err)
		}
		if err := Generate(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want %q", tc.field, err, tc.want)
		}
	}
}
