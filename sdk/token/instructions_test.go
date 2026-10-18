package token

import (
	"bytes"
	"encoding/binary"
	"gosvm/sdk/pda"
	"gosvm/solana"
	"testing"
)

// instructionContext: 0 Token-2022, 1 mint, 2 source, 3 destination,
// 4 signer authority, 5 classic Token, 6 Rent sysvar, 7 PDA authority (key 9s
// under program 11, derived from seeds [1,1,254]).
func instructionContext() solana.Context {
	t22, classic := Program2022ID(), ProgramID()
	owner := append([]byte(nil), t22[:]...)
	account := func(key byte) solana.Account {
		return solana.Account{Key: bytes.Repeat([]byte{key}, 32), Owner: owner, Data: make([]byte, 170), Writable: true}
	}
	rent := []byte{6, 167, 213, 23, 25, 44, 92, 81, 33, 140, 201, 76, 61, 74, 241, 127, 88, 218, 238, 8, 155, 161, 253, 68, 227, 219, 217, 138, 0, 0, 0, 0}
	c := solana.Context{ID: bytes.Repeat([]byte{11}, 32), Accounts: []solana.Account{
		{Key: t22[:], Executable: true}, account(1), account(2), account(3),
		{Key: bytes.Repeat([]byte{4}, 32), Owner: make([]byte, 32), Signer: true},
		{Key: classic[:], Executable: true}, {Key: rent, Owner: make([]byte, 32)},
		{Key: bytes.Repeat([]byte{9}, 32), Owner: make([]byte, 32)},
	}}
	c.DeriveAddress = func(seeds, program, output []byte) uint64 {
		copy(output, make([]byte, 32))
		if bytes.Equal(seeds, []byte{1, 1, 254}) && bytes.Equal(program, c.ID) {
			copy(output, c.Accounts[7].Key)
		}
		return 0
	}
	return c
}

func seedsFor(bump byte) *pda.Seeds {
	var seeds pda.Seeds
	seeds.AddByte(bump)
	return &seeds
}

// meta encodes one expected cpi.Metas record.
func meta(index uint64, flags byte) []byte {
	b := make([]byte, 9)
	binary.LittleEndian.PutUint64(b, index)
	b[8] = flags
	return b
}

func metas(records ...[]byte) []byte { return bytes.Join(records, nil) }

type invocation struct {
	program            uint64
	metas, data, seeds []byte
}

func record(c *solana.Context, status uint64) *[]invocation {
	var calls []invocation
	c.InvokeInstruction = func(program uint64, m, data, seeds []byte) uint64 {
		calls = append(calls, invocation{program, append([]byte(nil), m...), append([]byte(nil), data...), append([]byte(nil), seeds...)})
		return status
	}
	return &calls
}

func amount(opcode byte, n uint64, decimals byte) []byte {
	b := make([]byte, 10)
	b[0] = opcode
	binary.LittleEndian.PutUint64(b[1:], n)
	b[9] = decimals
	return b
}

func TestCheckedInstructionEncodings(t *testing.T) {
	c := instructionContext()
	calls := record(&c, 0)
	var authority, freeze, metadata [32]byte
	authority[0], freeze[0], metadata[0] = 21, 22, 23
	pdaSeeds := []byte{1, 1, 1, 254}
	for _, step := range []struct {
		name string
		run  func() uint64
		want invocation
	}{
		{"mint2", func() uint64 { return InitializeMint2(c, 0, 1, 6, authority, freeze, false) },
			invocation{0, meta(1, 1), append(append([]byte{20, 6}, authority[:]...), 0), nil}},
		{"mint2-freeze", func() uint64 { return InitializeMint2(c, 0, 1, 9, authority, freeze, true) },
			invocation{0, meta(1, 1), append(append(append([]byte{20, 9}, authority[:]...), 1), freeze[:]...), nil}},
		{"metadata-pointer", func() uint64 { return InitializeMetadataPointer(c, 0, 1, authority, metadata) },
			invocation{0, meta(1, 1), append(append([]byte{39, 0}, authority[:]...), metadata[:]...), nil}},
		{"account3", func() uint64 { return InitializeAccount3(c, 0, 2, 1, authority) },
			invocation{0, metas(meta(2, 1), meta(1, 0)), append([]byte{18}, authority[:]...), nil}},
		{"account-self-owned", func() uint64 { return InitializeAccount(c, 0, 2, 1, 2, 6) },
			invocation{0, metas(meta(2, 1), meta(1, 0), meta(2, 0), meta(6, 0)), []byte{1}, nil}},
		{"data-size", func() uint64 { return GetAccountDataSize(c, 0, 1, []uint16{7, 0x0102}) },
			invocation{0, meta(1, 0), []byte{21, 7, 0, 2, 1}, nil}},
		{"data-size-base", func() uint64 { return GetAccountDataSize(c, 0, 1, nil) },
			invocation{0, meta(1, 0), []byte{21}, nil}},
		{"mint-to", func() uint64 { return MintToChecked(c, 0, 1, 3, 4, 1<<40, 6) },
			invocation{0, metas(meta(1, 1), meta(3, 1), meta(4, 2)), amount(14, 1<<40, 6), nil}},
		{"mint-to-pda", func() uint64 { return MintToCheckedSigned(c, 0, 1, 3, 7, 5, 6, seedsFor(254)) },
			invocation{0, metas(meta(1, 1), meta(3, 1), meta(7, 2)), amount(14, 5, 6), pdaSeeds}},
		{"burn", func() uint64 { return BurnChecked(c, 0, 2, 1, 4, 7, 6) },
			invocation{0, metas(meta(2, 1), meta(1, 1), meta(4, 2)), amount(15, 7, 6), nil}},
		{"burn-pda", func() uint64 { return BurnCheckedSigned(c, 0, 2, 1, 7, 7, 6, seedsFor(254)) },
			invocation{0, metas(meta(2, 1), meta(1, 1), meta(7, 2)), amount(15, 7, 6), pdaSeeds}},
		{"transfer", func() uint64 { return TransferChecked(c, 0, 2, 1, 3, 4, ^uint64(0), 0) },
			invocation{0, metas(meta(2, 1), meta(1, 0), meta(3, 1), meta(4, 2)), amount(12, ^uint64(0), 0), nil}},
		{"transfer-pda", func() uint64 { return TransferCheckedSigned(c, 0, 2, 1, 3, 7, 3, 6, seedsFor(254)) },
			invocation{0, metas(meta(2, 1), meta(1, 0), meta(3, 1), meta(7, 2)), amount(12, 3, 6), pdaSeeds}},
	} {
		before := len(*calls)
		if code := step.run(); code != 0 || len(*calls) != before+1 {
			t.Fatal(step.name, code)
		}
		got := (*calls)[before]
		if got.program != step.want.program || !bytes.Equal(got.metas, step.want.metas) || !bytes.Equal(got.data, step.want.data) || !bytes.Equal(got.seeds, step.want.seeds) {
			t.Fatalf("%s: got %+v want %+v", step.name, got, step.want)
		}
	}
}

func TestCheckedInstructionsClassicAndSelfAuthority(t *testing.T) {
	c := instructionContext()
	classic := ProgramID()
	for j := 1; j <= 3; j++ {
		c.Accounts[j].Owner = classic[:]
	}
	calls := record(&c, 0)
	// A mint that is its own PDA authority (Hyperlane's synthetic mint) and an
	// escrow that owns itself: the same index appears twice in the metas.
	c.Accounts[7], c.Accounts[1] = c.Accounts[1], c.Accounts[7]
	c.Accounts[1].Key = bytes.Repeat([]byte{9}, 32)
	c.Accounts[1].Owner, c.Accounts[1].Writable = classic[:], true
	c.DeriveAddress = func(seeds, program, output []byte) uint64 { copy(output, c.Accounts[1].Key); return 0 }
	if code := MintToCheckedSigned(c, 5, 1, 3, 1, 1, 6, seedsFor(254)); code != 0 {
		t.Fatal(code)
	}
	c.DeriveAddress = func(seeds, program, output []byte) uint64 { copy(output, c.Accounts[2].Key); return 0 }
	if code := TransferCheckedSigned(c, 5, 2, 1, 3, 2, 1, 6, seedsFor(1)); code != 0 {
		t.Fatal(code)
	}
	if len(*calls) != 2 || (*calls)[0].program != 5 || !bytes.Equal((*calls)[0].metas, metas(meta(1, 1), meta(3, 1), meta(1, 2))) || !bytes.Equal((*calls)[1].metas, metas(meta(2, 1), meta(1, 0), meta(3, 1), meta(2, 2))) {
		t.Fatal(*calls)
	}
	var none [32]byte
	if code := InitializeMetadataPointer(c, 5, 1, none, none); code != 2001 {
		t.Fatal("classic Token has no metadata pointer", code)
	}
}

func TestCheckedInstructionGuards(t *testing.T) {
	var key [32]byte
	ops := map[string]func(solana.Context) uint64{
		"mint2":    func(c solana.Context) uint64 { return InitializeMint2(c, 0, 1, 6, key, key, false) },
		"account3": func(c solana.Context) uint64 { return InitializeAccount3(c, 0, 2, 1, key) },
		"account":  func(c solana.Context) uint64 { return InitializeAccount(c, 0, 2, 1, 4, 6) },
		"size":     func(c solana.Context) uint64 { return GetAccountDataSize(c, 0, 1, nil) },
		"mint-to":  func(c solana.Context) uint64 { return MintToChecked(c, 0, 1, 3, 4, 1, 6) },
		"burn":     func(c solana.Context) uint64 { return BurnChecked(c, 0, 2, 1, 4, 1, 6) },
		"transfer": func(c solana.Context) uint64 { return TransferChecked(c, 0, 2, 1, 3, 4, 1, 6) },
		"transfer-pda": func(c solana.Context) uint64 {
			return TransferCheckedSigned(c, 0, 2, 1, 3, 7, 1, 6, seedsFor(254))
		},
	}
	for _, tc := range []struct {
		name string
		edit func(*solana.Context)
		want map[string]uint64
	}{
		{"program-executable", func(c *solana.Context) { c.Accounts[0].Executable = false },
			map[string]uint64{"mint2": 2001, "account3": 2001, "account": 2001, "size": 2001, "mint-to": 2001, "burn": 2001, "transfer": 2001, "transfer-pda": 2001}},
		{"program-key", func(c *solana.Context) { c.Accounts[0].Key = bytes.Repeat([]byte{1}, 32) },
			map[string]uint64{"mint2": 2001, "account3": 2001, "size": 2001, "transfer": 2001}},
		{"mint-owner", func(c *solana.Context) { c.Accounts[1].Owner = keySlice(ProgramID()) },
			map[string]uint64{"mint2": ErrMint, "account3": ErrMint, "account": ErrMint, "size": ErrMint, "mint-to": ErrMint, "burn": ErrMint, "transfer": ErrMint}},
		{"mint-readonly", func(c *solana.Context) { c.Accounts[1].Writable = false },
			map[string]uint64{"mint2": ErrWritable, "mint-to": ErrWritable, "burn": ErrWritable}},
		{"source-owner", func(c *solana.Context) { c.Accounts[2].Owner = make([]byte, 32) },
			map[string]uint64{"account3": ErrAccount, "account": ErrAccount, "burn": ErrAccount, "transfer": ErrAccount}},
		{"source-readonly", func(c *solana.Context) { c.Accounts[2].Writable = false },
			map[string]uint64{"account3": ErrWritable, "account": ErrWritable, "burn": ErrWritable, "transfer": ErrWritable}},
		{"source-executable", func(c *solana.Context) { c.Accounts[2].Executable = true },
			map[string]uint64{"burn": ErrAccount, "transfer": ErrAccount}},
		{"destination-readonly", func(c *solana.Context) { c.Accounts[3].Writable = false },
			map[string]uint64{"mint-to": ErrWritable, "transfer": ErrWritable}},
		{"destination-owner", func(c *solana.Context) { c.Accounts[3].Owner = make([]byte, 32) },
			map[string]uint64{"mint-to": ErrAccount, "transfer": ErrAccount}},
		{"unsigned", func(c *solana.Context) { c.Accounts[4].Signer = false },
			map[string]uint64{"mint-to": ErrAuthority, "burn": ErrAuthority, "transfer": ErrAuthority}},
		{"rent-sysvar", func(c *solana.Context) { c.Accounts[6].Key = make([]byte, 32) },
			map[string]uint64{"account": ErrAccount}},
		{"wrong-pda", func(c *solana.Context) { c.DeriveAddress = func(_, _, out []byte) uint64 { out[0] = 8; return 0 } },
			map[string]uint64{"transfer-pda": ErrAuthority}},
	} {
		for name, want := range tc.want {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				c := instructionContext()
				tc.edit(&c)
				c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid instruction reached CPI"); return 0 }
				if code := ops[name](c); code != want {
					t.Fatal(code, want)
				}
			})
		}
	}
	c := instructionContext()
	c.InvokeInstruction = func(uint64, []byte, []byte, []byte) uint64 { t.Fatal("invalid instruction reached CPI"); return 0 }
	if GetAccountDataSize(c, 0, 1, make([]uint16, 17)) != ErrAccount {
		t.Fatal("extension count")
	}
	if MintToCheckedSigned(c, 0, 1, 3, 7, 1, 6, nil) != pda.ErrSeeds || BurnCheckedSigned(c, 0, 2, 1, 7, 1, 6, nil) != pda.ErrSeeds || TransferCheckedSigned(c, 0, 2, 1, 3, 7, 1, 6, nil) != pda.ErrSeeds {
		t.Fatal("nil seeds")
	}
	// Token errors, such as 18 (MintDecimalsMismatch), propagate unchanged.
	record(&c, 18)
	if TransferChecked(c, 0, 2, 1, 3, 4, 1, 7) != 18 {
		t.Fatal("propagated status")
	}
}

func keySlice(k [32]byte) []byte { return append([]byte(nil), k[:]...) }

func TestProgramPredicates(t *testing.T) {
	c := instructionContext()
	if !IsProgram(c, 0) || !IsProgram(c, 5) || IsProgram(c, 1) || IsProgram(c, 99) {
		t.Fatal("program predicate")
	}
	c.Accounts[5].Executable = false
	if IsProgram(c, 5) || !solana.IsTokenProgram(instructionContext(), 5) || solana.IsTokenProgram(c, 0) {
		t.Fatal("classic predicate unchanged")
	}
}
