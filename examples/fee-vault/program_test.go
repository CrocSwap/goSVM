package program_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	program "example.org/gosvm/fee-vault"
	"example.org/gosvm/fee-vault/client"
	"example.org/gosvm/fee-vault/model"
	"gosvm/solana"
)

var (
	id          = bytes.Repeat([]byte{7}, 32)
	system      = make([]byte, 32)
	authority   = model.PublicKey{4}
	beneficiary = model.PublicKey{5}
	vaultData   = client.EncodeAccountVault(model.Vault{Authority: authority, Beneficiary: beneficiary, Bump: 254})
)

// minimum is the default rent-exempt balance of the 73-byte vault.
const minimum = (128 + 73) * 3480 * 2

func readRent(b []byte) uint64 {
	binary.LittleEndian.PutUint64(b, 3480)
	binary.LittleEndian.PutUint64(b[8:], math.Float64bits(2))
	b[16] = 50
	return 0
}

func vault(lamports uint64) solana.Account {
	return solana.Account{Key: bytes.Repeat([]byte{1}, 32), Owner: id, Data: append([]byte(nil), vaultData...), Writable: true, Lamports: lamports}
}

func wallet(key model.PublicKey, lamports uint64, writable, signer bool) solana.Account {
	return solana.Account{Key: append([]byte(nil), key[:]...), Owner: system, Writable: writable, Signer: signer, Lamports: lamports}
}

func balances(c solana.Context) []uint64 {
	var out []uint64
	for _, a := range c.Accounts {
		out = append(out, a.Lamports)
	}
	return out
}

func TestClaimMovesFeesAboveRent(t *testing.T) {
	c := solana.Context{ID: id, ReadRent: readRent, InstructionData: client.EncodeInstructionClaim(model.ClaimArgs{}),
		Accounts: []solana.Account{vault(minimum + 5000), wallet(beneficiary, 100, true, false)}}
	if code := program.Process(c); code != 0 {
		t.Fatal(code)
	}
	if got := balances(c); got[0] != minimum || got[1] != 5100 {
		t.Fatal(got)
	}
	// Nothing left to claim: a zero move succeeds and changes nothing.
	if code := program.Process(c); code != 0 || balances(c)[1] != 5100 {
		t.Fatal(code, balances(c))
	}
	c.Accounts[1].Key[0] = 6
	if code := program.Process(c); code != client.ErrAccountRelation {
		t.Fatal("claimed to another account", code)
	}
}

func TestMoveRules(t *testing.T) {
	for _, tc := range []struct {
		name         string
		from, to     byte
		amount, code uint64
		vault, rcpt  uint64
	}{
		{"vault-to-recipient", 0, 2, 1000, 0, minimum + 4000, 1500},
		{"vault-to-authority", 0, 1, 1000, 0, minimum + 4000, 500},
		{"within-vault", 0, 0, 1000, 0, minimum + 5000, 500},
		{"insufficient", 0, 2, minimum + 5001, 2018, minimum + 5000, 500},
		{"unowned-recipient", 2, 0, 1, 2012, minimum + 5000, 500},
		{"unowned-signer", 1, 0, 1, 2012, minimum + 5000, 500},
		{"readonly-observer", 0, 3, 1, 2011, minimum + 5000, 500},
		{"index", 0, 4, 1, 2005, minimum + 5000, 500},
	} {
		c := solana.Context{ID: id, InstructionData: client.EncodeInstructionMove(model.MoveArgs{From: tc.from, To: tc.to, Amount: tc.amount}),
			Accounts: []solana.Account{vault(minimum + 5000), wallet(authority, 1500, true, true), wallet(model.PublicKey{8}, 500, true, false), wallet(model.PublicKey{9}, 700, false, false)}}
		if code := program.Process(c); code != tc.code {
			t.Fatalf("%s: %d, want %d", tc.name, code, tc.code)
		}
		if got := balances(c); got[0] != tc.vault || got[2] != tc.rcpt && tc.to == 2 || got[0]+got[1]+got[2]+got[3] != minimum+5000+1500+500+700 {
			t.Fatalf("%s: %v", tc.name, got)
		}
		if !bytes.Equal(c.Accounts[0].Data, vaultData) {
			t.Fatalf("%s: vault data changed", tc.name)
		}
	}
}
