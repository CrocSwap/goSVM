package program

import (
	"example.org/gosvm/full-swap/model"
	"gosvm/sdk/pda"
	"gosvm/sdk/token"
	"gosvm/solana"
)

const ErrLiquidity uint64 = 115

func CreatePool(c solana.Context, a CreatePoolAccounts, args model.CreatePoolArgs) (CreatePoolAccounts, uint64) {
	if a.MintX.Key == a.MintY.Key {
		return a, ErrMint
	}
	x, code := token.Balance(c, a.CreatorX)
	if code != 0 {
		return a, code
	}
	y, code := token.Balance(c, a.CreatorY)
	if code != 0 {
		return a, code
	}
	if args.ReserveX == 0 || args.ReserveY == 0 || x < args.ReserveX || y < args.ReserveY {
		return a, ErrLiquidity
	}
	var seedsX, seedsY pda.Seeds
	xLabel := [6]byte{'v', 'a', 'u', 'l', 't', 'x'}
	yLabel := [6]byte{'v', 'a', 'u', 'l', 't', 'y'}
	if seedsX.AddBytes(xLabel[:]) != 0 || seedsX.AddKey(a.PoolRef.Key) != 0 || seedsX.AddByte(args.XBump) != 0 || seedsY.AddBytes(yLabel[:]) != 0 || seedsY.AddKey(a.PoolRef.Key) != 0 || seedsY.AddByte(args.YBump) != 0 {
		return a, pda.ErrSeeds
	}
	code = token.CreateSigned(c, a.System.Index, a.TokenProgram.Index, a.Creator.Index, a.VaultX.Index, a.MintX.Index, a.Authority.Key, &seedsX)
	if code != 0 {
		return a, code
	}
	code = token.CreateSigned(c, a.System.Index, a.TokenProgram.Index, a.Creator.Index, a.VaultY.Index, a.MintY.Index, a.Authority.Key, &seedsY)
	if code != 0 {
		return a, code
	}
	vx, code := token.Load(c, a.VaultX.Index, true)
	if code != 0 {
		return a, code
	}
	vy, code := token.Load(c, a.VaultY.Index, true)
	if code != 0 {
		return a, code
	}
	code = token.Transfer(c, a.TokenProgram.Index, a.CreatorX, vx, a.Creator.Index, args.ReserveX)
	if code != 0 {
		return a, code
	}
	code = token.Transfer(c, a.TokenProgram.Index, a.CreatorY, vy, a.Creator.Index, args.ReserveY)
	if code != 0 {
		return a, code
	}
	a.Pool = model.ManagedPool{Core: model.Pool{VaultX: model.PublicKey(a.VaultX.Key), VaultY: model.PublicKey(a.VaultY.Key), MintX: model.PublicKey(a.MintX.Key), MintY: model.PublicKey(a.MintY.Key), Bump: args.Bump}, Creator: model.PublicKey(a.Creator.Key)}
	return a, 0
}

func ManagedSwap(c solana.Context, a ManagedSwapAccounts, args model.SwapArgs) (ManagedSwapAccounts, uint64) {
	old := SwapAccounts{Pool: a.Pool.Core, PoolRef: a.PoolRef, User: a.User, UserX: a.UserX, VaultX: a.VaultX, VaultY: a.VaultY, UserY: a.UserY, Authority: a.Authority, TokenProgram: a.TokenProgram}
	updated, code := Swap(c, old, args)
	if code == 0 {
		a.Pool.Core = updated.Pool
	}
	return a, code
}

func ClosePool(c solana.Context, a ClosePoolAccounts, args model.ClosePoolArgs) (ClosePoolAccounts, uint64) {
	x, code := token.Balance(c, a.VaultX)
	if code != 0 {
		return a, code
	}
	y, code := token.Balance(c, a.VaultY)
	if code != 0 {
		return a, code
	}
	var seeds pda.Seeds
	if seeds.AddKey(a.PoolRef.Key) != 0 || seeds.AddByte(a.Pool.Core.Bump) != 0 {
		return a, pda.ErrSeeds
	}
	if x != 0 {
		code = token.TransferSigned(c, a.TokenProgram.Index, a.VaultX, a.CreatorX, a.Authority.Index, x, &seeds)
		if code != 0 {
			return a, code
		}
	}
	if y != 0 {
		code = token.TransferSigned(c, a.TokenProgram.Index, a.VaultY, a.CreatorY, a.Authority.Index, y, &seeds)
		if code != 0 {
			return a, code
		}
	}
	code = token.CloseSigned(c, a.TokenProgram.Index, a.VaultX, a.Creator.Index, a.Authority.Index, &seeds)
	if code != 0 {
		return a, code
	}
	code = token.CloseSigned(c, a.TokenProgram.Index, a.VaultY, a.Creator.Index, a.Authority.Index, &seeds)
	return a, code
}
