// Package model contains the same full-width quote as the preserved manual baseline.
package model

// Quote uses a 30bps fee rounded up in input units, retained in the input vault.
// Floor(y * net / (x + net)) uses a full 128-bit product without a native shim.
func Quote(x, y, amount uint64) uint64 {
	if x == 0 || y == 0 || amount == 0 || amount > 18446744073709551615-x {
		return 0
	}
	fee := (amount/10000)*30 + ((amount%10000)*30+9999)/10000
	net := amount - fee
	if net == 0 {
		return 0
	}
	return mulDiv(y, net, x+net)
}

// mulDiv is adapted from Go's math/bits Mul64 and Div64 (Go Authors, BSD-3-Clause;
// see THIRD_PARTY_NOTICES and third_party/Go-LICENSE). Its invalid-input sentinel
// is zero. Quote proves d>0 and quotient<=a, so it never reaches that case.
func mulDiv(a, b, d uint64) uint64 {
	const mask = 4294967295
	a0 := a & mask
	a1 := a >> 32
	b0 := b & mask
	b1 := b >> 32
	w0 := a0 * b0
	t := a1*b0 + (w0 >> 32)
	w1 := (t & mask) + a0*b1
	hi := a1*b1 + (t >> 32) + (w1 >> 32)
	lo := a * b
	if d == 0 || hi >= d {
		return 0
	}
	if hi == 0 {
		return lo / d
	}
	s := uint64(0)
	for d&9223372036854775808 == 0 {
		d = d << 1
		s++
	}
	const base = 4294967296
	d1 := d >> 32
	d0 := d & mask
	u32 := hi<<s | lo>>(64-s)
	u10 := lo << s
	u1 := u10 >> 32
	u0 := u10 & mask
	q1 := u32 / d1
	r := u32 - q1*d1
	for q1 >= base || q1*d0 > base*r+u1 {
		q1--
		r = r + d1
		if r >= base {
			break
		}
	}
	u21 := u32*base + u1 - q1*d
	q0 := u21 / d1
	r = u21 - q0*d1
	for q0 >= base || q0*d0 > base*r+u0 {
		q0--
		r = r + d1
		if r >= base {
			break
		}
	}
	return q1*base + q0
}
