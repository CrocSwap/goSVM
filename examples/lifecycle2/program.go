// Package lifecycle2 exercises SDK-2 lifecycle boundaries. This is a runtime
// fixture, not the generated application/payer/authority framework.
package lifecycle2

import (
	"gosvm/sdk/pda"
	"gosvm/sdk/system"
	"gosvm/solana"
)

func read(b []byte, offset uint64) uint64 {
	n := uint64(0)
	for j := uint64(0); j < 8; j++ {
		n = n | uint64(b[offset+j])<<(j*8)
	}
	return n
}

func write(b []byte, offset, n uint64) {
	for j := uint64(0); j < 8; j++ {
		b[offset+j] = byte(n >> (j * 8))
	}
}

// Accounts are result, target, wallet, System, and duplicate target. The result
// reports current lengths/lamports after operations, including the alias.
func Process(c solana.Context) uint64 {
	if solana.Count(c) != 5 || len(solana.Instruction(c)) != 19 || len(solana.Data(c, 0)) != 56 {
		return 90
	}
	ix := solana.Instruction(c)
	length := read(ix, 1)
	amount := read(ix, 9)
	rent, code := system.MinimumBalance(c, length)
	if code != 0 {
		return code
	}
	before := solana.Lamports(c, 1)
	var owner [32]byte
	id := solana.ProgramID(c)
	for j := uint64(0); j < 32; j++ {
		owner[j] = id[j]
	}
	switch ix[0] {
	case 0: // Read current rent only.
	case 1:
		code = system.Create(c, 3, 2, 1, length, owner)
	case 2:
		var seeds pda.Seeds
		if seeds.AddBytes([]byte(nil)) != 0 || seeds.AddByte(17) != 0 || seeds.AddByte(ix[17]) != 0 {
			return pda.ErrSeeds
		}
		code = system.CreateSigned(c, 3, 2, 1, length, owner, &seeds)
	case 3:
		code = solana.ResizeAccount(c, 1, length)
	case 4:
		code = solana.ResizeAccount(c, 1, amount)
		if code == 0 {
			code = solana.ResizeAccount(c, 1, length)
		}
	case 5:
		code = solana.CloseAccount(c, 1, 2)
	case 6:
		code = solana.CloseAccount(c, 1, 4)
	case 7:
		code = system.Transfer(c, 3, 2, 1, amount)
	case 8: // Close and recreate the same signed address within one invocation.
		code = solana.CloseAccount(c, 1, 2)
		if code == 0 {
			code = system.Create(c, 3, 2, 1, length, owner)
		}
	default:
		return 91
	}
	if code != 0 {
		return code
	}
	result := solana.Data(c, 0)
	write(result, 0, rent)
	write(result, 8, before)
	write(result, 16, solana.Lamports(c, 1))
	write(result, 24, uint64(len(solana.Data(c, 1))))
	write(result, 32, uint64(len(solana.Data(c, 4))))
	write(result, 40, solana.Lamports(c, 2))
	write(result, 48, amount)
	if ix[18] != 0 {
		return 92
	}
	return 0
}
