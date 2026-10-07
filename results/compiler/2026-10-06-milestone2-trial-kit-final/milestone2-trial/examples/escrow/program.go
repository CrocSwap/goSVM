package program

import (
	"example.org/gosvm/escrow/model"
	"gosvm/sdk/system"
	"gosvm/solana"
)

const ErrAmount uint64 = 4100
const ErrBeneficiary uint64 = 4101
const ErrSecret uint64 = 4102
const ErrNonce uint64 = 4103

func Open(c solana.Context, a OpenAccounts, args model.OpenArgs) (OpenAccounts, uint64) {
	if args.Amount == 0 {
		return a, ErrAmount
	}
	if args.Beneficiary != model.PublicKey(a.Beneficiary.Key) {
		return a, ErrBeneficiary
	}
	a.Escrow = model.Escrow{Creator: model.PublicKey(a.Creator.Key), Beneficiary: args.Beneficiary, Commitment: args.Commitment, Amount: args.Amount, Nonce: args.Nonce, Bump: args.Bump}
	code := system.Transfer(c, a.System.Index, a.Creator.Index, a.EscrowRef.Index, args.Amount)
	return a, code
}

func Claim(c solana.Context, a ClaimAccounts, args model.ClaimArgs) (ClaimAccounts, uint64) {
	if args.Nonce != a.Escrow.Nonce {
		return a, ErrNonce
	}
	var hash [32]byte
	code := solana.SHA256(c, args.Preimage[:], 0, 32, hash[:], 0)
	if code != 0 {
		return a, code
	}
	if hash != a.Escrow.Commitment {
		return a, ErrSecret
	}
	return a, 0
}

func Cancel(c solana.Context, a CancelAccounts, args model.CancelArgs) (CancelAccounts, uint64) {
	return a, 0
}
