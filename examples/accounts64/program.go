// Package accounts64 takes up to 64 incoming accounts: build it with
// -max-accounts 64 (docs/ACCOUNT_DECODER64.md). Instruction data is a mode
// byte, the expected account count, then the mode's arguments.
package accounts64

import (
	"gosvm/sdk/system"
	"gosvm/solana"
)

const ErrCount uint64 = 7000
const ErrMode uint64 = 7001
const ErrFailed uint64 = 7002

func read(b []byte, off uint64) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(b[off+j])<<(j*8)
	}
	return v
}

// bump adds one to the first data byte of every writable program-owned
// descriptor, so an account listed k times gains k (duplicates alias).
func bump(c solana.Context) {
	id := solana.ProgramID(c)
	for j := uint64(0); j < solana.Count(c); j++ {
		owner, data := solana.Owner(c, j), solana.Data(c, j)
		if !solana.Writable(c, j) || solana.Executable(c, j) || len(data) == 0 {
			continue
		}
		owned := true
		for k := uint64(0); k < 32; k++ {
			if owner[k] != id[k] {
				owned = false
			}
		}
		if owned {
			data[0]++
		}
	}
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) < 2 || solana.Count(c) != uint64(ix[1]) {
		return ErrCount
	}
	switch ix[0] {
	case 0: // bump every owned account
		bump(c)
		return 0
	case 1: // System transfer: program, payer, destination indices, amount
		if len(ix) != 13 {
			return ErrMode
		}
		return system.Transfer(c, uint64(ix[2]), uint64(ix[3]), uint64(ix[4]), read(ix, 5))
	case 2: // bump, then fail: the writes roll back
		bump(c)
		return ErrFailed
	}
	return ErrMode
}
