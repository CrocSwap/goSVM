package program

// Pool is serialized field-by-field in declaration order, never by Go memory layout.
type Pool struct {
	ReserveX  uint64
	ReserveY  uint64
	SwapCount uint64
}

type SwapArgs struct {
	AmountIn     uint64
	MinAmountOut uint64
}

type SwapResult struct {
	State Pool
	Code  uint64
}

const (
	ErrInvalidReserves uint64 = 2
	ErrInvalidAmount   uint64 = 3
	ErrCounterOverflow uint64 = 4
	ErrSlippage        uint64 = 5
)

// Swap demonstrates bounded AMM arithmetic, not token transfers or a deployable AMM.
// The caps keep every intermediate multiplication within uint64.
func Swap(pool Pool, args SwapArgs) SwapResult {
	result := SwapResult{State: pool}
	if pool.ReserveX == 0 || pool.ReserveY == 0 || pool.ReserveX > 1000000000 || pool.ReserveY > 1000000000 {
		result.Code = ErrInvalidReserves
		return result
	}
	if args.AmountIn == 0 || args.AmountIn > 1000000 || pool.ReserveX+args.AmountIn > 1000000000 {
		result.Code = ErrInvalidAmount
		return result
	}
	if pool.SwapCount == 18446744073709551615 {
		result.Code = ErrCounterOverflow
		return result
	}
	effective := args.AmountIn * 9970
	out := pool.ReserveY * effective / (pool.ReserveX*10000 + effective)
	if out == 0 || out < args.MinAmountOut {
		result.Code = ErrSlippage
		return result
	}
	result.State.ReserveX = pool.ReserveX + args.AmountIn
	result.State.ReserveY = pool.ReserveY - out
	result.State.SwapCount = pool.SwapCount + 1
	return result
}
