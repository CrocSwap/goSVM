package program

import (
	"example.org/gosvm/find-address/model"
	"gosvm/sdk/pda"
	"gosvm/solana"
)

const ErrLength uint64 = 1
const ErrNotCanonical uint64 = 2

var recordSeed = [6]byte{'r', 'e', 'c', 'o', 'r', 'd'}

func programID(c solana.Context) [32]byte {
	var id [32]byte
	copy(id[:], solana.ProgramID(c))
	return id
}

// Create accepts only the canonical bump, as Anchor's seeds/bump constraint
// does: generated code has checked the PDA for args.Bump and created the
// account; FindAddress searches from 255 for the first off-curve address.
func Create(c solana.Context, a CreateAccounts, args model.CreateArgs) (CreateAccounts, uint64) {
	var seeds pda.Seeds
	seeds.AddBytes(recordSeed[:])
	seeds.AddKey(a.Authority.Key)
	_, bump, code := seeds.FindAddress(c, programID(c))
	if code != 0 {
		return a, code
	}
	if bump != args.Bump {
		return a, ErrNotCanonical
	}
	a.Record = model.Record{Authority: model.PublicKey(a.Authority.Key), Bump: bump}
	return a, 0
}

// Derive returns find_program_address(seeds, program_id) for its arguments
// as 33 bytes of return data: the address, then the bump.
func Derive(c solana.Context, a DeriveAccounts, args model.DeriveArgs) (DeriveAccounts, uint64) {
	if args.Length > 32 {
		return a, ErrLength
	}
	var seeds pda.Seeds
	if code := seeds.AddBytes(args.Seed[:args.Length]); code != 0 {
		return a, code
	}
	for j := byte(0); j < args.Extra; j++ {
		if code := seeds.AddByte(j); code != 0 {
			return a, code
		}
	}
	address, bump, code := seeds.FindAddress(c, programID(c))
	if code != 0 {
		return a, code
	}
	var out [33]byte
	copy(out[:32], address[:])
	out[32] = bump
	return a, solana.SetReturnData(c, out[:])
}
