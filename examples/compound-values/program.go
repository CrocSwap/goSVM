// Package compoundvalues is an ordinary-Go oracle for compound
// assignment (x op= y) in the on-chain subset: every operator (+=, -=, *=,
// /=, %=, &=, |=, ^=, &^=, <<=, >>=) on every integer type (uint8 to
// uint64, uint, int8 to int64, int and named integer types), on every
// addressable target (locals, fields, fields through pointers, array, slice
// and nested elements, pointer indirections, map elements present and
// absent, captured variables), with wrapping, truncating signed division,
// MinInt / -1, division and modulo by zero, shifts at and beyond the width
// and negative shift counts; Go's evaluation order (index and pointer
// operands once, before the right operand; the target read after it), and
// loops whose post statement is a compound assignment. The same source runs
// natively and through goSVM; tests compare output bytes and status (or a
// panic) for every vector. See docs/GO_INTEGERS.md#compound-assignment.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package compoundvalues

// Int is every integer type, including named ones.
type Int interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uint | ~int8 | ~int16 | ~int32 | ~int64 | ~int
}

// Amount and Delta are named integer types.
type (
	Amount uint64
	Delta  int16
)

// Box and Holder give fields, arrays in structs and pointer chains.
type Box[T Int] struct {
	V   T
	Arr [3]T
}

type Holder[T Int] struct {
	B *Box[T]
}

// W writes little-endian values into a byte slice held in a field.
type W struct {
	out []byte
	o   int
}

func (w *W) put(v uint64, width int) {
	for j := range width {
		w.out[w.o+j] = byte(v >> (8 * j))
	}
	w.o = w.o + width
}

func (w *W) u8(v uint64)  { w.put(v, 1) }
func (w *W) u64(v uint64) { w.put(v, 8) }

// le reads 8 little-endian bytes.
func le(b []byte) uint64 {
	v := uint64(0)
	for j := 7; j >= 0; j-- {
		v = v<<8 | uint64(b[j])
	}
	return v
}

// onLocal applies op= to the target x.
func onLocal[T Int](a, b T, op byte, sh uint8, i int) T {
	x := a
	switch op {
	case 0:
		x += b
	case 1:
		x -= b
	case 2:
		x *= b
	case 3:
		x /= b
	case 4:
		x %= b
	case 5:
		x &= b
	case 6:
		x |= b
	case 7:
		x ^= b
	case 8:
		x &^= b
	case 9:
		x <<= sh
	case 10:
		x >>= sh
	}
	return x
}

// onField applies op= to the target s.V.
func onField[T Int](a, b T, op byte, sh uint8, i int) T {
	var s Box[T]
	s.V = a
	switch op {
	case 0:
		s.V += b
	case 1:
		s.V -= b
	case 2:
		s.V *= b
	case 3:
		s.V /= b
	case 4:
		s.V %= b
	case 5:
		s.V &= b
	case 6:
		s.V |= b
	case 7:
		s.V ^= b
	case 8:
		s.V &^= b
	case 9:
		s.V <<= sh
	case 10:
		s.V >>= sh
	}
	return s.V
}

// onPointerField applies op= to the target s.V.
func onPointerField[T Int](a, b T, op byte, sh uint8, i int) T {
	s := &Box[T]{V: a}
	switch op {
	case 0:
		s.V += b
	case 1:
		s.V -= b
	case 2:
		s.V *= b
	case 3:
		s.V /= b
	case 4:
		s.V %= b
	case 5:
		s.V &= b
	case 6:
		s.V |= b
	case 7:
		s.V ^= b
	case 8:
		s.V &^= b
	case 9:
		s.V <<= sh
	case 10:
		s.V >>= sh
	}
	return s.V
}

// onArray applies op= to the target arr[i%3].
func onArray[T Int](a, b T, op byte, sh uint8, i int) T {
	var arr [3]T
	arr[i%3] = a
	switch op {
	case 0:
		arr[i%3] += b
	case 1:
		arr[i%3] -= b
	case 2:
		arr[i%3] *= b
	case 3:
		arr[i%3] /= b
	case 4:
		arr[i%3] %= b
	case 5:
		arr[i%3] &= b
	case 6:
		arr[i%3] |= b
	case 7:
		arr[i%3] ^= b
	case 8:
		arr[i%3] &^= b
	case 9:
		arr[i%3] <<= sh
	case 10:
		arr[i%3] >>= sh
	}
	return arr[i%3]
}

// onStructArray applies op= to the target s.Arr[i%3].
func onStructArray[T Int](a, b T, op byte, sh uint8, i int) T {
	var s Box[T]
	s.Arr[i%3] = a
	switch op {
	case 0:
		s.Arr[i%3] += b
	case 1:
		s.Arr[i%3] -= b
	case 2:
		s.Arr[i%3] *= b
	case 3:
		s.Arr[i%3] /= b
	case 4:
		s.Arr[i%3] %= b
	case 5:
		s.Arr[i%3] &= b
	case 6:
		s.Arr[i%3] |= b
	case 7:
		s.Arr[i%3] ^= b
	case 8:
		s.Arr[i%3] &^= b
	case 9:
		s.Arr[i%3] <<= sh
	case 10:
		s.Arr[i%3] >>= sh
	}
	return s.Arr[i%3]
}

// onPointerArray applies op= to the target pa[i%3].
func onPointerArray[T Int](a, b T, op byte, sh uint8, i int) T {
	pa := &[3]T{}
	pa[i%3] = a
	switch op {
	case 0:
		pa[i%3] += b
	case 1:
		pa[i%3] -= b
	case 2:
		pa[i%3] *= b
	case 3:
		pa[i%3] /= b
	case 4:
		pa[i%3] %= b
	case 5:
		pa[i%3] &= b
	case 6:
		pa[i%3] |= b
	case 7:
		pa[i%3] ^= b
	case 8:
		pa[i%3] &^= b
	case 9:
		pa[i%3] <<= sh
	case 10:
		pa[i%3] >>= sh
	}
	return pa[i%3]
}

// onSlice applies op= to the target sl[i%3].
func onSlice[T Int](a, b T, op byte, sh uint8, i int) T {
	sl := make([]T, 3)
	sl[i%3] = a
	switch op {
	case 0:
		sl[i%3] += b
	case 1:
		sl[i%3] -= b
	case 2:
		sl[i%3] *= b
	case 3:
		sl[i%3] /= b
	case 4:
		sl[i%3] %= b
	case 5:
		sl[i%3] &= b
	case 6:
		sl[i%3] |= b
	case 7:
		sl[i%3] ^= b
	case 8:
		sl[i%3] &^= b
	case 9:
		sl[i%3] <<= sh
	case 10:
		sl[i%3] >>= sh
	}
	return sl[i%3]
}

// onDeref applies op= to the target *q.
func onDeref[T Int](a, b T, op byte, sh uint8, i int) T {
	v := a
	q := &v
	switch op {
	case 0:
		*q += b
	case 1:
		*q -= b
	case 2:
		*q *= b
	case 3:
		*q /= b
	case 4:
		*q %= b
	case 5:
		*q &= b
	case 6:
		*q |= b
	case 7:
		*q ^= b
	case 8:
		*q &^= b
	case 9:
		*q <<= sh
	case 10:
		*q >>= sh
	}
	return v
}

// onMapPresent applies op= to the target m[i].
func onMapPresent[T Int](a, b T, op byte, sh uint8, i int) T {
	m := map[int]T{i: a}
	switch op {
	case 0:
		m[i] += b
	case 1:
		m[i] -= b
	case 2:
		m[i] *= b
	case 3:
		m[i] /= b
	case 4:
		m[i] %= b
	case 5:
		m[i] &= b
	case 6:
		m[i] |= b
	case 7:
		m[i] ^= b
	case 8:
		m[i] &^= b
	case 9:
		m[i] <<= sh
	case 10:
		m[i] >>= sh
	}
	return m[i]
}

// onMapAbsent applies op= to the target m[i].
func onMapAbsent[T Int](a, b T, op byte, sh uint8, i int) T {
	m := map[int]T{i + 1: a}
	switch op {
	case 0:
		m[i] += b
	case 1:
		m[i] -= b
	case 2:
		m[i] *= b
	case 3:
		m[i] /= b
	case 4:
		m[i] %= b
	case 5:
		m[i] &= b
	case 6:
		m[i] |= b
	case 7:
		m[i] ^= b
	case 8:
		m[i] &^= b
	case 9:
		m[i] <<= sh
	case 10:
		m[i] >>= sh
	}
	return m[i] + m[i+1]
}

// onNested applies op= to the target ss[i%2].Arr[(i+1)%3].
func onNested[T Int](a, b T, op byte, sh uint8, i int) T {
	ss := []Box[T]{{}, {}}
	ss[i%2].Arr[(i+1)%3] = a
	switch op {
	case 0:
		ss[i%2].Arr[(i+1)%3] += b
	case 1:
		ss[i%2].Arr[(i+1)%3] -= b
	case 2:
		ss[i%2].Arr[(i+1)%3] *= b
	case 3:
		ss[i%2].Arr[(i+1)%3] /= b
	case 4:
		ss[i%2].Arr[(i+1)%3] %= b
	case 5:
		ss[i%2].Arr[(i+1)%3] &= b
	case 6:
		ss[i%2].Arr[(i+1)%3] |= b
	case 7:
		ss[i%2].Arr[(i+1)%3] ^= b
	case 8:
		ss[i%2].Arr[(i+1)%3] &^= b
	case 9:
		ss[i%2].Arr[(i+1)%3] <<= sh
	case 10:
		ss[i%2].Arr[(i+1)%3] >>= sh
	}
	return ss[i%2].Arr[(i+1)%3]
}

// onPointerChain applies op= to the target h.B.V.
func onPointerChain[T Int](a, b T, op byte, sh uint8, i int) T {
	h := &Holder[T]{B: &Box[T]{V: a}}
	switch op {
	case 0:
		h.B.V += b
	case 1:
		h.B.V -= b
	case 2:
		h.B.V *= b
	case 3:
		h.B.V /= b
	case 4:
		h.B.V %= b
	case 5:
		h.B.V &= b
	case 6:
		h.B.V |= b
	case 7:
		h.B.V ^= b
	case 8:
		h.B.V &^= b
	case 9:
		h.B.V <<= sh
	case 10:
		h.B.V >>= sh
	}
	return h.B.V
}

// onCaptured applies op= to the target c.
func onCaptured[T Int](a, b T, op byte, sh uint8, i int) T {
	c := a
	f := func() T { return c }
	switch op {
	case 0:
		func() { c += b }()
	case 1:
		func() { c -= b }()
	case 2:
		func() { c *= b }()
	case 3:
		func() { c /= b }()
	case 4:
		func() { c %= b }()
	case 5:
		func() { c &= b }()
	case 6:
		func() { c |= b }()
	case 7:
		func() { c ^= b }()
	case 8:
		func() { c &^= b }()
	case 9:
		func() { c <<= sh }()
	case 10:
		func() { c >>= sh }()
	}
	return f()
}

// Mode 0 and 1: every operator on every target for one type. b is the
// divisor of /= and %= only when in[3] selects it (it may be 0: Go
// panics); otherwise b|1. sh is any count, often at or beyond the width.
func targets[T Int](w *W, in []byte) {
	a, b := T(le(in[4:12])), T(le(in[12:20]))
	sh := in[20]
	i := int(in[21])
	for op := byte(0); op < 11; op++ {
		y := b
		if (op == 3 || op == 4) && in[3]%5 != 0 {
			y = b | 1
		}
		// Every target's result, folded (FNV-1a over 64-bit words).
		h := uint64(14695981039346656037)
		h = (h ^ uint64(onLocal(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onField(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onPointerField(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onArray(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onStructArray(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onPointerArray(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onSlice(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onDeref(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onMapPresent(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onMapAbsent(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onNested(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onPointerChain(a, y, op, sh, i))) * 1099511628211
		h = (h ^ uint64(onCaptured(a, y, op, sh, i))) * 1099511628211
		w.u64(h)
	}
}

// Mode 2: shift counts of the target's own type (any integer type may be a
// shift count; a negative signed count panics), MinInt / -1 and explicit,
// non-generic compound assignments.
func counts[T Int](w *W, a, c T) {
	x, y := a, a
	x <<= c
	y >>= c
	w.u64(uint64(x))
	w.u64(uint64(y))
}

func edges(w *W, in []byte) {
	sel := in[2]
	// Shift counts of every type; negative signed counts only when selected.
	cnt := int8(in[4])
	if sel%4 != 0 && cnt < 0 {
		cnt = -cnt - 1
	}
	counts(w, uint8(le(in[8:16])), uint8(cnt))
	counts(w, int16(le(in[8:16])), int16(cnt))
	counts(w, uint32(le(in[8:16])), uint32(in[5]))
	counts(w, int64(le(in[8:16])), int64(cnt))
	counts(w, int(le(in[8:16])), int(cnt))
	counts(w, uint(le(in[8:16])), uint(in[5]))
	var z8 int8 = int8(in[6])
	z8 <<= cnt
	w.u64(uint64(z8))
	var u uint64 = le(in[8:16])
	u >>= int32(cnt)
	w.u64(u)
	// MinInt / -1 is MinInt and MinInt % -1 is 0, at every width.
	m8, m16, m32, m64, mi := int8(-128), int16(-32768), int32(-2147483648), int64(-9223372036854775808), int(-9223372036854775808)
	r8, r16, r32, r64, ri := m8, m16, m32, m64, mi
	neg := int8(in[7]%2) - 1 // -1 or 0 when the selector is 0 mod 2
	if sel%2 != 0 {
		neg = -1
	}
	m8 /= neg
	m16 /= int16(neg)
	m32 /= int32(neg)
	m64 /= int64(neg)
	mi /= int(neg)
	r8 %= neg
	r16 %= int16(neg)
	r32 %= int32(neg)
	r64 %= int64(neg)
	ri %= int(neg)
	w.u64(uint64(m8))
	w.u64(uint64(m16))
	w.u64(uint64(m32))
	w.u64(uint64(m64))
	w.u64(uint64(mi))
	w.u64(uint64(r8) + uint64(r16) + uint64(r32) + uint64(r64) + uint64(ri))
	// Signed division truncates toward zero; the remainder has the
	// dividend's sign.
	d := int32(le(in[16:24]))
	e := int32(in[24]%7) - 3
	if e == 0 {
		e = 2
	}
	q, r := d, d
	q /= e
	r %= e
	w.u64(uint64(q))
	w.u64(uint64(r))
	// Named types and the output byte slice itself.
	amt := Amount(le(in[8:16]))
	amt *= 3
	amt -= Amount(in[25])
	amt &^= 0xff
	w.u64(uint64(amt))
	dl := Delta(le(in[16:24]))
	dl >>= 3
	dl ^= -1
	w.u64(uint64(dl))
	w.out[w.o] = in[26]
	w.out[w.o] += in[27]
	w.out[w.o] <<= in[28] % 9
	w.out[w.o] |= 1
	w.o = w.o + 1
}

// Mode 3: evaluation order. Index and pointer operands are evaluated once,
// before the right operand; the target is read after the right operand, so
// a right operand that changes the target's value is seen; traps on the
// target (bounds, nil, nil map) and in the operator come after the right
// operand's side effects.
func order(w *W, in []byte) {
	a := []uint64{1, 2, 3}
	idx := func(i int, tag byte) int { w.u8(uint64(tag)); return i }
	rhs := func(v uint64, tag byte) uint64 { w.u8(uint64(tag)); a[1] = 50; return v }
	a[idx(1, 1)] += rhs(7, 2)
	w.u64(a[1])
	x := uint32(in[1])
	setx := func() uint32 { x = 100; return 5 }
	x += setx()
	w.u64(uint64(x))
	// The right operand grows the map; the element is found after it.
	m := map[uint8]uint64{1: 1}
	grow := func() uint64 {
		for k := uint8(10); k < 40; k++ {
			m[k] = uint64(k)
		}
		m[1] = 40
		return 2
	}
	m[1] += grow()
	m[200] -= grow()
	w.u64(m[1])
	w.u64(m[200])
	w.u64(uint64(len(m)))
	// Each operand call runs once.
	calls := 0
	f := func() int { calls++; return int(in[2]) % 3 }
	arr := [3]int16{int16(in[3]), -5, 7}
	arr[f()] *= -3
	v := int64(in[4])
	ptr := func() *int64 { calls++; return &v }
	*ptr() <<= 3
	*ptr() -= int64(f())
	var s Box[uint16]
	s.Arr[f()] |= uint16(in[5]) << 8
	ms := map[string]int8{}
	key := func() string { calls++; return string(in[6:7]) }
	ms[key()] -= int8(in[7])
	ms[key()] *= 2
	w.u8(uint64(calls))
	w.u64(uint64(arr[0]) ^ uint64(arr[1]) ^ uint64(arr[2]))
	w.u64(uint64(v))
	w.u64(uint64(s.Arr[0]) + uint64(s.Arr[1]) + uint64(s.Arr[2]))
	w.u64(uint64(ms[string(in[6:7])]))
	// Traps after the right operand's side effects.
	switch in[8] % 8 {
	case 0:
		c := []uint8{1}
		c[idx(int(in[9])%3, 3)] += uint8(rhs(1, 4))
		w.u8(uint64(c[0]))
	case 1:
		var p *int32
		*p += int32(rhs(1, 5))
	case 2:
		var nm map[string]int8
		nm[string(in[10:11])] ^= int8(rhs(1, 6))
	case 3:
		z := int16(in[11])
		z %= int16(rhs(uint64(in[12]%2), 7))
		w.u64(uint64(z))
	case 4:
		q := uint8(1)
		q <<= int8(rhs(uint64(in[13]), 8))
		w.u64(uint64(q))
	case 5:
		var bp *Box[int8]
		bp.V -= int8(rhs(1, 9))
	case 6:
		var nm map[uint64]uint64
		nm[uint64(idx(1, 10))] /= rhs(0, 11) // both panics are aborts
	}
}

// Mode 4: loops with compound post statements and bodies, and closures
// capturing per-iteration variables updated by the post statement.
func loops(w *W, in []byte) {
	n := int(in[1]%40) + 1
	sum := uint16(0)
	for i := 0; i < n; i += 3 {
		sum += uint16(i) * 7
		sum ^= uint16(in[2])
	}
	w.u64(uint64(sum))
	down := int8(0)
	for j := int8(100); j > -100; j -= int8(in[3]%13) + 1 {
		down += j
	}
	w.u64(uint64(down))
	var fs []func() int
	for i := 1; i < 200; i *= 3 {
		fs = append(fs, func() int { return i })
	}
	for _, f := range fs {
		w.u64(uint64(f()))
	}
	acc := uint64(1)
	for k := uint8(in[4]); k != 0; k >>= 1 {
		acc *= 3
		acc %= 1000003
	}
	w.u64(acc)
}

// Run dispatches on in[0]: modes 0 and 1 run targets for the type in[1]
// selects (mode 1 with the other half of the types).
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0, 1:
		switch (uint(in[0])*6 + uint(in[1])%6) % 12 {
		case 0:
			targets[uint8](&w, in)
		case 1:
			targets[uint16](&w, in)
		case 2:
			targets[uint32](&w, in)
		case 3:
			targets[uint64](&w, in)
		case 4:
			targets[uint](&w, in)
		case 5:
			targets[Amount](&w, in)
		case 6:
			targets[int8](&w, in)
		case 7:
			targets[int16](&w, in)
		case 8:
			targets[int32](&w, in)
		case 9:
			targets[int64](&w, in)
		case 10:
			targets[int](&w, in)
		default:
			targets[Delta](&w, in)
		}
	case 2:
		edges(&w, in)
	case 3:
		order(&w, in)
	case 4:
		loops(&w, in)
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
