package program

import (
	"example.org/gosvm/fee-vault/model"
	"gosvm/sdk/system"
	"gosvm/solana"
)

// Create records the vault's authority and beneficiary. Generated code has
// already checked the PDA and created the rent-exempt account.
func Create(c solana.Context, a CreateAccounts, args model.CreateArgs) (CreateAccounts, uint64) {
	a.Vault = model.Vault{Authority: model.PublicKey(a.Authority.Key), Beneficiary: args.Beneficiary, Bump: args.Bump}
	return a, 0
}

// Claim moves every lamport above the vault's rent-exempt minimum to the
// recorded beneficiary, as Hyperlane's claim_protocol_fees does:
// **vault.lamports -= claimable; **beneficiary.lamports += claimable.
func Claim(c solana.Context, a ClaimAccounts, args model.ClaimArgs) (ClaimAccounts, uint64) {
	minimum, code := system.MinimumBalance(c, uint64(len(solana.Data(c, a.VaultRef.Index))))
	if code != 0 {
		return a, code
	}
	claimable := model.Claimable(solana.Lamports(c, a.VaultRef.Index), minimum)
	return a, solana.MoveLamports(c, a.VaultRef.Index, a.Beneficiary.Index, claimable)
}

// Move passes its arguments straight to MoveLamports so the fixtures can
// exercise the SDK's rules. It leaves rent exemption to the runtime.
func Move(c solana.Context, a MoveAccounts, args model.MoveArgs) (MoveAccounts, uint64) {
	return a, solana.MoveLamports(c, uint64(args.From), uint64(args.To), args.Amount)
}
