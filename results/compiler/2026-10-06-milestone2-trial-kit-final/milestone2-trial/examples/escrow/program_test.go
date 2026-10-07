package program_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	program "example.org/gosvm/escrow"
	"example.org/gosvm/escrow/client"
	"example.org/gosvm/escrow/model"
	"gosvm/solana"
	"math"
	"reflect"
	"testing"
)

func contextFor(t *testing.T) (solana.Context, *uint64) {
	id := bytes.Repeat([]byte{11}, 32)
	creator, beneficiary := model.PublicKey{7}, model.PublicKey{9}
	preimage := [32]byte{19, 23, 31}
	args := model.OpenArgs{Beneficiary: beneficiary, Commitment: sha256.Sum256(preimage[:]), Amount: 1000000, Nonce: 123, Bump: 255}
	seeds := append([]byte("escrow"), creator[:]...)
	var nonce [8]byte
	binary.LittleEndian.PutUint64(nonce[:], args.Nonce)
	seeds = append(seeds, nonce[:]...)
	seeds = append(seeds, args.Bump)
	pdaBytes := append(append(seeds, id...), []byte("ProgramDerivedAddress")...)
	key := sha256.Sum256(pdaBytes)
	calls := new(uint64)
	c := solana.Context{ID: id, InstructionData: client.EncodeInstructionOpen(args), Accounts: []solana.Account{
		{Key: key[:], Owner: make([]byte, 32), Writable: true},
		{Key: creator[:], Owner: make([]byte, 32), Writable: true, Signer: true, Lamports: 100000000},
		{Key: beneficiary[:], Owner: make([]byte, 32), Lamports: 100000000},
		{Key: make([]byte, 32), Owner: make([]byte, 32), Executable: true},
	}}
	c.Hash = func(b []byte) []byte { value := sha256.Sum256(b); return value[:] }
	c.ReadRent = func(b []byte) uint64 {
		binary.LittleEndian.PutUint64(b, 3480)
		binary.LittleEndian.PutUint64(b[8:], math.Float64bits(2))
		return 0
	}
	// This native callback checks independent encoded-seed hashing. Off-curve
	// rejection and actual PDA signing are proved separately by the SBF suite.
	c.DeriveAddress = func(encoded, id, out []byte) uint64 {
		data, off := []byte{}, 1
		for j := 0; j < int(encoded[0]); j++ {
			n := int(encoded[off])
			off++
			data = append(data, encoded[off:off+n]...)
			off += n
		}
		data = append(append(data, id...), []byte("ProgramDerivedAddress")...)
		h := sha256.Sum256(data)
		copy(out, h[:])
		return 0
	}
	c.InvokeInstruction = func(program uint64, metas, data, seeds []byte) uint64 {
		*calls++
		if program != 3 || len(metas) != 18 || binary.LittleEndian.Uint64(metas) != 1 || binary.LittleEndian.Uint64(metas[9:]) != 0 || metas[8] != 3 || metas[17]&1 != 1 {
			t.Fatal("System CPI metadata", program, metas)
		}
		amount := binary.LittleEndian.Uint64(data[4:])
		if c.Accounts[1].Lamports < amount {
			return 1
		}
		if binary.LittleEndian.Uint32(data) == 0 {
			if len(data) != 52 || binary.LittleEndian.Uint64(data[12:]) != 121 || !bytes.Equal(data[20:], c.ID) || len(seeds) == 0 || metas[17] != 3 {
				t.Fatal("create encoding", data, seeds)
			}
			c.Accounts[0].Data = make([]byte, 121)
			c.Accounts[0].Owner = append([]byte(nil), data[20:]...)
		} else if binary.LittleEndian.Uint32(data) != 2 || len(data) != 12 || len(seeds) != 0 || metas[17] != 1 {
			t.Fatal("transfer encoding", data, seeds)
		}
		c.Accounts[1].Lamports -= amount
		c.Accounts[0].Lamports += amount
		return 0
	}
	return c, calls
}

func copyAccounts(c solana.Context) []solana.Account {
	accounts := append([]solana.Account(nil), c.Accounts...)
	for j := range accounts {
		a := &accounts[j]
		a.Key = bytes.Clone(a.Key)
		a.Owner = bytes.Clone(a.Owner)
		a.Data = bytes.Clone(a.Data)
	}
	return accounts
}

func TestEscrowOpenClaimCancel(t *testing.T) {
	for _, claim := range []bool{true, false} {
		c, calls := contextFor(t)
		if code := program.Process(c); code != 0 || *calls != 2 {
			t.Fatal(code, *calls)
		}
		e, err := client.DecodeAccountEscrow(c.Accounts[0].Data)
		if err != nil || e.Creator != model.PublicKey(c.Accounts[1].Key) || e.Beneficiary != model.PublicKey(c.Accounts[2].Key) || e.Amount != 1000000 || e.Nonce != 123 || e.Bump != 255 {
			t.Fatal(e, err)
		}
		if c.Accounts[0].Lamports != 2733040 || c.Accounts[1].Lamports != 97266960 {
			t.Fatal(c.Accounts)
		}
		if claim {
			c.Accounts[1].Writable, c.Accounts[1].Signer = false, false
			c.Accounts[2].Writable, c.Accounts[2].Signer = true, true
			c.Accounts = c.Accounts[:3]
			c.InstructionData = client.EncodeInstructionClaim(model.ClaimArgs{Preimage: [32]byte{19, 23, 31}, Nonce: 123})
			if code := program.Process(c); code != 0 || c.Accounts[2].Lamports != 102733040 {
				t.Fatal(code, c.Accounts)
			}
		} else {
			c.Accounts = c.Accounts[:2]
			c.InstructionData = client.EncodeInstructionCancel(model.CancelArgs{})
			if code := program.Process(c); code != 0 || c.Accounts[1].Lamports != 100000000 {
				t.Fatal(code, c.Accounts)
			}
		}
		if c.Accounts[0].Lamports != 0 || len(c.Accounts[0].Data) != 0 || !bytes.Equal(c.Accounts[0].Owner, make([]byte, 32)) {
			t.Fatal("close wrote state back", c.Accounts)
		}
	}
}

func TestEscrowValidationBeforeCreation(t *testing.T) {
	for _, test := range []struct {
		name string
		code uint64
		edit func(*solana.Context)
	}{
		{"target funded", 6009, func(c *solana.Context) { c.Accounts[0].Lamports = 1 }},
		{"target data", 6009, func(c *solana.Context) { c.Accounts[0].Data = []byte{1} }},
		{"target owner", 6009, func(c *solana.Context) { c.Accounts[0].Owner[0] = 1 }},
		{"payer data", 6009, func(c *solana.Context) { c.Accounts[1].Data = []byte{1} }},
		{"unsigned", 6001, func(c *solana.Context) { c.Accounts[1].Signer = false }},
		{"readonly", 6001, func(c *solana.Context) { c.Accounts[0].Writable = false }},
		{"executable", 6001, func(c *solana.Context) { c.Accounts[0].Executable = true }},
		{"System substitute", 6005, func(c *solana.Context) { c.Accounts[3].Key = bytes.Repeat([]byte{1}, 32) }},
		{"beneficiary substitute", 6007, func(c *solana.Context) { c.Accounts[2].Key[0]++ }},
		{"alias", 6006, func(c *solana.Context) { c.Accounts[2].Key = c.Accounts[1].Key }},
		{"PDA", 6008, func(c *solana.Context) { c.Accounts[0].Key[0]++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, calls := contextFor(t)
			test.edit(&c)
			before := copyAccounts(c)
			if code := program.Process(c); code != test.code {
				t.Fatal(code, test.code)
			}
			if *calls != 0 || !reflect.DeepEqual(before, c.Accounts) {
				t.Fatal("invalid constraints moved funds", *calls)
			}
		})
	}
}

func TestEscrowClaimAndCancelAuthority(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   uint64
		cancel bool
		edit   func(*solana.Context)
	}{
		{"bad secret", program.ErrSecret, false, func(c *solana.Context) {
			c.InstructionData = client.EncodeInstructionClaim(model.ClaimArgs{Preimage: [32]byte{99}, Nonce: 123})
		}},
		{"bad nonce", program.ErrNonce, false, func(c *solana.Context) {
			c.InstructionData = client.EncodeInstructionClaim(model.ClaimArgs{Preimage: [32]byte{19, 23, 31}, Nonce: 124})
		}},
		{"claim substitute", 6007, false, func(c *solana.Context) { c.Accounts[2].Key = bytes.Repeat([]byte{99}, 32) }},
		{"claim unsigned", 6001, false, func(c *solana.Context) { c.Accounts[2].Signer = false }},
		{"cancel substitute", 6007, true, func(c *solana.Context) { c.Accounts[1].Key = bytes.Repeat([]byte{99}, 32) }},
		{"cancel unsigned", 6001, true, func(c *solana.Context) { c.Accounts[1].Signer = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := contextFor(t)
			if code := program.Process(c); code != 0 {
				t.Fatal(code)
			}
			if test.cancel {
				c.Accounts = c.Accounts[:2]
				c.InstructionData = client.EncodeInstructionCancel(model.CancelArgs{})
			} else {
				c.Accounts = c.Accounts[:3]
				c.Accounts[1].Signer = false
				c.Accounts[1].Writable = false
				c.Accounts[2].Signer = true
				c.Accounts[2].Writable = true
				c.InstructionData = client.EncodeInstructionClaim(model.ClaimArgs{Preimage: [32]byte{19, 23, 31}, Nonce: 123})
			}
			test.edit(&c)
			before := copyAccounts(c)
			if code := program.Process(c); code != test.code {
				t.Fatal(code, test.code)
			}
			if !reflect.DeepEqual(before, c.Accounts) {
				t.Fatal("rejected close changed state")
			}
		})
	}
}
