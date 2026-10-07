package token

import (
	"bytes"
	"encoding/binary"
	"gosvm/sdk/pda"
	"gosvm/solana"
	"math"
	"testing"
)

func createContext() solana.Context {
	id := ProgramID()
	mint := make([]byte, 82)
	mint[45] = 1
	return solana.Context{Accounts: []solana.Account{
		{Key: make([]byte, 32), Executable: true}, {Key: id[:], Executable: true},
		{Key: bytes.Repeat([]byte{7}, 32), Owner: make([]byte, 32), Writable: true, Signer: true, Lamports: 100000000},
		{Key: bytes.Repeat([]byte{8}, 32), Owner: make([]byte, 32), Writable: true, Signer: true},
		{Key: bytes.Repeat([]byte{6}, 32), Owner: id[:], Data: mint},
	}, ReadRent: func(b []byte) uint64 {
		binary.LittleEndian.PutUint64(b, 3480)
		binary.LittleEndian.PutUint64(b[8:], math.Float64bits(2))
		return 0
	}}
}

func TestTokenCreateCurrentStorageAndCPIEncoding(t *testing.T) {
	c := createContext()
	var authority [32]byte
	authority[0] = 19
	calls := 0
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		calls++
		if calls == 1 {
			if program != 0 || len(data) != 52 || len(metas) != 18 || binary.LittleEndian.Uint64(metas) != 2 || metas[8] != 3 || binary.LittleEndian.Uint64(metas[9:]) != 3 || metas[17] != 3 || len(seeds) != 0 {
				t.Fatal(program, metas, data, seeds)
			}
			if binary.LittleEndian.Uint32(data) != 0 || binary.LittleEndian.Uint64(data[4:]) != 2039280 || binary.LittleEndian.Uint64(data[12:]) != 165 || !equalKey(data[20:], ProgramID()) {
				t.Fatal(data)
			}
			c.Accounts[3].Data = make([]byte, 165)
			id := ProgramID()
			c.Accounts[3].Owner = id[:]
			c.Accounts[3].Lamports = 2039280
			c.Accounts[2].Lamports -= 2039280
		} else {
			if program != 1 || len(data) != 33 || data[0] != 18 || !bytes.Equal(data[1:], authority[:]) || len(metas) != 18 || binary.LittleEndian.Uint64(metas) != 3 || metas[8] != 1 || binary.LittleEndian.Uint64(metas[9:]) != 4 || metas[17] != 0 || len(seeds) != 0 {
				t.Fatal(program, metas, data, seeds)
			}
			c.Accounts[3].Data = make([]byte, 165)
			copy(c.Accounts[3].Data, c.Accounts[4].Key)
			copy(c.Accounts[3].Data[32:], authority[:])
			c.Accounts[3].Data[108] = 1
		}
		return 0
	}
	if code := Create(c, 0, 1, 2, 3, 4, authority); code != 0 || calls != 2 {
		t.Fatal(code, calls)
	}
	ref, code := Load(c, 3, true)
	state, status := Read(c, ref)
	if code != 0 || status != 0 || state.Authority != authority || state.Amount != 0 {
		t.Fatal(code, status, state)
	}
}

func TestTokenCreatePreconditionsAndDownstreamFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		want uint64
		edit func(*solana.Context)
	}{
		{"token-program", 2001, func(c *solana.Context) { c.Accounts[1].Executable = false }},
		{"mint-owner", ErrMint, func(c *solana.Context) { c.Accounts[4].Owner = make([]byte, 32) }},
		{"mint-size", ErrMint, func(c *solana.Context) { c.Accounts[4].Data = c.Accounts[4].Data[:81] }},
		{"mint-uninitialized", ErrMint, func(c *solana.Context) { c.Accounts[4].Data[45] = 0 }},
		{"mint-coption", ErrMint, func(c *solana.Context) { c.Accounts[4].Data[46] = 2 }},
		{"native-mint", ErrMint, func(c *solana.Context) { v := nativeMint(); c.Accounts[4].Key = v[:] }},
		{"target-funded", 3022, func(c *solana.Context) { c.Accounts[3].Lamports = 1 }},
		{"target-unsigned", 3023, func(c *solana.Context) { c.Accounts[3].Signer = false }},
		{"payer-unsigned", 3023, func(c *solana.Context) { c.Accounts[2].Signer = false }},
		{"payer-aliased-target", 3022, func(c *solana.Context) { c.Accounts[3].Key = c.Accounts[2].Key }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := createContext()
			tc.edit(&c)
			c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid creation reached CPI"); return 0 }
			if code := Create(c, 0, 1, 2, 3, 4, [32]byte{19}); code != tc.want {
				t.Fatal(code, tc.want)
			}
		})
	}
	for _, failAt := range []int{1, 2} {
		c := createContext()
		calls := 0
		c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 {
			calls++
			if calls == failAt {
				return 1234
			}
			return 0
		}
		if code := Create(c, 0, 1, 2, 3, 4, [32]byte{19}); code != 1234 || calls != failAt {
			t.Fatal(code, calls)
		}
	}
	if CreateSigned(createContext(), 0, 1, 2, 3, 4, [32]byte{19}, nil) != pda.ErrSeeds {
		t.Fatal("nil seeds accepted")
	}
}

func TestTokenCloseAuthorityAndCurrentReference(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		c := tokenContext()
		c.Accounts[2].Lamports = 100
		c.Accounts[1].Data[108] = 2 // Frozen empty accounts may close.
		binary.LittleEndian.PutUint64(c.Accounts[1].Data[64:], 0)
		if explicit {
			c.Accounts[1].Data[129] = 1
			copy(c.Accounts[1].Data[133:], c.Accounts[3].Key)
			c.Accounts[1].Data[32] = 8
		}
		ref, _ := Load(c, 1, true)
		calls := 0
		c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
			calls++
			if program != 0 || len(data) != 1 || data[0] != 9 || len(metas) != 27 || binary.LittleEndian.Uint64(metas) != 1 || metas[8] != 1 || binary.LittleEndian.Uint64(metas[9:]) != 2 || metas[17] != 1 || binary.LittleEndian.Uint64(metas[18:]) != 3 || metas[26] != 2 || len(seeds) != 0 {
				t.Fatal(program, metas, data, seeds)
			}
			c.Accounts[1].Data = nil
			c.Accounts[1].Owner = make([]byte, 32)
			c.Accounts[1].Lamports = 0
			return 0
		}
		if code := Close(c, 0, ref, 2, 3); code != 0 || calls != 1 {
			t.Fatal(code, calls)
		}
		if _, code := Read(c, ref); code != ErrAccount {
			t.Fatal("stale closed ref remained valid", code)
		}
	}
	for _, tc := range []struct {
		name string
		want uint64
		edit func(*solana.Context)
	}{
		{"refund-readonly", ErrWritable, func(c *solana.Context) { c.Accounts[2].Writable = false }},
		{"refund-source-alias", ErrAccount, func(c *solana.Context) { c.Accounts[2].Key = c.Accounts[1].Key }},
		{"refund-executable", ErrAccount, func(c *solana.Context) { c.Accounts[2].Executable = true }},
		{"authority-unsigned", ErrAuthority, func(c *solana.Context) { c.Accounts[3].Signer = false }},
		{"close-authority-mismatch", ErrAuthority, func(c *solana.Context) { c.Accounts[1].Data[129] = 1; c.Accounts[1].Data[133] = 8 }},
		{"close-coption", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data[130] = 1 }},
		{"system-owned-authority", ErrAuthority, func(c *solana.Context) { clear(c.Accounts[1].Data[32:64]) }},
		{"source-identity", ErrIdentity, func(c *solana.Context) { c.Accounts[1].Key[0] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tokenContext()
			ref, _ := Load(c, 1, true)
			tc.edit(&c)
			c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid close reached CPI"); return 0 }
			if code := Close(c, 0, ref, 2, 3); code != tc.want {
				t.Fatal(code, tc.want)
			}
		})
	}
	c := tokenContext()
	ref, _ := Load(c, 1, true)
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { return 11 }
	if code := Close(c, 0, ref, 2, 3); code != 11 {
		t.Fatal("nonzero SPL balance error changed", code)
	}
	if CloseSigned(c, 0, ref, 2, 3, nil) != pda.ErrSeeds {
		t.Fatal("nil close seeds accepted")
	}
}
