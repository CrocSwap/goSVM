// Package program implements a generated, checked classic-token swap.
// Accounts are preloaded for the initial migration proof; lifecycle is separate.
package program

import (
	"example.org/gosvm/full-swap/model"
	"gosvm/sdk/pda"
	"gosvm/sdk/token"
	"gosvm/solana"
)

const ErrMint uint64 = 109
const ErrInput uint64 = 112
const ErrOutput uint64 = 113
const ErrCounter uint64 = 114

func Swap(c solana.Context, a SwapAccounts, args model.SwapArgs) (SwapAccounts, uint64) {
	if a.Pool.MintX == a.Pool.MintY {
		return a, ErrMint
	}
	ux, code := token.Balance(c, a.UserX)
	if code != 0 {
		return a, code
	}
	vx, code := token.Balance(c, a.VaultX)
	if code != 0 {
		return a, code
	}
	vy, code := token.Balance(c, a.VaultY)
	if code != 0 {
		return a, code
	}
	uy, code := token.Balance(c, a.UserY)
	if code != 0 {
		return a, code
	}
	amount := args.AmountIn
	if amount == 0 || vx == 0 || vy == 0 || amount > 18446744073709551615-vx || ux < amount {
		return a, ErrInput
	}
	out := model.Quote(vx, vy, amount)
	if out == 0 || out < args.MinOut || out > 18446744073709551615-uy {
		return a, ErrOutput
	}
	if a.Pool.Swaps == 18446744073709551615 {
		return a, ErrCounter
	}
	code = token.Transfer(c, a.TokenProgram.Index, a.UserX, a.VaultX, a.User.Index, amount)
	if code != 0 {
		return a, code
	}
	var seeds pda.Seeds
	if seeds.AddKey(a.PoolRef.Key) != 0 || seeds.AddByte(a.Pool.Bump) != 0 {
		return a, pda.ErrSeeds
	}
	code = token.TransferSigned(c, a.TokenProgram.Index, a.VaultY, a.UserY, a.Authority.Index, out, &seeds)
	if code != 0 {
		return a, code
	}
	a.Pool.Swaps++
	return a, 0
}
