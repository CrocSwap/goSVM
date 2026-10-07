// Package contextvalues exercises SDK view construction in actual SBF.
package contextvalues

import (
	views "gosvm/examples/slice-views"
	"gosvm/solana"
)

func Process(c solana.Context) uint64 {
	if solana.Count(c) != 1 {
		return 201
	}
	s := solana.Data(c, 0)
	i := solana.Instruction(c)
	key := solana.Key(c, 0)
	owner := solana.Owner(c, 0)
	program := solana.ProgramID(c)
	if len(s) != 24 || cap(s) != 24 || len(i) != 16 || cap(i) != 16 {
		return 202
	}
	if cap(key) != 32 || cap(owner) != 32 || cap(program) != 32 {
		return 203
	}
	if !solana.Writable(c, 0) || solana.Executable(c, 0) {
		return 204
	}
	for j := uint64(0); j < 32; j++ {
		if owner[j] != program[j] {
			return 205
		}
	}
	if i[0] == 12 {
		var output [32]byte
		code := solana.SHA256(c, nil, 0, 0, output[:], 0)
		if code != 0 {
			return code
		}
		for j := uint64(0); j < 24; j++ {
			s[j] = output[j]
		}
		return 0
	}
	return views.Process(s, i)
}
