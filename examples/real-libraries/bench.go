package reallibs

import (
	"github.com/KyberNetwork/int256"
	"github.com/KyberNetwork/uniswapv3-sdk-uint256/constants"
	"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"
	"github.com/holiman/uint256"
	"lukechampine.com/uint128"
)

// Bench operations: one library call each on the decoded operands, writing
// one 256-bit result, so that the CU of an operation minus the CU of
// BenchNone (which decodes and writes the same way but calls nothing) is
// the call's own cost. Errors are not written (the result is).
const (
	BenchNone = iota
	BenchAdd
	BenchMul
	BenchDiv64    // a / b with a 64-bit divisor (udivremBy1)
	BenchDivKnuth // a / b with a wide divisor (Knuth's algorithm D)
	BenchMulDiv   // MulDivOverflow(a, b, c)
	BenchMulMod   // MulMod(a, b, c)
	BenchExp      // Exp(a, b)
	BenchSqrt     // Sqrt(a)
	BenchLog10    // Log10(a)
	BenchDec      // Dec(a), the decimal string's length
	BenchFromHex  // SetFromHex(text)
	BenchFromDec  // SetFromDecimal(text)
	BenchI256Mul  // int256 Mul(a, b)
	BenchI256Quo  // int256 Quo(a, b)
	BenchI256Dec  // int256 SetFromDec(text)
	BenchSqrtAtTick
	BenchTickAtSqrt
	BenchUniMulDiv  // utils.MulDivV2(a, b, c)
	BenchAmount0    // utils.GetAmount0DeltaV2(a, b, c, true)
	BenchNextPrice  // utils.GetNextSqrtPriceFromInput(a, c, b, zeroForOne)
	BenchSwapStep   // utils.ComputeSwapStep(a, b, c, d, fee n1)
	BenchU128Mul    // uint128 Mul of the low halves
	BenchU128QuoRem // uint128 QuoRem
	BenchOps
)

func benchGroup(w *W, v *Input) {
	a, b, c := v.a, v.b, v.c
	z := v.e
	switch v.op % BenchOps {
	case BenchNone:
		z = a
	case BenchAdd:
		z.Add(&a, &b)
	case BenchMul:
		z.Mul(&a, &b)
	case BenchDiv64, BenchDivKnuth:
		z.Div(&a, &b)
	case BenchMulDiv:
		z.MulDivOverflow(&a, &b, &c)
	case BenchMulMod:
		z.MulMod(&a, &b, &c)
	case BenchExp:
		z.Exp(&a, &b)
	case BenchSqrt:
		z.Sqrt(&a)
	case BenchLog10:
		z.SetUint64(uint64(a.Log10()))
	case BenchDec:
		z.SetUint64(uint64(len(a.Dec())))
	case BenchFromHex:
		z.SetFromHex(v.text)
	case BenchFromDec:
		z.SetFromDecimal(v.text)
	case BenchI256Mul:
		x, y := int256.Int(a), int256.Int(b)
		r := (*int256.Int)(&z)
		r.Mul(&x, &y)
	case BenchI256Quo:
		x, y := int256.Int(a), int256.Int(b)
		r := (*int256.Int)(&z)
		r.Quo(&x, &y)
	case BenchI256Dec:
		r := (*int256.Int)(&z)
		r.SetFromDec(v.text)
	case BenchSqrtAtTick:
		utils.GetSqrtRatioAtTickV2(int(int64(v.n0)), &z)
	case BenchTickAtSqrt:
		t, _ := utils.GetTickAtSqrtRatioV2(&a)
		z.SetUint64(uint64(t))
	case BenchUniMulDiv:
		utils.MulDivV2(&a, &b, &c, &z, nil)
	case BenchAmount0:
		utils.GetAmount0DeltaV2(&a, &b, &c, true, &z)
	case BenchNextPrice:
		utils.GetNextSqrtPriceFromInput(&a, &c, &b, v.flags&2 != 0, &z)
	case BenchSwapStep:
		remaining := int256.Int(v.d)
		var in, out, fee uint256.Int
		utils.ComputeSwapStep(&a, &b, &c, &remaining, constants.FeeAmount(v.n1), &z, &in, &out, &fee)
	case BenchU128Mul:
		x := uint128.New(a[0], a[1]).MulWrap(uint128.New(b[0], b[1]))
		z[0], z[1] = x.Lo, x.Hi
	case BenchU128QuoRem:
		q, _ := uint128.New(a[0], a[1]).QuoRem(uint128.New(b[0], b[1]))
		z[0], z[1] = q.Lo, q.Hi
	}
	w.u256(&z)
}
