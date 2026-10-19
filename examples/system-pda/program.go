// Package systempda exercises SDK-2 System CPIs that sign for a program PDA:
// creation of a pre-funded address, lamport transfers out of a data-less
// System-owned vault, and separate allocate/assign. It is an SBF fixture, not
// application policy. Accounts: System, payer, vault PDA ("vault", bump),
// plain signer/recipient. Data: opcode, bump, little-endian uint64 argument.
package systempda

import (
	"gosvm/sdk/pda"
	"gosvm/sdk/system"
	"gosvm/solana"
)

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if solana.Count(c) != 4 || len(ix) != 10 {
		return 4000
	}
	value := uint64(0)
	for j := uint64(0); j < 8; j++ {
		value = value | uint64(ix[2+j])<<(j*8)
	}
	tag := [5]byte{'v', 'a', 'u', 'l', 't'}
	var seeds pda.Seeds
	if seeds.AddBytes(tag[:]) != 0 || seeds.AddByte(ix[1]) != 0 {
		return pda.ErrSeeds
	}
	id := solana.ProgramID(c)
	if len(id) != 32 {
		return 4001
	}
	var owner [32]byte
	for j := uint64(0); j < 32; j++ {
		owner[j] = id[j]
	}
	switch ix[0] {
	case 0:
		return system.CreatePDA(c, 0, 1, 2, value, owner, &seeds)
	case 1:
		return system.TransferSigned(c, 0, 2, 3, value, &seeds)
	case 2:
		return system.AllocateSigned(c, 0, 2, value, &seeds)
	case 3:
		return system.AssignSigned(c, 0, 2, owner, &seeds)
	case 4:
		return system.Allocate(c, 0, 3, value)
	case 5:
		return system.Assign(c, 0, 3, owner)
	case 6:
		var none [32]byte // A data-less System-owned vault.
		return system.CreatePDA(c, 0, 1, 2, value, none, &seeds)
	}
	return 4002
}
