// Package tokencpi exercises SDK-2 checked Token and Token-2022 instruction
// builders through real CPIs. It is an SBF fixture, not application policy.
// Instruction data: opcode, five account indices, little-endian uint64 amount
// (or size), decimals, seed mode (0 none, 1 "mint", 2 "escrow") and bump.
package tokencpi

import (
	"gosvm/sdk/pda"
	"gosvm/sdk/system"
	"gosvm/sdk/token"
	"gosvm/solana"
)

func key(c solana.Context, index uint64) ([32]byte, uint64) {
	var out [32]byte
	if index >= solana.Count(c) || len(solana.Key(c, index)) != 32 {
		return out, 4001
	}
	k := solana.Key(c, index)
	for j := uint64(0); j < 32; j++ {
		out[j] = k[j]
	}
	return out, 0
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) != 17 {
		return 4000
	}
	a0, a1, a2, a3, a4 := uint64(ix[1]), uint64(ix[2]), uint64(ix[3]), uint64(ix[4]), uint64(ix[5])
	amount := uint64(0)
	for j := uint64(0); j < 8; j++ {
		amount = amount | uint64(ix[6+j])<<(j*8)
	}
	decimals := ix[14]
	mintTag := [4]byte{'m', 'i', 'n', 't'}
	escrowTag := [6]byte{'e', 's', 'c', 'r', 'o', 'w'}
	var seeds pda.Seeds
	signed := ix[15] != 0
	if ix[15] == 1 && seeds.AddBytes(mintTag[:]) != 0 || ix[15] == 2 && seeds.AddBytes(escrowTag[:]) != 0 || signed && seeds.AddByte(ix[16]) != 0 {
		return pda.ErrSeeds
	}
	switch ix[0] {
	case 0:
		authority, code := key(c, a2)
		if code != 0 {
			return code
		}
		return token.InitializeMint2(c, a0, a1, decimals, authority, authority, false)
	case 1:
		if signed {
			return token.MintToCheckedSigned(c, a0, a1, a2, a3, amount, decimals, &seeds)
		}
		return token.MintToChecked(c, a0, a1, a2, a3, amount, decimals)
	case 2:
		if signed {
			return token.BurnCheckedSigned(c, a0, a1, a2, a3, amount, decimals, &seeds)
		}
		return token.BurnChecked(c, a0, a1, a2, a3, amount, decimals)
	case 3:
		if signed {
			return token.TransferCheckedSigned(c, a0, a1, a2, a3, a4, amount, decimals, &seeds)
		}
		return token.TransferChecked(c, a0, a1, a2, a3, a4, amount, decimals)
	case 4:
		owner, code := key(c, a3)
		if code != 0 {
			return code
		}
		return token.InitializeAccount3(c, a0, a1, a2, owner)
	case 5:
		return token.InitializeAccount(c, a0, a1, a2, a3, a4)
	case 6:
		var extensions [1]uint16
		extensions[0] = 7 // ImmutableOwner
		return token.GetAccountDataSize(c, a0, a1, extensions[:uint64(decimals)])
	case 7:
		authority, code := key(c, a2)
		if code != 0 {
			return code
		}
		metadata, status := key(c, a3)
		if status != 0 {
			return status
		}
		return token.InitializeMetadataPointer(c, a0, a1, authority, metadata)
	case 8:
		owner, code := key(c, a3)
		if code != 0 {
			return code
		}
		if !signed {
			return system.Create(c, a0, a1, a2, amount, owner)
		}
		return system.CreateSigned(c, a0, a1, a2, amount, owner, &seeds)
	}
	return 4002
}
