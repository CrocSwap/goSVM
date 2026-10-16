package reallibs

import (
	"github.com/KyberNetwork/int256"
	"github.com/KyberNetwork/uniswapv3-sdk-uint256/constants"
	"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"
	"github.com/holiman/uint256"
)

// tick is a signed tick from a 64-bit parameter, mostly within a few
// ticks of the valid range [MinTick, MaxTick] or of its ends.
func tick(n uint64) int {
	switch n % 8 {
	case 0:
		return int(int64(n)) // any 64-bit value
	case 1:
		return utils.MinTick - 4 + int(n>>3%9)
	case 2:
		return utils.MaxTick - 4 + int(n>>3%9)
	case 3:
		return int(n>>3%17) - 8
	}
	return int(n>>3%(2*utils.MaxTick+1)) + utils.MinTick
}

// TickOps is the number of operations of tickGroup.
const TickOps = 6

// tickGroup: the tick and sqrt-price conversions and MostSignificantBit.
func tickGroup(w *W, v *Input) {
	a := v.a
	switch v.op % TickOps {
	case 0:
		t := tick(v.n0)
		var r utils.Uint160
		r = v.e
		err := utils.GetSqrtRatioAtTickV2(t, &r)
		w.int(t)
		w.u256(&r)
		w.err(err)
	case 1:
		t, err := utils.GetTickAtSqrtRatioV2(&a)
		w.int(t)
		w.err(err)
	case 2:
		// A round trip from a valid tick, and the prices on each side.
		t := tick(v.n0)
		var r, below, above utils.Uint160
		err := utils.GetSqrtRatioAtTickV2(t, &r)
		w.err(err)
		if err == nil {
			back, err := utils.GetTickAtSqrtRatioV2(&r)
			w.int(back)
			w.err(err)
			below.SubUint64(&r, 1)
			t1, err := utils.GetTickAtSqrtRatioV2(&below)
			w.int(t1)
			w.err(err)
			above.AddUint64(&r, v.n1%3)
			t2, err := utils.GetTickAtSqrtRatioV2(&above)
			w.int(t2)
			w.err(err)
		}
	case 3:
		msb, err := utils.MostSignificantBit(&a)
		w.u64(uint64(msb))
		w.err(err)
	case 4:
		w.u256(utils.MinSqrtRatioU256)
		w.u256(utils.MaxSqrtRatioU256)
		w.u256(utils.Q32U256)
		w.u256(utils.MaxUint256)
		w.u256(utils.MaxUint160)
		w.u256(utils.Uint128Max)
		w.u256(utils.Uint160Max)
		w.u256(utils.MaxFeeUint256)
		w.u256(constants.Q96U256)
		w.int(constants.TickSpacings[constants.FeeAmount(v.n0%12000)])
		w.int(len(constants.TickSpacings))
		w.str(constants.PoolInitCodeHash)
	case 5:
		err := utils.CheckToUint160(&a)
		w.err(err)
		var s utils.Int256
		err = utils.ToInt256(&a, &s)
		w.i256(&s)
		w.err(err)
		var u utils.Uint256
		err = utils.ToUInt256((*int256.Int)(&v.b), &u)
		w.u256(&u)
		w.err(err)
	}
}

// MathOps is the number of operations of mathGroup.
const MathOps = 12

// mathGroup: full-precision mul/div and the sqrt-price math.
func mathGroup(w *W, v *Input) {
	a, b, c := v.a, v.b, v.c
	z := v.e
	roundUp := v.flags&1 != 0
	zeroForOne := v.flags&2 != 0
	switch v.op % MathOps {
	case 0:
		var r uint256.Int
		err := utils.MulDivV2(&a, &b, &c, &z, &r)
		w.u256(&z)
		w.u256(&r)
		w.err(err)
	case 1:
		err := utils.MulDivV2(&a, &b, &c, &z, nil)
		w.u256(&z)
		w.err(err)
	case 2:
		err := utils.MulDivRoundingUpV2(&a, &b, &c, &z)
		w.u256(&z)
		w.err(err)
	case 3:
		r, err := utils.MulDiv(&a, &b, &c)
		w.u256(r)
		w.err(err)
		r, err = utils.MulDivRoundingUp(&a, &b, &c)
		w.u256(r)
		w.err(err)
	case 4:
		utils.DivRoundingUp(&a, &c, &z)
		w.u256(&z)
	case 5:
		err := utils.GetAmount0DeltaV2(&a, &b, &c, roundUp, &z)
		w.u256(&z)
		w.err(err)
	case 6:
		err := utils.GetAmount1DeltaV2(&a, &b, &c, roundUp, &z)
		w.u256(&z)
		w.err(err)
	case 7:
		err := utils.GetNextSqrtPriceFromInput(&a, &c, &b, zeroForOne, &z)
		w.u256(&z)
		w.err(err)
	case 8:
		err := utils.GetNextSqrtPriceFromOutput(&a, &c, &b, zeroForOne, &z)
		w.u256(&z)
		w.err(err)
	case 9:
		y := int256.Int(b)
		err := utils.AddDeltaInPlace(&a, &y)
		w.u256(&a)
		w.err(err)
	case 10, 11:
		// ComputeSwapStep: current and target prices a and b, liquidity c,
		// amount remaining d (signed: exact input when non-negative) and a
		// fee in pips.
		fee := constants.FeeAmount(v.n0 % 1000001)
		if v.op%MathOps == 11 {
			fees := []constants.FeeAmount{constants.FeeLowest, constants.FeeLow, constants.FeeMedium, constants.FeeHigh, constants.Fee80, constants.Fee450, constants.Fee2500, 0}
			fee = fees[v.n0%8]
		}
		remaining := int256.Int(v.d)
		var next utils.Uint160
		var in, out, feeAmount utils.Uint256
		next, in, out, feeAmount = v.e, v.e, v.e, v.e
		err := utils.ComputeSwapStep(&a, &b, &c, &remaining, fee, &next, &in, &out, &feeAmount)
		w.u256(&next)
		w.u256(&in)
		w.u256(&out)
		w.u256(&feeAmount)
		w.err(err)
	}
}
