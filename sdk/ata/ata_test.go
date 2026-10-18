package ata

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"gosvm/sdk/fixture"
	"gosvm/sdk/pda"
	"gosvm/sdk/token"
	"gosvm/solana"
	"testing"
)

// derive is create_program_address for one encoded seed group, returning 1
// for an on-curve candidate as the runtime does.
func derive(seeds, program, output []byte) uint64 {
	h := sha256.New()
	off := 1
	for i := 0; i < int(seeds[0]); i++ {
		n := int(seeds[off])
		h.Write(seeds[off+1 : off+1+n])
		off += 1 + n
	}
	h.Write(program)
	h.Write([]byte("ProgramDerivedAddress"))
	var k fixture.Key
	copy(k[:], h.Sum(nil))
	if fixture.OnCurve(k) {
		return 1
	}
	copy(output, k[:])
	return 0
}

// ataContext: 0 ATA, 1 payer, 2 derived ATA, 3 wallet, 4 mint, 5 System,
// 6 Token-2022, 7 PDA payer ("ata_payer" + bump under program 11s).
func ataContext(t *testing.T) (solana.Context, byte) {
	id, t22 := ProgramID(), token.Program2022ID()
	wallet, mint := bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{4}, 32)
	var w, m fixture.Key
	copy(w[:], wallet)
	copy(m[:], mint)
	address, _, err := fixture.FindProgramAddress(id, w[:], t22[:], m[:])
	if err != nil {
		t.Fatal(err)
	}
	program := bytes.Repeat([]byte{11}, 32)
	var p fixture.Key
	copy(p[:], program)
	payer, bump, err := fixture.FindProgramAddress(p, []byte("ata_payer"))
	if err != nil {
		t.Fatal(err)
	}
	system := make([]byte, 32)
	return solana.Context{ID: program, DeriveAddress: derive, Accounts: []solana.Account{
		{Key: id[:], Executable: true},
		{Key: bytes.Repeat([]byte{1}, 32), Owner: system, Signer: true, Writable: true, Lamports: 1 << 30},
		{Key: address[:], Owner: system, Writable: true},
		{Key: wallet, Owner: system},
		{Key: mint, Owner: t22[:], Data: make([]byte, 82)},
		{Key: system, Executable: true},
		{Key: t22[:], Executable: true},
		{Key: payer[:], Owner: system, Writable: true, Lamports: 1 << 30},
	}}, bump
}

func payerSeeds(bump byte) *pda.Seeds {
	var seeds pda.Seeds
	seeds.AddBytes([]byte("ata_payer"))
	seeds.AddByte(bump)
	return &seeds
}

func TestAddressMatchesFindProgramAddress(t *testing.T) {
	c, _ := ataContext(t)
	for _, program := range [][32]byte{token.ProgramID(), token.Program2022ID()} {
		for j := byte(0); j < 20; j++ {
			wallet, mint := [32]byte{j, 1}, [32]byte{j, 2}
			want, wantBump, err := fixture.FindProgramAddress(ProgramID(), wallet[:], program[:], mint[:])
			got, bump, code := Address(c, wallet, program, mint)
			if err != nil || code != 0 || got != want || bump != wantBump {
				t.Fatal(j, got, want, bump, wantBump, code)
			}
		}
	}
	var wallet, mint [32]byte
	copy(wallet[:], c.Accounts[3].Key)
	copy(mint[:], c.Accounts[4].Key)
	if ok, code := Matches(c, 2, wallet, token.Program2022ID(), mint); !ok || code != 0 {
		t.Fatal("derived account", ok, code)
	}
	if ok, code := Matches(c, 2, wallet, token.ProgramID(), mint); ok || code != 0 {
		t.Fatal("classic address differs", ok, code)
	}
	if _, code := Matches(c, 99, wallet, token.ProgramID(), mint); code != ErrAccount {
		t.Fatal("index", code)
	}
}

func TestCreateEncoding(t *testing.T) {
	c, bump := ataContext(t)
	var got [][]byte
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		if program != 0 {
			t.Fatal(program)
		}
		got = append(got, append(append(append([]byte(nil), metas...), data...), seeds...))
		return 0
	}
	meta := func(index uint64, flags byte) []byte {
		b := make([]byte, 9)
		binary.LittleEndian.PutUint64(b, index)
		b[8] = flags
		return b
	}
	metas := func(payer uint64) []byte {
		return bytes.Join([][]byte{meta(payer, 3), meta(2, 1), meta(3, 0), meta(4, 0), meta(5, 0), meta(6, 0)}, nil)
	}
	if CreateIdempotent(c, 0, 1, 2, 3, 4, 5, 6) != 0 || Create(c, 0, 1, 2, 3, 4, 5, 6) != 0 ||
		CreateIdempotentSigned(c, 0, 7, 2, 3, 4, 5, 6, payerSeeds(bump)) != 0 || CreateSigned(c, 0, 7, 2, 3, 4, 5, 6, payerSeeds(bump)) != 0 {
		t.Fatal("create")
	}
	signer := append([]byte{1, 2, 9}, append([]byte("ata_payer"), 1, bump)...)
	want := [][]byte{append(metas(1), 1), append(metas(1), 0), append(append(metas(7), 1), signer...), append(append(metas(7), 0), signer...)}
	if len(got) != 4 {
		t.Fatal(got)
	}
	for j := range want {
		if !bytes.Equal(got[j], want[j]) {
			t.Fatalf("%d: %x want %x", j, got[j], want[j])
		}
	}
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { return 6 }
	if CreateIdempotent(c, 0, 1, 2, 3, 4, 5, 6) != 6 {
		t.Fatal("ATA status must propagate")
	}
}

func TestCreateGuards(t *testing.T) {
	_, bump := ataContext(t)
	for _, tc := range []struct {
		name   string
		edit   func(*solana.Context)
		signed bool
		bump   byte
		want   uint64
	}{
		{"ata-program", func(c *solana.Context) { c.Accounts[0].Key = bytes.Repeat([]byte{8}, 32) }, false, 0, ErrProgram},
		{"ata-executable", func(c *solana.Context) { c.Accounts[0].Executable = false }, false, 0, ErrProgram},
		{"token-program", func(c *solana.Context) { c.Accounts[6].Executable = false }, false, 0, ErrProgram},
		{"system", func(c *solana.Context) { c.Accounts[5].Key = bytes.Repeat([]byte{1}, 32) }, false, 0, ErrProgram},
		{"mint-owner", func(c *solana.Context) { v := token.ProgramID(); c.Accounts[4].Owner = v[:] }, false, 0, ErrAccount},
		{"mint-executable", func(c *solana.Context) { c.Accounts[4].Executable = true }, false, 0, ErrAccount},
		{"not-derived", func(c *solana.Context) { c.Accounts[2].Key = bytes.Repeat([]byte{2}, 32) }, false, 0, ErrAccount},
		{"account-readonly", func(c *solana.Context) { c.Accounts[2].Writable = false }, false, 0, ErrAccount},
		{"payer-owner", func(c *solana.Context) { c.Accounts[1].Owner = c.Accounts[0].Key }, false, 0, ErrAccount},
		{"payer-data", func(c *solana.Context) { c.Accounts[1].Data = []byte{0} }, false, 0, ErrAccount},
		{"payer-readonly", func(c *solana.Context) { c.Accounts[1].Writable = false }, false, 0, ErrAccount},
		{"payer-unsigned", func(c *solana.Context) { c.Accounts[1].Signer = false }, false, 0, ErrAuthority},
		{"pda-payer-unsigned-without-seeds", func(c *solana.Context) { c.Accounts[1] = c.Accounts[7] }, false, 0, ErrAuthority},
		{"pda-payer-wrong-seeds", func(c *solana.Context) {}, true, bump ^ 1, ErrAuthority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := ataContext(t)
			tc.edit(&c)
			c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid create reached CPI"); return 0 }
			var code uint64
			if !tc.signed {
				code = CreateIdempotent(c, 0, 1, 2, 3, 4, 5, 6)
			} else {
				code = CreateIdempotentSigned(c, 0, 7, 2, 3, 4, 5, 6, payerSeeds(tc.bump))
				if code == 1 {
					code = ErrAuthority // an on-curve wrong bump fails derivation instead
				}
			}
			if code != tc.want {
				t.Fatal(code, tc.want)
			}
		})
	}
	c, _ := ataContext(t)
	if CreateIdempotentSigned(c, 0, 7, 2, 3, 4, 5, 6, nil) != pda.ErrSeeds || CreateSigned(c, 0, 7, 2, 3, 4, 5, 6, nil) != pda.ErrSeeds {
		t.Fatal("nil seeds")
	}
}
