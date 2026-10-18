package program_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	program "example.com/counter"
	"example.com/counter/client"
	"example.com/counter/model"
	"gosvm/solana"
)

func TestNext(t *testing.T) {
	for _, tc := range []struct{ count, limit, amount, want, code uint64 }{
		{0, 10, 3, 3, 0},
		{7, 10, 3, 10, 0},
		{8, 10, 3, 8, model.ErrLimit},
		{0, 10, 11, 0, model.ErrLimit},
		{5, 10, 0, 5, model.ErrZeroAmount},
		{1, ^uint64(0), ^uint64(0), 1, model.ErrLimit}, // no wraparound
	} {
		got, code := model.Next(tc.count, tc.limit, tc.amount)
		if got != tc.want || code != tc.code {
			t.Errorf("Next(%d, %d, %d) = %d, %d; want %d, %d", tc.count, tc.limit, tc.amount, got, code, tc.want, tc.code)
		}
	}
}

var programID = bytes.Repeat([]byte{7}, 32)

// incrementContext is an Increment instruction with a program-owned counter
// (count 5, limit 10) and its owner as signer: the accounts a client sends.
func incrementContext(amount uint64) solana.Context {
	owner := model.PublicKey{1}
	counter := model.Counter{Owner: owner, Count: 5, Limit: 10, Bump: 254}
	return solana.Context{
		ID:              programID,
		InstructionData: client.EncodeInstructionIncrement(model.IncrementArgs{Amount: amount}),
		Accounts: []solana.Account{
			{Key: bytes.Repeat([]byte{2}, 32), Owner: programID, Data: client.EncodeAccountCounter(counter), Writable: true},
			{Key: owner[:], Owner: make([]byte, 32), Signer: true},
		},
	}
}

func TestIncrement(t *testing.T) {
	c := incrementContext(3)
	if code := program.Process(c); code != 0 {
		t.Fatalf("Process = %d", code)
	}
	counter, err := client.DecodeAccountCounter(c.Accounts[0].Data)
	if err != nil || counter.Count != 8 || counter.Limit != 10 {
		t.Fatalf("counter = %+v, %v", counter, err)
	}
}

func TestIncrementRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint64
		edit func(*solana.Context)
	}{
		{"zero amount", program.ErrZeroAmount, func(c *solana.Context) {
			c.InstructionData = client.EncodeInstructionIncrement(model.IncrementArgs{})
		}},
		{"over limit", program.ErrLimit, func(c *solana.Context) {
			c.InstructionData = client.EncodeInstructionIncrement(model.IncrementArgs{Amount: 6})
		}},
		{"owner did not sign", 6001, func(c *solana.Context) { c.Accounts[1].Signer = false }},
		{"counter not writable", 6001, func(c *solana.Context) { c.Accounts[0].Writable = false }},
		{"counter owned by another program", 6002, func(c *solana.Context) { c.Accounts[0].Owner = bytes.Repeat([]byte{9}, 32) }},
		{"someone else signs", 6007, func(c *solana.Context) { c.Accounts[1].Key = bytes.Repeat([]byte{3}, 32) }},
		{"missing account", 6000, func(c *solana.Context) { c.Accounts = c.Accounts[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := incrementContext(3)
			tc.edit(&c)
			before := bytes.Clone(c.Accounts[0].Data)
			if code := program.Process(c); code != tc.code {
				t.Fatalf("Process = %d, want %d", code, tc.code)
			}
			if !bytes.Equal(before, c.Accounts[0].Data) {
				t.Fatal("a rejected instruction changed the counter")
			}
		})
	}
}

func TestPreview(t *testing.T) {
	var returned []byte
	c := solana.Context{
		ID:              programID,
		InstructionData: client.EncodeInstructionPreview(model.PreviewArgs{Count: 5, Limit: 10, Amount: 3}),
		Accounts:        []solana.Account{{Key: bytes.Repeat([]byte{1}, 32), Owner: make([]byte, 32), Signer: true, Writable: true}},
		WriteReturnData: func(data []byte) { returned = data },
	}
	if code := program.Process(c); code != 0 || len(returned) != 8 || binary.LittleEndian.Uint64(returned) != 8 {
		t.Fatalf("Process = %d, return data %x", code, returned)
	}
	c.InstructionData = client.EncodeInstructionPreview(model.PreviewArgs{Count: 9, Limit: 10, Amount: 3})
	if code := program.Process(c); code != program.ErrLimit {
		t.Fatalf("Process = %d, want %d", code, program.ErrLimit)
	}
}
