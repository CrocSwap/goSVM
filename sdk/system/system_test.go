package system

import (
	"bytes"
	"encoding/binary"
	"gosvm/sdk/pda"
	"gosvm/solana"
	"math"
	"testing"
)

func createContext() solana.Context {
	id, payer, account := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	id[0], payer[0], account[0] = 11, 7, 9
	return solana.Context{ID: id, Accounts: []solana.Account{
		{Key: make([]byte, 32), Executable: true},
		{Key: payer, Owner: make([]byte, 32), Signer: true, Writable: true, Lamports: 10000000},
		{Key: account, Owner: make([]byte, 32), Signer: true, Writable: true},
	}, ReadRent: func(b []byte) uint64 {
		binary.LittleEndian.PutUint64(b, 3480)
		binary.LittleEndian.PutUint64(b[8:], math.Float64bits(2))
		return 0
	}}
}

func TestSystemCreateAndTransferEncoding(t *testing.T) {
	c := createContext()
	var owner [32]byte
	owner[0] = 11
	calls := 0
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		calls++
		if program != 0 || len(seeds) != 0 || !bytes.Equal(metas, []byte{1, 0, 0, 0, 0, 0, 0, 0, 3, 2, 0, 0, 0, 0, 0, 0, 0, 3}) || len(data) != 52 || binary.LittleEndian.Uint32(data) != 0 || binary.LittleEndian.Uint64(data[4:]) != 1900080 || binary.LittleEndian.Uint64(data[12:]) != 145 || !bytes.Equal(data[20:], owner[:]) {
			t.Fatal(program, metas, data, seeds)
		}
		return 77
	}
	if code := Create(c, 0, 1, 2, 145, owner); code != 77 || calls != 1 {
		t.Fatal(code, calls)
	}
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		calls++
		if program != 0 || len(metas) != 18 || metas[8] != 3 || metas[17] != 1 || len(data) != 12 || binary.LittleEndian.Uint32(data) != 2 || binary.LittleEndian.Uint64(data[4:]) != math.MaxUint64 || len(seeds) != 0 {
			t.Fatal(program, metas, data, seeds)
		}
		return 78
	}
	if code := Transfer(c, 0, 1, 2, math.MaxUint64); code != 78 || calls != 2 {
		t.Fatal(code, calls)
	}
}

func TestSystemPDAAndCreationGuards(t *testing.T) {
	var owner [32]byte
	owner[0] = 11
	tests := []struct {
		name string
		edit func(*solana.Context)
		size uint64
		code uint64
	}{
		{"program-key", func(c *solana.Context) { c.Accounts[0].Key[0] = 1 }, 145, ErrProgram},
		{"program-executable", func(c *solana.Context) { c.Accounts[0].Executable = false }, 145, ErrProgram},
		{"payer-signer", func(c *solana.Context) { c.Accounts[1].Signer = false }, 145, ErrAuthority},
		{"target-signer", func(c *solana.Context) { c.Accounts[2].Signer = false }, 145, ErrAuthority},
		{"payer-owner", func(c *solana.Context) { c.Accounts[1].Owner[0] = 1 }, 145, ErrAccount},
		{"payer-data", func(c *solana.Context) { c.Accounts[1].Data = []byte{1} }, 145, ErrAccount},
		{"target-owner", func(c *solana.Context) { c.Accounts[2].Owner[0] = 1 }, 145, ErrAccount},
		{"target-data", func(c *solana.Context) { c.Accounts[2].Data = []byte{1} }, 145, ErrAccount},
		{"target-funded", func(c *solana.Context) { c.Accounts[2].Lamports = 1 }, 145, ErrAccount},
		{"target-readonly", func(c *solana.Context) { c.Accounts[2].Writable = false }, 145, ErrAccount},
		{"target-executable", func(c *solana.Context) { c.Accounts[2].Executable = true }, 145, ErrAccount},
		{"duplicate", func(c *solana.Context) { c.Accounts[2].Key = c.Accounts[1].Key }, 145, ErrAccount},
		{"size", func(c *solana.Context) {}, 10241, ErrSize},
		{"size-wide", func(c *solana.Context) {}, math.MaxUint64, ErrSize},
		{"rent-failure", func(c *solana.Context) { c.ReadRent = func([]byte) uint64 { return 99 } }, 145, 99},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := createContext()
			c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid create invoked CPI"); return 0 }
			test.edit(&c)
			if code := Create(c, 0, 1, 2, test.size, owner); code != test.code {
				t.Fatal(code, test.code)
			}
		})
	}
	c := createContext()
	c.Accounts[2].Signer = false
	var seeds pda.Seeds
	if seeds.AddByte(255) != 0 {
		t.Fatal("seed")
	}
	c.DeriveAddress = func(seeds, program, output []byte) uint64 {
		if !bytes.Equal(program, c.ID) || !bytes.Equal(seeds, []byte{1, 1, 255}) {
			t.Fatal(seeds, program)
		}
		copy(output, c.Accounts[2].Key)
		return 0
	}
	c.InvokeInstruction = func(_ uint64, _, _, encoded []byte) uint64 {
		if !bytes.Equal(encoded, []byte{1, 1, 1, 255}) {
			t.Fatal(encoded)
		}
		return 0
	}
	if code := CreateSigned(c, 0, 1, 2, 145, owner, &seeds); code != 0 {
		t.Fatal(code)
	}
	c.DeriveAddress = func(_, _, output []byte) uint64 { output[0] = 10; return 0 }
	if code := CreateSigned(c, 0, 1, 2, 145, owner, &seeds); code != ErrAuthority {
		t.Fatal(code)
	}
	if code := CreateSigned(c, 0, 1, 2, 145, owner, nil); code != pda.ErrSeeds {
		t.Fatal(code)
	}
}

func TestSystemFreeRentRetainsAccount(t *testing.T) {
	c := createContext()
	c.ReadRent = func([]byte) uint64 { return 0 }
	c.InvokeInstruction = func(_ uint64, _, data, _ []byte) uint64 {
		if binary.LittleEndian.Uint64(data[4:]) != 1 {
			t.Fatal("free-rent creation must retain a funded account", data)
		}
		return 0
	}
	if code := Create(c, 0, 1, 2, 145, [32]byte{11}); code != 0 {
		t.Fatal(code)
	}
}

// pdaContext holds System, payer, a PDA target derived as bytes{9} under
// program 11, and a recipient. DeriveAddress maps seeds [1,1,bump] to the PDA
// only for bump 254.
func pdaContext() solana.Context {
	c := createContext()
	c.Accounts[2].Signer = false
	recipient := make([]byte, 32)
	recipient[0] = 5
	c.Accounts = append(c.Accounts, solana.Account{Key: recipient, Owner: make([]byte, 32), Writable: true})
	c.DeriveAddress = func(seeds, program, output []byte) uint64 {
		copy(output, make([]byte, 32))
		if bytes.Equal(seeds, []byte{1, 1, 254}) && bytes.Equal(program, c.ID) {
			copy(output, c.Accounts[2].Key)
		}
		return 0
	}
	return c
}

func bumpSeeds(bump byte) *pda.Seeds {
	var seeds pda.Seeds
	seeds.AddByte(bump)
	return &seeds
}

func TestSystemTransferSignedEncodingAndGuards(t *testing.T) {
	c := pdaContext()
	c.Accounts[2].Lamports = 500
	calls := 0
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		calls++
		if program != 0 || !bytes.Equal(metas, []byte{2, 0, 0, 0, 0, 0, 0, 0, 3, 3, 0, 0, 0, 0, 0, 0, 0, 1}) || len(data) != 12 || binary.LittleEndian.Uint32(data) != 2 || binary.LittleEndian.Uint64(data[4:]) != 400 || !bytes.Equal(seeds, []byte{1, 1, 1, 254}) {
			t.Fatal(program, metas, data, seeds)
		}
		return 1 // System ResultWithNegativeLamports, propagated unchanged.
	}
	if code := TransferSigned(c, 0, 2, 3, 400, bumpSeeds(254)); code != 1 || calls != 1 {
		t.Fatal(code, calls)
	}
	for _, tc := range []struct {
		name  string
		want  uint64
		bump  byte
		edit  func(*solana.Context)
		nilSd bool
	}{
		{"nil-seeds", pda.ErrSeeds, 254, func(*solana.Context) {}, true},
		{"wrong-seeds", ErrAuthority, 253, func(*solana.Context) {}, false},
		{"program", ErrProgram, 254, func(c *solana.Context) { c.Accounts[0].Key[0] = 1 }, false},
		{"source-owner", ErrAccount, 254, func(c *solana.Context) { c.Accounts[2].Owner[0] = 1 }, false},
		{"source-data", ErrAccount, 254, func(c *solana.Context) { c.Accounts[2].Data = []byte{0} }, false},
		{"source-readonly", ErrAccount, 254, func(c *solana.Context) { c.Accounts[2].Writable = false }, false},
		{"destination-readonly", ErrAccount, 254, func(c *solana.Context) { c.Accounts[3].Writable = false }, false},
		{"self", ErrAccount, 254, func(c *solana.Context) { c.Accounts[3].Key = c.Accounts[2].Key }, false},
		{"index", ErrAccount, 254, func(c *solana.Context) { c.Accounts = c.Accounts[:3] }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := pdaContext()
			tc.edit(&c)
			c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid transfer reached CPI"); return 0 }
			seeds := bumpSeeds(tc.bump)
			if tc.nilSd {
				seeds = nil
			}
			if code := TransferSigned(c, 0, 2, 3, 1, seeds); code != tc.want {
				t.Fatal(code, tc.want)
			}
		})
	}
}

func TestSystemAllocateAssignEncoding(t *testing.T) {
	c := pdaContext()
	var owner [32]byte
	owner[0] = 11
	var got [][]byte
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		if program != 0 || !bytes.Equal(metas, []byte{2, 0, 0, 0, 0, 0, 0, 0, 3}) {
			t.Fatal(program, metas)
		}
		got = append(got, append(append([]byte(nil), data...), seeds...))
		return 0
	}
	if Allocate(c, 0, 2, 64) != ErrAuthority || AllocateSigned(c, 0, 2, 64, bumpSeeds(254)) != 0 || AssignSigned(c, 0, 2, owner, bumpSeeds(254)) != 0 {
		t.Fatal("signed allocate/assign")
	}
	c.Accounts[2].Signer = true
	if Allocate(c, 0, 2, 10240) != 0 || Assign(c, 0, 2, owner) != 0 {
		t.Fatal("signer allocate/assign")
	}
	alloc := []byte{8, 0, 0, 0, 64, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 254}
	assign := append(append([]byte{1, 0, 0, 0}, owner[:]...), 1, 1, 1, 254)
	if len(got) != 4 || !bytes.Equal(got[0], alloc) || !bytes.Equal(got[1], assign) || !bytes.Equal(got[2], []byte{8, 0, 0, 0, 0, 40, 0, 0, 0, 0, 0, 0}) || !bytes.Equal(got[3], assign[:36]) {
		t.Fatal(got)
	}
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid allocate/assign reached CPI"); return 0 }
	if Allocate(c, 0, 2, 10241) != ErrSize || AllocateSigned(c, 0, 2, 1, nil) != pda.ErrSeeds || AssignSigned(c, 0, 2, owner, nil) != pda.ErrSeeds {
		t.Fatal("size/seeds guards")
	}
	c.Accounts[2].Owner = owner[:]
	if Allocate(c, 0, 2, 1) != ErrAccount || Assign(c, 0, 2, owner) != ErrAccount {
		t.Fatal("non-System target")
	}
	c.Accounts[2].Owner, c.Accounts[2].Data = make([]byte, 32), []byte{0, 1}
	if Allocate(c, 0, 2, 1) != ErrAccount || Assign(c, 0, 2, owner) != ErrAccount {
		t.Fatal("allocated target with data")
	}
	c.Accounts[2].Data = []byte{0, 0}
	if Allocate(c, 0, 2, 1) != ErrAccount {
		t.Fatal("allocated target")
	}
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { return 0 }
	if Assign(c, 0, 2, owner) != 0 {
		t.Fatal("assign of zeroed allocation")
	}
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid allocate/assign reached CPI"); return 0 }
	c.Accounts[2].Data = nil
	c.Accounts[0].Executable = false
	if Allocate(c, 0, 2, 1) != ErrProgram || Assign(c, 0, 2, owner) != ErrProgram {
		t.Fatal("System program")
	}
}

func TestSystemCreatePDAFundedAndUnfunded(t *testing.T) {
	var owner [32]byte
	owner[0] = 11
	// 1900080 is the default-rent minimum for 145 bytes.
	for _, tc := range []struct {
		name     string
		lamports uint64
		ops      []byte // System opcodes in order
		topUp    uint64
	}{
		{"unfunded", 0, []byte{0}, 0},
		{"partly-funded", 1000, []byte{2, 8, 1}, 1900080 - 1000},
		{"rent-funded", 1900080, []byte{8, 1}, 0},
		{"over-funded", 5000000, []byte{8, 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := pdaContext()
			c.Accounts[2].Lamports = tc.lamports
			var ops []byte
			c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
				ops = append(ops, data[0])
				switch data[0] {
				case 2:
					if binary.LittleEndian.Uint64(data[4:]) != tc.topUp || len(seeds) != 0 || binary.LittleEndian.Uint64(metas) != 1 || metas[8] != 3 {
						t.Fatal("top-up", data, seeds, metas)
					}
				case 8:
					if binary.LittleEndian.Uint64(data[4:]) != 145 || !bytes.Equal(seeds, []byte{1, 1, 1, 254}) {
						t.Fatal("allocate", data, seeds)
					}
					c.Accounts[2].Data = make([]byte, 145) // Assign then sees the allocated, zeroed data.
				case 1:
					if !bytes.Equal(data[4:], owner[:]) || !bytes.Equal(seeds, []byte{1, 1, 1, 254}) {
						t.Fatal("assign", data, seeds)
					}
				case 0:
					if binary.LittleEndian.Uint64(data[4:]) != 1900080 || !bytes.Equal(seeds, []byte{1, 1, 1, 254}) {
						t.Fatal("create", data, seeds)
					}
				}
				return 0
			}
			if code := CreatePDA(c, 0, 1, 2, 145, owner, bumpSeeds(254)); code != 0 || !bytes.Equal(ops, tc.ops) {
				t.Fatal(code, ops, tc.ops)
			}
		})
	}
	c := pdaContext()
	c.Accounts[2].Lamports = 1000
	stops := 0
	c.InvokeInstruction = func(_ uint64, _, data, _ []byte) uint64 { stops++; return 1 }
	if code := CreatePDA(c, 0, 1, 2, 145, owner, bumpSeeds(254)); code != 1 || stops != 1 {
		t.Fatal("failed top-up must stop before allocation", code, stops)
	}
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid create reached CPI"); return 0 }
	if CreatePDA(c, 0, 1, 2, 145, owner, bumpSeeds(253)) != ErrAuthority || CreatePDA(c, 0, 1, 2, 145, owner, nil) != pda.ErrSeeds || CreatePDA(c, 0, 1, 2, 10241, owner, bumpSeeds(254)) != ErrSize {
		t.Fatal("funded guards")
	}
	c.Accounts[1].Signer = false
	if CreatePDA(c, 0, 1, 2, 145, owner, bumpSeeds(254)) != ErrAuthority {
		t.Fatal("payer signature")
	}
	c.Accounts[1].Signer = true
	c.Accounts[2].Owner = owner[:]
	if CreatePDA(c, 0, 1, 2, 145, owner, bumpSeeds(254)) != ErrAccount {
		t.Fatal("already-assigned target")
	}
	if CreatePDA(c, 0, 1, 9, 145, owner, bumpSeeds(254)) != ErrAccount {
		t.Fatal("target index")
	}
}
