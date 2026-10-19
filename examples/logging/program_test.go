package program_test

import (
	"bytes"
	"testing"

	program "example.org/gosvm/logging"
	"example.org/gosvm/logging/client"
	"example.org/gosvm/logging/model"
	"gosvm/sdk/fixture"
	"gosvm/solana"
)

var id = bytes.Repeat([]byte{7}, 32)

func note(text string, words [4]uint64) (solana.Context, *[]string) {
	author := model.PublicKey{0, 0, 9, 8}
	args := model.NoteArgs{Length: byte(len(text)), Words: words}
	copy(args.Text[:], text)
	lines := &[]string{}
	return solana.Context{ID: id, InstructionData: client.EncodeInstructionNote(args), WriteLog: func(m string) { *lines = append(*lines, m) },
		Accounts: []solana.Account{
			{Key: bytes.Repeat([]byte{1}, 32), Owner: id, Data: client.EncodeAccountJournal(model.Journal{Author: author, Notes: 41}), Writable: true},
			{Key: author[:], Owner: make([]byte, 32), Signer: true},
		}}, lines
}

func TestNoteLogs(t *testing.T) {
	c, lines := note("gas paid: 世界", [4]uint64{1, 0xabc, 0, ^uint64(0)})
	if code := program.Process(c); code != 0 {
		t.Fatal(code)
	}
	want := []string{"note", "gas paid: 世界", "0x2a, 0x1, 0xabc, 0x0, 0xffffffffffffffff", fixture.Base58(c.Accounts[1].Key)}
	if len(*lines) != len(want) {
		t.Fatal(*lines)
	}
	for j := range want {
		if (*lines)[j] != want[j] {
			t.Fatalf("line %d: %q, want %q", j, (*lines)[j], want[j])
		}
	}
	journal, err := client.DecodeAccountJournal(c.Accounts[0].Data)
	if err != nil || journal.Notes != 42 {
		t.Fatal(journal, err)
	}
}

// Invalid UTF-8 fails the instruction on chain; the native SDK panics.
func TestNoteRejectsInvalidUTF8(t *testing.T) {
	c, _ := note("bad \xff byte", [4]uint64{})
	defer func() {
		if recover() == nil {
			t.Fatal("logged invalid UTF-8")
		}
	}()
	program.Process(c)
}
