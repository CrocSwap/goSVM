package reallibs

import (
	"io"

	"github.com/KyberNetwork/int256"
	"github.com/KyberNetwork/uniswapv3-sdk-uint256/utils"
	"github.com/holiman/uint256"
)

// sentinels are the libraries' exported error values, in a fixed order.
func sentinels() []error {
	return []error{
		uint256.ErrEmptyString, uint256.ErrSyntax, uint256.ErrMissingPrefix, uint256.ErrEmptyNumber,
		uint256.ErrLeadingZero, uint256.ErrBig256Range, uint256.ErrBadBufferLength, uint256.ErrBadEncodedLength,
		int256.ErrOverflow, int256.ErrZeroDivision, int256.ErrNegativeNum,
		utils.ErrMulDivOverflow, utils.ErrExceedMaxInt256, utils.ErrOverflowUint128, utils.ErrOverflowUint160,
		utils.ErrInvalidInput, utils.ErrSqrtPriceLessThanZero, utils.ErrLiquidityLessThanZero, utils.ErrInvariant,
		utils.ErrAddOverflow, utils.ErrInvalidTick, utils.ErrInvalidSqrtRatio, io.EOF,
	}
}

// U256ArithOps is the number of operations of u256Arith.
const U256ArithOps = 32

// u256Arith: binary and ternary uint256 arithmetic, each result written
// with the operands afterwards (several operations alias z with an operand).
func u256Arith(w *W, v *Input) {
	a, b, c := v.a, v.b, v.c
	var z uint256.Int
	z = v.e // results start from a non-zero receiver
	switch v.op % U256ArithOps {
	case 0:
		w.u256(z.Add(&a, &b))
	case 1:
		r, over := z.AddOverflow(&a, &b)
		w.u256(r)
		w.flag(over)
	case 2:
		w.u256(z.Sub(&a, &b))
	case 3:
		r, over := z.SubOverflow(&a, &b)
		w.u256(r)
		w.flag(over)
	case 4:
		w.u256(z.Mul(&a, &b))
	case 5:
		r, over := z.MulOverflow(&a, &b)
		w.u256(r)
		w.flag(over)
	case 6:
		w.u256(z.Div(&a, &b))
	case 7:
		w.u256(z.Mod(&a, &b))
	case 8:
		q, m := z.DivMod(&a, &b, &c)
		w.u256(q)
		w.u256(m)
	case 9:
		w.u256(z.SDiv(&a, &b))
	case 10:
		w.u256(z.SMod(&a, &b))
	case 11:
		w.u256(z.Exp(&a, &b))
	case 12:
		w.u256(z.MulMod(&a, &b, &c))
	case 13:
		w.u256(z.AddMod(&a, &b, &c))
	case 14:
		r, over := z.MulDivOverflow(&a, &b, &c)
		w.u256(r)
		w.flag(over)
	case 15:
		mu := uint256.Reciprocal(&c)
		for _, m := range mu {
			w.u64(m)
		}
		w.u256(z.MulModWithReciprocal(&a, &b, &c, &mu))
	case 16:
		var x, y, q uint256.Int
		w.u256(x.And(&a, &b))
		w.u256(y.Or(&a, &b))
		w.u256(q.Xor(&a, &b))
	case 17:
		w.int(a.Cmp(&b))
		w.flag(a.Lt(&b))
		w.flag(a.Gt(&b))
		w.flag(a.Slt(&b))
		w.flag(a.Sgt(&b))
		w.flag(a.Eq(&b))
	case 18:
		w.u256(z.Byte(&b))
		w.u256(a.Byte(&a))
	case 19:
		w.u256(z.ExtendSign(&a, &b))
	case 20:
		w.u256(z.AddUint64(&a, v.n0))
		w.u256(a.SubUint64(&a, v.n1))
	case 21:
		w.int(a.CmpUint64(v.n0))
		w.flag(a.LtUint64(v.n0))
		w.flag(a.GtUint64(v.n0))
	// Aliased operands: z is also an operand.
	case 22:
		w.u256(a.Add(&a, &a))
		w.u256(b.Sub(&b, &a))
	case 23:
		w.u256(a.Mul(&a, &a))
		w.u256(b.Mul(&a, &b))
	case 24:
		w.u256(a.Div(&a, &b))
		w.u256(b.Div(&c, &b))
	case 25:
		w.u256(a.Mod(&a, &b))
		w.u256(c.Mod(&c, &c))
	case 26:
		w.u256(a.MulMod(&a, &a, &c))
		w.u256(b.MulMod(&b, &c, &b))
	case 27:
		w.u256(a.AddMod(&a, &b, &a))
	case 28:
		w.u256(a.Exp(&a, &a))
		w.u256(b.Exp(&c, &b))
	case 29:
		w.u256(a.SDiv(&a, &b))
		w.u256(c.SMod(&c, &c))
	case 30:
		q, m := a.DivMod(&a, &b, &b)
		w.u256(q)
		w.u256(m)
	case 31:
		r, over := a.MulDivOverflow(&a, &b, &a)
		w.u256(r)
		w.flag(over)
	}
	w.u256(&a)
	w.u256(&b)
	w.u256(&c)
}

// U256UnaryOps is the number of operations of u256Unary.
const U256UnaryOps = 18

// u256Unary: one-operand uint256 operations, shifts and queries.
func u256Unary(w *W, v *Input) {
	a := v.a
	z := v.e
	shift := uint(v.n0 % 300)
	switch v.op % U256UnaryOps {
	case 0:
		w.u256(z.Lsh(&a, shift))
	case 1:
		w.u256(z.Rsh(&a, shift))
	case 2:
		w.u256(z.SRsh(&a, shift))
	case 3:
		w.u256(a.Lsh(&a, shift))
		w.u256(a.Rsh(&a, uint(v.n1%300)))
		w.u256(a.SRsh(&a, uint(v.n2%300)))
	case 4:
		w.u256(z.Sqrt(&a))
	case 5:
		w.u64(uint64(a.Log10()))
	case 6:
		w.u256(z.Neg(&a))
		w.u256(z.Abs(&a))
		w.u256(z.Not(&a))
	case 7:
		w.int(a.Sign())
		w.int(a.BitLen())
		w.int(a.ByteLen())
		w.flag(a.IsZero())
		w.flag(a.IsUint64())
		u, over := a.Uint64WithOverflow()
		w.u64(u)
		w.flag(over)
		w.u64(a.Uint64())
	case 8:
		w.u256(a.Clone())
		w.u256(z.Set(&a))
		w.u256(z.SetUint64(v.n0))
		w.u256(z.SetOne())
		w.u256(z.SetAllOne())
		w.u256(z.Clear())
		w.u256(uint256.NewInt(v.n1))
	case 9:
		w.u256(a.Sqrt(&a))
		w.u256(a.Neg(&a))
		w.u256(a.Not(&a))
	case 10:
		r := a.Bytes32()
		w.bs(r[:])
		s := a.Bytes20()
		w.bs(s[:])
		w.bs(a.Bytes())
	case 11:
		w.bs(a.PaddedBytes(int(v.n0 % 70)))
	case 12:
		buf := make([]byte, v.n0%40)
		a.WriteToSlice(buf)
		w.bs(buf)
		var arr32 [32]byte
		a.WriteToArray32(&arr32)
		w.bs(arr32[:])
		var arr20 [20]byte
		a.WriteToArray20(&arr20)
		w.bs(arr20[:])
	case 13:
		// SetBytes with every length 0..40 (SetBytes1..SetBytes32 and the
		// generic path); the bytes are the vector's own.
		n := int(v.n0 % 41)
		w.u256(z.SetBytes(v.raw[32 : 32+n]))
	case 14:
		n := int(v.n0%32) + 1
		src := v.raw[64 : 64+n]
		switch n {
		case 1:
			z.SetBytes1(src)
		case 2:
			z.SetBytes2(src)
		case 3:
			z.SetBytes3(src)
		case 8:
			z.SetBytes8(src)
		case 9:
			z.SetBytes9(src)
		case 16:
			z.SetBytes16(src)
		case 17:
			z.SetBytes17(src)
		case 20:
			z.SetBytes20(src)
		case 24:
			z.SetBytes24(src)
		case 31:
			z.SetBytes31(src)
		case 32:
			z.SetBytes32(src)
		default:
			z.SetBytes(src)
		}
		w.u256(&z)
	case 15:
		b, err := a.MarshalSSZ()
		w.bs(b)
		w.err(err)
		b, err = a.MarshalSSZAppend(v.raw[0:3:3])
		w.bs(b)
		w.err(err)
		w.int(a.SizeSSZ())
		h, err := a.HashTreeRoot()
		w.bs(h[:])
		w.err(err)
	case 16:
		w.flag(a.Lt(&v.b))
		w.u256(z.Lsh(&a, uint(v.n0%4)*64))
		w.u256(z.Rsh(&a, uint(v.n0%4)*64))
		w.u256(z.SRsh(&a, uint(v.n0%4)*64))
	case 17:
		// Chained pointer results.
		w.u256(z.Add(&a, &v.b).Mul(&z, &v.c).Div(&z, &v.d))
	}
	w.u256(&a)
}

// U256TextOps is the number of operations of u256Text.
const U256TextOps = 14

// u256Text: decimal and hexadecimal formatting and parsing, JSON, text
// and SQL-scan forms.
func u256Text(w *W, v *Input) {
	a := v.a
	z := v.e
	s := v.text
	switch v.op % U256TextOps {
	case 0:
		w.str(a.Dec())
		w.str(a.String())
	case 1:
		w.str(a.Hex())
	case 2:
		w.str(a.PrettyDec(byte(v.n0)))
	case 3:
		r, err := uint256.FromHex(s)
		w.u256(r)
		w.err(err)
	case 4:
		err := z.SetFromHex(s)
		w.u256(&z)
		w.err(err)
	case 5:
		r, err := uint256.FromDecimal(s)
		w.u256(r)
		w.err(err)
	case 6:
		err := z.SetFromDecimal(s)
		w.u256(&z)
		w.err(err)
	case 7:
		err := z.UnmarshalText([]byte(s))
		w.u256(&z)
		w.err(err)
	case 8:
		err := z.UnmarshalJSON([]byte(s))
		w.u256(&z)
		w.err(err)
	case 9:
		b, err := a.MarshalText()
		w.text(b)
		w.err(err)
		b, err = a.MarshalJSON()
		w.text(b)
		w.err(err)
	case 10:
		var src interface{}
		switch v.flags % 4 {
		case 0:
			src = s
		case 1:
			src = []byte(s)
		case 2:
			src = nil
		case 3:
			src = v.n0
		}
		err := z.Scan(src)
		w.u256(&z)
		w.err(err)
	case 11:
		// Round trips.
		var x, y uint256.Int
		ex := x.SetFromHex(a.Hex())
		ey := y.SetFromDecimal(a.Dec())
		w.u256(&x)
		w.err(ex)
		w.u256(&y)
		w.err(ey)
	case 12:
		w.u256(uint256.MustFromHex(s))
	case 13:
		w.u256(uint256.MustFromDecimal(s))
	}
}
