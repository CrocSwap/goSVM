package program

import (
	"example.org/gosvm/logging/model"
	"gosvm/solana"
)

const ErrLength uint64 = 1

// Note logs as Rust's msg!, sol_log_64 and Pubkey::log do: a constant line,
// the note's text, the note count with four words in hexadecimal, and the
// author's key in base58. Text that is not valid UTF-8 fails the instruction
// in the runtime's log syscall.
func Note(c solana.Context, a NoteAccounts, args model.NoteArgs) (NoteAccounts, uint64) {
	if args.Length > 64 {
		return a, ErrLength
	}
	a.Journal.Notes++
	solana.Log(c, "note")
	solana.Log(c, string(args.Text[:args.Length]))
	solana.Log64(c, a.Journal.Notes, args.Words[0], args.Words[1], args.Words[2], args.Words[3])
	return a, solana.LogPubkey(c, a.Author.Key[:])
}
