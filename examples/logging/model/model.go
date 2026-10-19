// Package model is ordinary shared Go, with no runtime or compiler dependency.
package model

type PublicKey [32]byte

// Journal counts the notes its author has logged.
type Journal struct {
	Author PublicKey
	Notes  uint64
}

// NoteArgs is a note of Length bytes of Text and four words to log.
type NoteArgs struct {
	Text   [64]byte
	Length byte
	Words  [4]uint64
}
