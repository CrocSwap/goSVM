package program

import (
	"encoding/binary"

	"example.com/counter/model"
	"gosvm/solana"
)

const ErrZeroAmount = model.ErrZeroAmount
const ErrLimit = model.ErrLimit

// Create initializes the caller's counter. Generated code has already checked
// the accounts and the PDA, and created the rent-exempt account.
func Create(c solana.Context, a CreateAccounts, args model.CreateArgs) (CreateAccounts, uint64) {
	if args.Limit == 0 {
		return a, ErrLimit
	}
	a.Counter = model.Counter{Owner: model.PublicKey(a.Owner.Key), Limit: args.Limit, Bump: args.Bump}
	return a, 0
}

// Increment adds to the counter. Only its owner can sign for it.
func Increment(c solana.Context, a IncrementAccounts, args model.IncrementArgs) (IncrementAccounts, uint64) {
	next, code := model.Next(a.Counter.Count, a.Counter.Limit, args.Amount)
	if code != 0 {
		return a, code
	}
	a.Counter.Count = next
	return a, 0
}

// Preview applies the same rule to its arguments, without any state, and
// returns the new count as eight little-endian bytes of return data.
func Preview(c solana.Context, a PreviewAccounts, args model.PreviewArgs) (PreviewAccounts, uint64) {
	next, code := model.Next(args.Count, args.Limit, args.Amount)
	if code != 0 {
		return a, code
	}
	var out [8]byte
	binary.LittleEndian.PutUint64(out[:], next)
	return a, solana.SetReturnData(c, out[:])
}
