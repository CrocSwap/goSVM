// Command fixtures writes testdata/svm.json, the local SVM scenarios for
// gosvm test --svm, using the generated client's instructions and encoders.
//
//	go run ./cmd/fixtures > testdata/svm.json
//
// main_test.go fails while the committed file differs from this output.
package main

import (
	"fmt"
	"os"

	"example.org/gosvm/logging/client"
	"example.org/gosvm/logging/model"
	"gosvm/sdk/fixture"
)

func main() {
	b, err := suiteJSON()
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.Write(b)
}

// suiteJSON returns the fixture file's contents.
func suiteJSON() ([]byte, error) {
	// Keys derive from this namespace and each name: local test identities only.
	f := fixture.New("logging")
	author := f.Signer("author", 1_000_000_000)
	journal := func(notes uint64) []byte {
		return client.EncodeAccountJournal(model.Journal{Author: author.Key, Notes: notes})
	}
	account := f.Account("journal", f.Owned(journal(0)))
	note := func(text string, words [4]uint64) fixture.Instruction {
		args := model.NoteArgs{Length: byte(len(text)), Words: words}
		copy(args.Text[:], text)
		return client.NoteInstruction(client.NoteKeys{Journal: account.Key, Author: author.Key}, args)
	}
	key := "Program log: " + fixture.Base58(author.Key[:])

	s := f.Scenario("note-logs-four-lines")
	s.Step(note("gas paid: 世界", [4]uint64{1, 0xabc, 0, ^uint64(0)})).ExpectData(account, journal(1)).
		ExpectLog("Program log: note", 1).
		ExpectLog("Program log: gas paid: 世界", 1).
		ExpectLog("Program log: 0x1, 0x1, 0xabc, 0x0, 0xffffffffffffffff", 1).
		ExpectLog(key, 1)
	s.Step(note("", [4]uint64{})).ExpectData(account, journal(2)).
		ExpectLog("Program log: 0x2, 0x0, 0x0, 0x0, 0x0", 1).
		ExpectLog(key, 1)

	// Each byte that is not valid UTF-8 fails the log syscall, rolling back
	// the note count and its earlier lines.
	for _, text := range []string{"bad \xff byte", "\xed\xa0\x80", "cut \xe4\xb8"} {
		f.Scenario(fmt.Sprintf("invalid-utf8-%x-fails", text)).
			Step(note(text, [4]uint64{})).FailsWith(fixture.InstructionError(0, "ProgramFailedToComplete")).
			ExpectLog("Program log: note", 1).ExpectLog("utf-8", 1)
	}
	return f.JSON()
}
