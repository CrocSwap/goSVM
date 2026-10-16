package reallibs

import (
	"lukechampine.com/uint128"
)

// U128Ops is the number of operations of u128Group.
const U128Ops = 20

// u128Group: lukechampine.com/uint128, whose checked operations panic on
// overflow and underflow and whose divisions trap on zero.
func u128Group(w *W, v *Input) {
	x := uint128.New(v.a[0], v.a[1])
	y := uint128.New(v.b[0], v.b[1])
	n := v.n0
	put := func(u uint128.Uint128) {
		w.u64(u.Lo)
		w.u64(u.Hi)
	}
	switch v.op % U128Ops {
	case 0:
		put(x.Add(y))
	case 1:
		put(x.AddWrap(y))
		put(x.AddWrap64(n))
		put(x.Add64(n))
	case 2:
		put(x.Sub(y))
	case 3:
		put(x.SubWrap(y))
		put(x.SubWrap64(n))
		put(x.Sub64(n))
	case 4:
		put(x.Mul(y))
	case 5:
		put(x.MulWrap(y))
		put(x.MulWrap64(n))
		put(x.Mul64(n))
	case 6:
		put(x.Div(y))
	case 7:
		q, r := x.QuoRem(y)
		put(q)
		put(r)
		put(x.Mod(y))
	case 8:
		q, r := x.QuoRem64(n)
		put(q)
		w.u64(r)
		put(x.Div64(n))
		w.u64(x.Mod64(n))
	case 9:
		put(x.Lsh(uint(n % 200)))
		put(x.Rsh(uint(n % 200)))
	case 10:
		w.int(x.LeadingZeros())
		w.int(x.TrailingZeros())
		w.int(x.OnesCount())
		w.int(x.Len())
	case 11:
		put(x.RotateLeft(int(int64(n))))
		put(x.RotateRight(int(n % 300)))
		put(x.Reverse())
		put(x.ReverseBytes())
	case 12:
		w.str(x.String())
	case 13:
		var b, be [16]byte
		x.PutBytes(b[:])
		x.PutBytesBE(be[:])
		w.bs(b[:])
		w.bs(be[:])
		put(uint128.FromBytes(v.raw[64:]))
		put(uint128.FromBytesBE(v.raw[64:]))
	case 14:
		w.int(x.Cmp(y))
		w.int(x.Cmp64(n))
		w.flag(x.Equals(y))
		w.flag(x.Equals64(n))
		w.flag(x.IsZero())
	case 15:
		put(x.And(y))
		put(x.Or(y))
		put(x.Xor(y))
		put(x.And64(n))
		put(x.Or64(n))
		put(x.Xor64(n))
	case 16:
		put(uint128.Zero)
		put(uint128.Max)
		put(uint128.From64(n))
		w.flag(uint128.Max.AddWrap64(1) == uint128.Zero)
	case 17:
		// A short buffer panics with a bounds trap.
		buf := make([]byte, n%20)
		x.PutBytes(buf)
		w.bs(buf)
	case 18:
		put(x.Add(y).Mul64(n).Sub(y))
	case 19:
		q, r := x.QuoRem(uint128.From64(n))
		put(q)
		put(r)
	}
}
