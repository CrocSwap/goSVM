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
