package token

import (
	"bytes"
	"encoding/binary"
	"gosvm/solana"
	"testing"
)

func tokenContext() solana.Context {
	id := ProgramID()
	mint := [32]byte{6}
	authority := [32]byte{7}
	account := func(key byte, amount uint64) solana.Account {
		data := make([]byte, 165)
		copy(data, mint[:])
		copy(data[32:], authority[:])
		binary.LittleEndian.PutUint64(data[64:], amount)
		data[108] = 1
		return solana.Account{Key: bytes.Repeat([]byte{key}, 32), Owner: append([]byte(nil), id[:]...), Data: data, Writable: true}
	}
	return solana.Context{Accounts: []solana.Account{{Key: id[:], Executable: true}, account(1, 100), account(2, 5), {Key: authority[:], Signer: true}}}
}

func TestCheckedTransferAndCurrentReads(t *testing.T) {
	c := tokenContext()
	src, code := Load(c, 1, true)
	if code != 0 {
		t.Fatal(code)
	}
	dst, code := Load(c, 2, true)
	if code != 0 {
		t.Fatal(code)
	}
	calls := 0
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		calls++
		if program != 0 || len(metas) != 27 || binary.LittleEndian.Uint64(metas) != 1 || metas[8] != 1 || binary.LittleEndian.Uint64(metas[9:]) != 2 || metas[17] != 1 || binary.LittleEndian.Uint64(metas[18:]) != 3 || metas[26] != 2 || len(data) != 9 || data[0] != 3 || binary.LittleEndian.Uint64(data[1:]) != 10 || len(seeds) != 0 {
			t.Fatal(program, metas, data, seeds)
		}
		// Replace the backing slices too, so reads cannot use an old token snapshot.
		c.Accounts[1].Data = append([]byte(nil), c.Accounts[1].Data...)
		c.Accounts[2].Data = append([]byte(nil), c.Accounts[2].Data...)
		binary.LittleEndian.PutUint64(c.Accounts[1].Data[64:], 90)
		binary.LittleEndian.PutUint64(c.Accounts[2].Data[64:], 15)
		return 0
	}
	if code := Transfer(c, 0, src, dst, 3, 10); code != 0 {
		t.Fatal(code)
	}
	a, code := Read(c, src)
	if code != 0 || a.Amount != 90 || calls != 1 {
		t.Fatal(a, code, calls)
	}
	if amount, status := Balance(c, src); status != 0 || amount != 90 {
		t.Fatal("current balance after replaced backing", amount, status)
	}
	c.Accounts[2].Data[108] = 2
	c.InvokeInstruction = func(_ uint64, _, _, _ []byte) uint64 { calls++; return 17 }
	if code := Transfer(c, 0, src, dst, 3, 10); code != 17 || calls != 2 {
		t.Fatal("frozen/CPI status", code, calls)
	}
}

func TestCheckedBalanceRevalidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		want uint64
		edit func(*solana.Context)
	}{
		{"owner", ErrAccount, func(c *solana.Context) { c.Accounts[1].Owner[0] ^= 1 }},
		{"layout", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data = c.Accounts[1].Data[:164] }},
		{"privilege", ErrWritable, func(c *solana.Context) { c.Accounts[1].Writable = false }},
		{"identity", ErrIdentity, func(c *solana.Context) { c.Accounts[1].Key[0] ^= 1 }},
		{"uninitialized", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data[108] = 0 }},
		{"native", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data[109] = 1 }},
		{"executable", ErrAccount, func(c *solana.Context) { c.Accounts[1].Executable = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tokenContext()
			ref, _ := Load(c, 1, true)
			tc.edit(&c)
			if amount, code := Balance(c, ref); code != tc.want || amount != 0 {
				t.Fatal(amount, code, tc.want)
			}
		})
	}
	c := tokenContext()
	ref, _ := Load(c, 1, false)
	c.Accounts[1].Writable = false
	c.Accounts[1].Data[108] = 2
	binary.LittleEndian.PutUint64(c.Accounts[1].Data[64:], ^uint64(0))
	if amount, code := Balance(c, ref); code != 0 || amount != ^uint64(0) {
		t.Fatal("readonly frozen wide balance", amount, code)
	}
}

func TestTokenRevalidationBeforeInvoke(t *testing.T) {
	for _, tc := range []struct {
		name string
		want uint64
		edit func(*solana.Context)
	}{
		{"owner", ErrAccount, func(c *solana.Context) { c.Accounts[1].Owner[0] ^= 1 }},
		{"size", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data = c.Accounts[1].Data[:164] }},
		{"uninitialized", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data[108] = 0 }},
		{"native", ErrAccount, func(c *solana.Context) { c.Accounts[1].Data[109] = 1 }},
		{"readonly", ErrWritable, func(c *solana.Context) { c.Accounts[1].Writable = false }},
		{"identity", ErrIdentity, func(c *solana.Context) { c.Accounts[1].Key[0] ^= 1 }},
		{"mint", ErrMint, func(c *solana.Context) { c.Accounts[2].Data[0] ^= 1 }},
		{"authority", ErrAuthority, func(c *solana.Context) { c.Accounts[1].Data[32] ^= 1 }},
		{"unsigned", ErrAuthority, func(c *solana.Context) { c.Accounts[3].Signer = false }},
		{"program", 2001, func(c *solana.Context) { c.Accounts[0].Executable = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tokenContext()
			src, _ := Load(c, 1, true)
			dst, _ := Load(c, 2, true)
			tc.edit(&c)
			c.InvokeInstruction = func(_ uint64, _, _, _ []byte) uint64 { t.Fatal("invalid account reached CPI"); return 0 }
			if code := Transfer(c, 0, src, dst, 3, 10); code != tc.want {
				t.Fatal(code, tc.want)
			}
		})
	}
	c := tokenContext()
	src, _ := Load(c, 1, false)
	dst, _ := Load(c, 2, true)
	if code := Transfer(c, 0, src, dst, 3, 10); code != ErrWritable {
		t.Fatal("readonly reference", code)
	}
}
