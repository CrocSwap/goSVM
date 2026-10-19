// Package cpilimits drives one outgoing CPI with a chosen number of account
// metas and PDA signer groups, through the sdk/cpi builders or the raw
// solana.Invoke encoding. Accounts: 0 the callee program, 1..4 the PDAs of
// seeds ("cpi-limits", group) under this program, then plain accounts that
// the remaining metas cycle through (so counts above the distinct accounts
// repeat them). Instruction: mode, metas, groups, then one bump per group.
// The callee receives [metas, groups, plain] and asserts its account count,
// that its first groups accounts are signers and that the other metas cycle
// through the plain accounts (callee.c.txt).
package cpilimits

import (
	"gosvm/sdk/cpi"
	"gosvm/sdk/pda"
	"gosvm/solana"
)

// Mode selects the encoding path.
const (
	ModeBuilder byte = 0 // cpi.Metas, cpi.Signers, cpi.Invoke
	ModeRaw     byte = 1 // hand-encoded metas and signer groups, solana.Invoke
)

// FirstPlain is the first plain account; 1..4 are signer PDAs.
const FirstPlain uint64 = 5

// MaxRawMetas and MaxRawGroups bound the raw encoder, one past the limits.
const (
	MaxRawMetas  = 65
	MaxRawGroups = 5
)

const ErrInput uint64 = 4000

// label is the first seed of every signer PDA.
var label = [10]byte{'c', 'p', 'i', '-', 'l', 'i', 'm', 'i', 't', 's'}

func meta(i, groups, plain uint64) (uint64, bool) {
	if i < groups {
		return 1 + i, true
	}
	return FirstPlain + (i-groups)%plain, false
}

func Process(c solana.Context) uint64 {
	ix := solana.Instruction(c)
	if len(ix) < 3 || solana.Count(c) <= FirstPlain {
		return ErrInput
	}
	mode, n, groups := ix[0], uint64(ix[1]), uint64(ix[2])
	if uint64(len(ix)) != 3+groups || groups > MaxRawGroups || n > MaxRawMetas || groups > n {
		return ErrInput
	}
	plain := solana.Count(c) - FirstPlain
	var data [3]byte
	data[0], data[1], data[2] = byte(n), byte(groups), byte(plain)
	if mode == ModeRaw {
		return raw(c, ix, n, groups, plain, data[:])
	}
	var metas cpi.Metas
	for i := uint64(0); i < n; i++ {
		index, signer := meta(i, groups, plain)
		if code := metas.Add(index, false, signer); code != 0 {
			return code
		}
	}
	var signers cpi.Signers
	for g := uint64(0); g < groups; g++ {
		var seeds pda.Seeds
		if seeds.AddBytes(label[:]) != 0 || seeds.AddByte(byte(g)) != 0 || seeds.AddByte(ix[3+g]) != 0 {
			return pda.ErrSeeds
		}
		if code := signers.Add(&seeds); code != 0 {
			return code
		}
	}
	return cpi.Invoke(c, 0, &metas, data[:], &signers)
}

// raw encodes up to 65 metas and 5 signer groups directly, so the boundary's
// own limits are reached without the builders' earlier checks.
func raw(c solana.Context, ix []byte, n, groups, plain uint64, data []byte) uint64 {
	var metas [MaxRawMetas * 9]byte
	for i := uint64(0); i < n; i++ {
		index, signer := meta(i, groups, plain)
		for j := uint64(0); j < 8; j++ {
			metas[i*9+j] = byte(index >> (j * 8))
		}
		if signer {
			metas[i*9+8] = 2
		}
	}
	var seeds [1 + MaxRawGroups*16]byte
	used := uint64(1)
	seeds[0] = byte(groups)
	for g := uint64(0); g < groups; g++ {
		seeds[used] = 3
		seeds[used+1] = byte(len(label))
		used += 2
		for j := uint64(0); j < uint64(len(label)); j++ {
			seeds[used] = label[j]
			used++
		}
		seeds[used], seeds[used+1], seeds[used+2], seeds[used+3] = 1, byte(g), 1, ix[3+g]
		used += 4
	}
	signerSeeds := seeds[:used]
	if groups == 0 {
		signerSeeds = nil
	}
	return solana.Invoke(c, 0, metas[:n*9], data, signerSeeds)
}
