// Package atacreate exercises SDK-2 associated token account derivation and
// creation through the real ATA program, with a transaction-signer or PDA
// payer, and canonical PDA bump search. It is an SBF fixture, not application
// policy. Instruction data: opcode, seven account indices, payer seed mode
// (1 for the "ata_payer" PDA) and bump.
package atacreate

import (
	"gosvm/sdk/ata"
	"gosvm/sdk/pda"
	"gosvm/solana"
)

func key(c solana.Context, index uint64) ([32]byte, bool) {
	var out [32]byte
	if index >= solana.Count(c) || len(solana.Key(c, index)) != 32 {
		return out, false
	}
	k := solana.Key(c, index)
	for j := uint64(0); j < 32; j++ {
		out[j] = k[j]
	}
	return out, true
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 10 {
		return 4000
	}
	a0, a1, a2, a3, a4, a5, a6 := uint64(ix[1]), uint64(ix[2]), uint64(ix[3]), uint64(ix[4]), uint64(ix[5]), uint64(ix[6]), uint64(ix[7])
	tag := [9]byte{'a', 't', 'a', '_', 'p', 'a', 'y', 'e', 'r'}
	var seeds pda.Seeds
	if seeds.AddBytes(tag[:]) != 0 || seeds.AddByte(ix[9]) != 0 {
		return pda.ErrSeeds
	}
	switch ix[0] {
	case 0:
		if ix[8] == 1 {
			return ata.CreateIdempotentSigned(c, a0, a1, a2, a3, a4, a5, a6, &seeds)
		}
		return ata.CreateIdempotent(c, a0, a1, a2, a3, a4, a5, a6)
	case 1:
		if ix[8] == 1 {
			return ata.CreateSigned(c, a0, a1, a2, a3, a4, a5, a6, &seeds)
		}
		return ata.Create(c, a0, a1, a2, a3, a4, a5, a6)
	}
	if a0 >= solana.Count(c) || !solana.Writable(c, a0) || len(solana.Data(c, a0)) != 40 {
		return 4000
	}
	out := solana.Data(c, a0)
	var address [32]byte
	var bump byte
	code := uint64(4002)
	if ix[0] == 2 {
		wallet, ok1 := key(c, a1)
		program, ok2 := key(c, a2)
		mint, ok3 := key(c, a3)
		if !ok1 || !ok2 || !ok3 {
			return 4001
		}
		address, bump, code = ata.Address(c, wallet, program, mint)
	} else if ix[0] == 3 {
		caller, ok := key(c, a0)
		id := solana.ProgramID(c)
		if !ok || len(id) != 32 {
			return 4001
		}
		for j := uint64(0); j < 32; j++ {
			caller[j] = id[j]
		}
		vault := [5]byte{'v', 'a', 'u', 'l', 't'}
		var search pda.Seeds
		if search.AddBytes(vault[:]) != 0 {
			return pda.ErrSeeds
		}
		address, bump, code = search.FindAddress(c, caller)
	}
	if code != 0 {
		return code
	}
	for j := uint64(0); j < 32; j++ {
		out[j] = address[j]
	}
	out[32] = bump
	for j := uint64(33); j < 40; j++ {
		out[j] = 0
	}
	return 0
}
