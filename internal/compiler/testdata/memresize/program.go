// Package program exercises the SDK runtime's bulk moves on SBF: the
// entrypoint's copy of a duplicate account descriptor and ResizeAccount's
// zero fill. Accounts 0 and 1 are the same owned writable account; the
// instruction is shrink u16le, grow u16le. The program shrinks the account
// through index 0, grows it through index 1 (the bytes between are zeroed),
// and returns 7 unless both indices see the new length.
package program

import "gosvm/solana"

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 4 || solana.Count(c) != 2 {
		return 1
	}
	shrink := uint64(ix[0]) | uint64(ix[1])<<8
	grow := uint64(ix[2]) | uint64(ix[3])<<8
	if code := solana.ResizeAccount(c, 0, shrink); code != 0 {
		return code
	}
	if code := solana.ResizeAccount(c, 1, grow); code != 0 {
		return code
	}
	if uint64(len(solana.Data(c, 0))) != grow || uint64(len(solana.Data(c, 1))) != grow {
		return 7
	}
	return 0
}
