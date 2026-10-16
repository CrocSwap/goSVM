package reallibs

import (
	"github.com/KyberNetwork/int256"
)

func signed(z *Input, which int) int256.Int {
	switch which {
	case 0:
		return int256.Int(z.a)
	case 1:
		return int256.Int(z.b)
	}
	return int256.Int(z.c)
}

// I256Ops is the number of operations of i256Group.
const I256Ops = 26

// i256Group: KyberNetwork/int256 arithmetic, comparisons, conversions and
// text, including the panicking division by zero.
func i256Group(w *W, v *Input) {
	a, b := signed(v, 0), signed(v, 1)
	z := int256.Int(v.e)
	shift := uint(v.n0 % 300)
	switch v.op % I256Ops {
	case 0:
		w.i256(z.Add(&a, &b))
	case 1:
		r, over := z.AddOverflow(&a, &b)
		w.i256(r)
		w.flag(over)
	case 2:
		w.i256(z.Sub(&a, &b))
	case 3:
		r, over := z.SubOverflow(&a, &b)
		w.i256(r)
		w.flag(over)
	case 4:
		w.i256(z.Mul(&a, &b))
	case 5:
		r, over := z.MulOverflow(&a, &b)
		w.i256(r)
		w.flag(over)
	case 6:
		w.i256(z.Quo(&a, &b)) // panics (ErrZeroDivision) when b == 0
	case 7:
		w.i256(z.Rem(&a, &b))
	case 8:
		w.i256(z.Pow(&a, v.n1%300))
	case 9:
		w.flag(a.Lt(&b))
		w.flag(a.Lte(&b))
		w.flag(a.Gt(&b))
		w.flag(a.Gte(&b))
		w.int(a.Cmp(&b))
		w.flag(a.Eq(&b))
	case 10:
		w.i256(z.Neg(&a))
		w.i256(a.Neg(&a))
	case 11:
		var x, y, q, n int256.Int
		w.i256(x.Or(&a, &b))
		w.i256(y.And(&a, &b))
		w.i256(q.Xor(&a, &b))
		w.i256(n.Not(&a))
	case 12:
		w.i256(z.Lsh(&a, shift))
	case 13:
		w.i256(z.Rsh(&a, shift))
	case 14:
		w.int(a.Sign())
		w.flag(a.IsZero())
		w.flag(a.IsOne())
		w.flag(a.IsNegative())
		w.flag(a.IsPositive())
		w.flag(a.IsMinI256())
		w.flag(a.IsInt64())
		w.u64(uint64(a.Int64()))
		w.flag(a.IsUint64())
		w.u64(a.Uint64())
		w.int(a.BitLen())
	case 15:
		w.str(a.Dec())
	case 16:
		r, err := int256.FromDec(v.text)
		w.i256(r)
		w.err(err)
	case 17:
		err := z.SetFromDec(v.text)
		w.i256(&z)
		w.err(err)
	case 18:
		b1, err := a.MarshalText()
		w.text(b1)
		w.err(err)
		b2, err := a.MarshalJSON()
		w.text(b2)
		w.err(err)
	case 19:
		err := z.UnmarshalText([]byte(v.text))
		w.i256(&z)
		w.err(err)
		err = z.UnmarshalJSON([]byte(v.text))
		w.i256(&z)
		w.err(err)
	case 20:
		w.i256(int256.NewInt(int64(v.n0)))
		w.i256(z.SetInt64(int64(v.n1)))
		w.i256(z.SetUint64(v.n2))
		w.i256(a.Clone())
		w.i256(z.Set(&b))
		w.i256(z.Clear())
		w.i256(z.SetOne())
		w.i256(z.SetAllBitsOne())
	case 21:
		var buf [32]byte
		a.WriteToArray32(&buf)
		w.bs(buf[:])
		w.i256(z.SetBytes32(v.raw[64:96]))
	case 22:
		// Aliased operands.
		w.i256(a.Add(&a, &a))
		w.i256(b.Mul(&b, &a))
		w.i256(a.Sub(&b, &a))
	case 23:
		w.i256(a.Quo(&a, &b))
		w.i256(b.Rem(&b, &b))
	case 24:
		w.i256(a.Lsh(&a, shift))
		w.i256(a.Rsh(&a, uint(v.n1%300)))
		w.i256(a.Pow(&a, v.n2%8))
	case 25:
		w.i256(int256.MustFromDec(v.text))
	}
	w.i256(&a)
	w.i256(&b)
}
