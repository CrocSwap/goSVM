package program

import (
	"gosvm/solana"
	"example.org/gosvm/multi-state/model"
)

const ErrMove uint64 = 1
const ErrCounter uint64 = 2

// Move changes two program-owned ledgers. It does not transfer SPL tokens.
// Generated validation checks the authority and both layouts before this runs.
func Move(c solana.Context, accounts MoveAccounts, args model.MoveArgs) (MoveAccounts, uint64) {
	if !model.CanMove(accounts.Source.Balance, accounts.Destination.Balance, args.Amount, accounts.Policy.Limit) {
		return accounts, ErrMove
	}
	if accounts.Source.Moves == 18446744073709551615 || accounts.Destination.Moves == 18446744073709551615 {
		return accounts, ErrCounter
	}
	accounts.Source.Balance = accounts.Source.Balance - args.Amount
	accounts.Destination.Balance = accounts.Destination.Balance + args.Amount
	accounts.Source.Moves++
	accounts.Destination.Moves++
	// Returned readonly values are not committed by the adapter.
	accounts.Policy.Limit = 0
	return accounts, 0
}

func SetLimit(c solana.Context, accounts SetLimitAccounts, args model.SetLimitArgs) (SetLimitAccounts, uint64) {
	accounts.Policy.Limit = args.Limit
	return accounts, 0
}
