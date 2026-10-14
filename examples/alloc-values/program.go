// Package allocvalues is an ordinary-Go oracle for invocation-heap
// allocation in the on-chain subset: new, make, append, copy, slice literals,
// slices of every element kind, pointer and slice fields, and locals that
// escape to the heap. The same source is compiled natively and through goSVM;
// tests compare the output bytes and status (or a panic) for every vector.
// See docs/GO_ALLOCATION.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package allocvalues

// Element kinds of many sizes, with and without pointers.
type B3 [3]byte
type B5 [5]byte
type B12 [12]byte
type W2 [2]uint64
type W3 [3]uint64
type W5 [5]uint64
type W9 [9]uint64
type W65 [65]uint64
type W128 [128]uint64
type Pair struct {
	A uint64
	B uint32
}
type Ref struct {
	P *uint64
	A [4]uint64
}
type BigRef struct {
	P *uint64
	A [64]uint64
}
type Node struct {
	Next *Node
	Kids []*Node
	Val  uint64
}
type List struct {
	Head *Node
	Len  uint64
	Keys [][32]byte
}

// Push stores n, a caller's pointer, in heap data: callers that pass the
// address of a local get that local moved to the heap.
func (l *List) Push(n *Node) {
	n.Next = l.Head
	l.Head = n
	l.Len++
}

func (l *List) Sum() uint64 {
	sum := uint64(0)
	for n := l.Head; n != nil; n = n.Next {
		sum = sum + n.Val
		for _, k := range n.Kids {
			sum = sum + k.Val*3
		}
	}
	return sum
}

func put(out []byte, o *uint64, v uint64, width uint64) {
	for j := uint64(0); j < width; j++ {
		out[*o+j] = byte(v >> (8 * j))
	}
	*o = *o + width
}

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

func get16(in []byte, at uint64) uint64 { return uint64(in[at]) | uint64(in[at+1])<<8 }

// record writes one append step: len, cap and whether the result shares the
// operand's backing array.
func record(out []byte, o *uint64, length, capacity uint64, alias bool) {
	put(out, o, length, 4)
	put(out, o, capacity, 4)
	put(out, o, flag(alias), 1)
}

// count is how an append step adds elements: 1, 2 or 3 values, or a spread
// of 1..8 elements.
func count(in []byte, j uint64) (uint64, uint64) {
	k := uint64(in[5+j])
	return k % 4, k/32 + 1
}

// Each grow function appends to an empty slice step by step and records
// len, cap and aliasing after every append, then a checksum of the elements.
// It stops once the capacity reaches limit bytes.

func growByte(out, in []byte, o *uint64, limit uint64) {
	var s []byte
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []byte
		v := byte(j)
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v+1)
		case 2:
			t = append(s, v, v+1, v+2)
		default:
			extra := make([]byte, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s)) >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(x)
	}
	put(out, o, sum, 8)
}

func growU16(out, in []byte, o *uint64, limit uint64) {
	var s []uint16
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []uint16
		v := uint16(j * 7)
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v+1)
		case 2:
			t = append(s, v, v+1, v+2)
		default:
			extra := make([]uint16, n)
			extra[n-1] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*2 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(x)
	}
	put(out, o, sum, 8)
}

func growU32(out, in []byte, o *uint64, limit uint64) {
	var s []uint32
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []uint32
		v := uint32(j * 11)
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v+1)
		case 2:
			t = append(s, v, v+1, v+2)
		default:
			extra := make([]uint32, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*4 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(x)
	}
	put(out, o, sum, 8)
}

func growU64(out, in []byte, o *uint64, limit uint64) {
	var s []uint64
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []uint64
		switch form {
		case 0:
			t = append(s, j)
		case 1:
			t = append(s, j, j*j)
		case 2:
			t = append(s, j, j*j, j^5)
		default:
			extra := make([]uint64, n)
			extra[0] = j
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*8 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x
	}
	put(out, o, sum, 8)
}

func growPtr(out, in []byte, o *uint64, limit uint64) {
	var s []*uint64
	cell, marker := new(uint64), new(uint64)
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []*uint64
		switch form {
		case 0:
			t = append(s, cell)
		case 1:
			t = append(s, nil, cell)
		case 2:
			t = append(s, cell, nil, cell)
		default:
			extra := make([]*uint64, n)
			extra[0] = cell
			t = append(s, extra...)
		}
		// Element addresses would be **uint64: detect sharing by a write.
		alias := false
		if cap(s) > 0 {
			orig := t[0]
			t[0] = marker
			alias = s[:1][0] == marker
			t[0] = orig
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), alias)
		s = t
		if uint64(cap(s))*8 >= limit {
			break
		}
	}
	*cell = 5
	sum := uint64(0)
	for _, p := range s {
		if p != nil {
			sum = sum + *p
		} else {
			sum = sum*3 + 1
		}
	}
	put(out, o, sum, 8)
}

func growB3(out, in []byte, o *uint64, limit uint64) {
	var s []B3
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []B3
		v := B3{byte(j), 1, byte(j >> 3)}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, B3{})
		case 2:
			t = append(s, v, v, B3{9})
		default:
			extra := make([]B3, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*3 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(x[0]) + uint64(x[2])
	}
	put(out, o, sum, 8)
}

func growB5(out, in []byte, o *uint64, limit uint64) {
	var s []B5
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []B5
		v := B5{byte(j), 2, 3, 4, byte(j * 3)}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, B5{}, v)
		default:
			extra := make([]B5, n)
			extra[n-1] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*5 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(x[0]) + uint64(x[4])
	}
	put(out, o, sum, 8)
}

func growB12(out, in []byte, o *uint64, limit uint64) {
	var s []B12
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []B12
		var v B12
		v[11] = byte(j)
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]B12, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*12 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(x[11])
	}
	put(out, o, sum, 8)
}

func growW2(out, in []byte, o *uint64, limit uint64) {
	var s []W2
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []W2
		v := W2{j, j * 9}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, W2{1, 1})
		default:
			extra := make([]W2, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*16 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x[0] + x[1]
	}
	put(out, o, sum, 8)
}

func growPair(out, in []byte, o *uint64, limit uint64) {
	var s []Pair
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []Pair
		v := Pair{A: j, B: uint32(j * 5)}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, Pair{B: 1})
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]Pair, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*16 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x.A + uint64(x.B)
	}
	put(out, o, sum, 8)
}

func growW3(out, in []byte, o *uint64, limit uint64) {
	var s []W3
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []W3
		v := W3{j, 2, j * j}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, W3{}, v)
		default:
			extra := make([]W3, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*24 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x[0] + x[2]
	}
	put(out, o, sum, 8)
}

func growBytes(out, in []byte, o *uint64, limit uint64) {
	var s [][]byte
	marker := make([]byte, 99)
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t [][]byte
		v := []byte{byte(j), 7}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, nil)
		case 2:
			t = append(s, nil, v, v[1:])
		default:
			extra := make([][]byte, n)
			extra[0] = v
			t = append(s, extra...)
		}
		alias := false
		if cap(s) > 0 {
			orig := t[0]
			t[0] = marker
			alias = len(s[:1][0]) == 99
			t[0] = orig
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), alias)
		s = t
		if uint64(cap(s))*24 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + uint64(len(x))
		if len(x) > 0 {
			sum = sum + uint64(x[0])
		}
	}
	put(out, o, sum, 8)
}

func growW5(out, in []byte, o *uint64, limit uint64) {
	var s []W5
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []W5
		v := W5{j, 0, 0, 0, j + 4}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]W5, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*40 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x[0] + x[4]
	}
	put(out, o, sum, 8)
}

func growRef(out, in []byte, o *uint64, limit uint64) {
	var s []Ref
	cell := new(uint64)
	*cell = 3
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []Ref
		v := Ref{P: cell, A: [4]uint64{j, 0, 0, 1}}
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, Ref{})
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]Ref, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*40 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x.A[0] + x.A[3]
		if x.P != nil {
			sum = sum + *x.P
		}
	}
	put(out, o, sum, 8)
}

func growW9(out, in []byte, o *uint64, limit uint64) {
	var s []W9
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []W9
		var v W9
		v[8] = j
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]W9, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*72 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x[8]
	}
	put(out, o, sum, 8)
}

func growW65(out, in []byte, o *uint64, limit uint64) {
	var s []W65
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []W65
		var v W65
		v[64] = j + 1
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]W65, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*520 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x[64]
	}
	put(out, o, sum, 8)
}

func growBigRef(out, in []byte, o *uint64, limit uint64) {
	var s []BigRef
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []BigRef
		var v BigRef
		v.A[63] = j + 2
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]BigRef, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*520 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x.A[63]
	}
	put(out, o, sum, 8)
}

func growW128(out, in []byte, o *uint64, limit uint64) {
	var s []W128
	for j := uint64(0); j < uint64(in[2]); j++ {
		form, n := count(in, j)
		var t []W128
		var v W128
		v[127] = j + 3
		switch form {
		case 0:
			t = append(s, v)
		case 1:
			t = append(s, v, v)
		case 2:
			t = append(s, v, v, v)
		default:
			extra := make([]W128, n)
			extra[0] = v
			t = append(s, extra...)
		}
		record(out, o, uint64(len(t)), uint64(cap(t)), cap(s) > 0 && &s[:1][0] == &t[:1][0])
		s = t
		if uint64(cap(s))*1024 >= limit {
			break
		}
	}
	sum := uint64(0)
	for _, x := range s {
		sum = sum*3 + x[127]
	}
	put(out, o, sum, 8)
}

// GrowKinds is the number of element kinds grow dispatches on.
const GrowKinds = 17

func grow(out, in []byte, o *uint64) {
	limit := get16(in, 3)
	switch uint64(in[1]) % GrowKinds {
	case 0:
		growByte(out, in, o, limit)
	case 1:
		growU16(out, in, o, limit)
	case 2:
		growU32(out, in, o, limit)
	case 3:
		growU64(out, in, o, limit)
	case 4:
		growPtr(out, in, o, limit)
	case 5:
		growB3(out, in, o, limit)
	case 6:
		growB5(out, in, o, limit)
	case 7:
		growB12(out, in, o, limit)
	case 8:
		growW2(out, in, o, limit)
	case 9:
		growPair(out, in, o, limit)
	case 10:
		growW3(out, in, o, limit)
	case 11:
		growBytes(out, in, o, limit)
	case 12:
		growW5(out, in, o, limit)
	case 13:
		growRef(out, in, o, limit)
	case 14:
		growW9(out, in, o, limit)
	case 15:
		growW65(out, in, o, limit)
	default:
		if in[4]&1 == 0 {
			growBigRef(out, in, o, limit)
		} else {
			growW128(out, in, o, limit)
		}
	}
}

// zeros makes slices and allocates values, reports their len/cap and that
// they are zero. make panics when len > cap or the size is out of range.
func zeros(out, in []byte, o *uint64) {
	n, m := get16(in, 1), get16(in, 3)
	if in[5] == 1 {
		n = n << 48
	}
	words := make([]uint64, n, m)
	zero := true
	for _, w := range words[:cap(words)] {
		zero = zero && w == 0
	}
	put(out, o, uint64(len(words)), 4)
	put(out, o, uint64(cap(words)), 4)
	put(out, o, flag(zero), 1)
	bytes := make([]byte, m)
	for _, b := range bytes {
		zero = zero && b == 0
	}
	refs := make([]Ref, n%8)
	for _, r := range refs {
		zero = zero && r.P == nil && r.A[3] == 0
	}
	w := new(W5)
	p := new(Ref)
	u := new(uint16)
	l := new(List)
	zero = zero && w[4] == 0 && p.P == nil && *u == 0 && l.Head == nil && l.Keys == nil && len(l.Keys) == 0
	put(out, o, flag(zero), 1)
	put(out, o, uint64(len(bytes))+uint64(cap(refs)), 4)
	var none []uint64
	empty := make([]uint64, 0)
	put(out, o, flag(none == nil)|flag(empty == nil)<<1|flag(make([]Pair, 0, 3) != nil)<<2, 1)
}

// copies exercises copy's overlap and length rules for several element sizes.
func copies(out, in []byte, o *uint64) {
	s := make([]uint64, 16)
	for j := range s {
		s[j] = uint64(j)*7 + 1
	}
	a, b, c := uint64(in[1])%17, uint64(in[2])%17, uint64(in[3])%17
	n := uint64(copy(s[a:], s[b:c]))
	put(out, o, n, 1)
	for _, x := range s {
		put(out, o, x, 1)
	}
	bs := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	n = uint64(copy(bs[a%11:], bs[b%11:]))
	put(out, o, n, 1)
	for _, x := range bs {
		put(out, o, uint64(x), 1)
	}
	ws := make([]B3, 6)
	for j := range ws {
		ws[j] = B3{byte(j), byte(j) * 2, 1}
	}
	n = uint64(copy(ws[a%7:c%7], ws[b%7:]))
	put(out, o, n, 1)
	for _, x := range ws {
		put(out, o, uint64(x[0])+uint64(x[1])<<4, 1)
	}
	var dst [4]uint32
	n = uint64(copy(dst[:], []uint32{9, 8, 7, 6, 5, 4}))
	put(out, o, n+uint64(dst[3]), 1)
}

// bounds slices a made slice and an array of uint32 with input bounds; Go
// panics, and SBF aborts, when they are out of range.
func bounds(out, in []byte, o *uint64) {
	s := make([]W2, in[1]%9, in[2]%17)
	lo, hi, max := uint64(in[3]), uint64(in[4]), uint64(in[5])
	var t []W2
	if in[6]&1 == 0 {
		t = s[lo:hi]
	} else {
		t = s[lo:hi:max]
	}
	put(out, o, uint64(len(t)), 1)
	put(out, o, uint64(cap(t)), 1)
	if len(t) > 0 {
		t[0][1] = 77
		put(out, o, s[lo][1], 1)
	}
	var a [8]uint32
	v := a[lo%9 : hi%9]
	if len(v) > 0 {
		v[uint64(len(v))-1] = 5
	}
	put(out, o, uint64(a[7])+uint64(len(v))<<4, 1)
	full := s[:cap(s)]
	put(out, o, uint64(len(full)), 1)
	put(out, o, t[in[7]%4][0], 1)
}

// newNode returns the address of a local: it escapes, so it is allocated.
func newNode(v uint64) *Node {
	n := Node{Val: v}
	return &n
}

func (n Node) Copy() *Node { return &n }

// lists builds linked structures with new, &T{}, escaping locals, elided
// &T{} slice elements and caller locals stored by a method.
func lists(out, in []byte, o *uint64) {
	var l List
	count := uint64(in[1]) % 40
	for j := uint64(0); j < count; j++ {
		switch j % 4 {
		case 0:
			l.Push(newNode(j))
		case 1:
			n := new(Node)
			n.Val = j * 2
			l.Push(n)
		case 2:
			local := Node{Val: j + 100}
			l.Push(&local)
		default:
			l.Push(&Node{Val: j, Kids: []*Node{{Val: 1}, newNode(j)}})
		}
	}
	put(out, o, l.Sum(), 8)
	put(out, o, l.Len, 1)
	var key [32]byte
	key[0] = in[2]
	l.Keys = append(l.Keys, key, [32]byte{1, 2})
	l.Keys[1][31] = 9
	l.Keys = append(l.Keys[:1], l.Keys...)
	put(out, o, uint64(len(l.Keys))+uint64(l.Keys[2][31])<<4+uint64(l.Keys[1][0])<<8, 4)
	if l.Head != nil {
		c := l.Head.Copy()
		c.Val = 1000
		put(out, o, l.Head.Val+c.Val, 8)
	}
	tree := &Node{Val: 1}
	for j := uint64(0); j < uint64(in[3])%10; j++ {
		tree.Kids = append(tree.Kids, &Node{Val: j})
		tree.Kids[0].Kids = append(tree.Kids[0].Kids, tree.Kids[uint64(len(tree.Kids))-1])
	}
	sum := uint64(0)
	for _, k := range tree.Kids {
		sum = sum + k.Val + uint64(len(k.Kids))*100
	}
	put(out, o, sum, 8)
}

// escapes checks Go 1.22 per-iteration variables and moved locals: every
// iteration's address is distinct.
func escapes(out, in []byte, o *uint64) {
	var ps []*uint64
	n := uint64(in[1]) % 20
	for j := uint64(0); j < n; j++ {
		v := j * 3
		ps = append(ps, &v)
	}
	for _, x := range [4]uint64{5, 6, 7, 8} {
		ps = append(ps, &x)
	}
	var kept *uint64
	{
		inner := uint64(in[2])
		kept = &inner
	}
	*kept = *kept + 1
	ps = append(ps, kept)
	for _, p := range ps {
		*p = *p + 1
	}
	sum := uint64(0)
	for j, p := range ps {
		sum = sum + *p*(uint64(j)+1)
	}
	put(out, o, sum, 8)
	put(out, o, flag(len(ps) > 1 && ps[0] != ps[1]), 1)
	view := func3(in[3])
	put(out, o, uint64(len(view))+uint64(view[1]), 2)
}

// func3 returns a view of a local array, which therefore lives on the heap.
func func3(b byte) []byte {
	var k [8]byte
	k[1] = b
	return k[1:4]
}

// literals covers slice literals: keyed, nested, of arrays and of slices.
func literals(out, in []byte, o *uint64) {
	ks := []uint16{5: 1, 2: 3, 7}
	put(out, o, uint64(len(ks))+uint64(cap(ks))<<4, 1)
	put(out, o, uint64(ks[2])+uint64(ks[5])<<4, 1)
	grid := [][]byte{{1, 2}, nil, {in[1]}}
	grid[1] = append(grid[1], grid[0]...)
	grid[1][0] = 9
	put(out, o, uint64(grid[0][0])+uint64(grid[1][0])<<4+uint64(len(grid[1]))<<8+uint64(grid[2][0])<<12, 4)
	keys := [][32]byte{{1}, {31: in[2]}}
	keys = append(keys, keys[1])
	keys[2][0] = 4
	put(out, o, uint64(keys[1][31])+uint64(keys[2][0])<<8+uint64(keys[0][0])<<12, 4)
	var s []uint64
	s = append(s[:0], s...)
	put(out, o, flag(s == nil), 1)
	s = append(s, []uint64(nil)...)
	put(out, o, flag(s == nil), 1)
	pairs := []Pair{{A: 1}, {B: 2}, {3, 4}}
	put(out, o, pairs[0].A+uint64(pairs[1].B)+pairs[2].A, 1)
	for j, v := range []uint32{10, 20, 30} {
		if uint64(j) == uint64(in[3])%3 {
			put(out, o, uint64(v), 1)
		}
	}
}

// aliasing shows that appends within capacity share the backing array.
func aliasing(out, in []byte, o *uint64) {
	s := make([]uint32, 3, 4+uint64(in[1])%3)
	s[0] = 1
	t := append(s, 4)
	u := append(s, 5)
	put(out, o, uint64(t[3])+uint64(u[3])<<8, 2)
	v := append(t, 6, 7)
	v[0] = 8
	put(out, o, uint64(s[0])+uint64(cap(v))<<8+flag(&v[0] == &s[0])<<16, 4)
	tail := s[:cap(s)]
	sum := uint64(0)
	for _, x := range tail {
		sum = sum*10 + uint64(x)
	}
	put(out, o, sum, 8)
	ss := s[1:2]
	ss = append(ss, 9)
	put(out, o, uint64(s[2])+uint64(cap(ss))<<8, 2)
}

// Touch allocates n bytes and writes the last one: the SBF heap-budget probe.
func touch(out, in []byte, o *uint64) {
	n := uint64(in[1]) | uint64(in[2])<<8 | uint64(in[3])<<16
	b := make([]byte, n)
	if n > 0 {
		b[n-1] = 1
	}
	put(out, o, uint64(len(b)), 4)
}

// Run dispatches on in[0] and writes results into out from offset 0.
func Run(out, in []byte) uint64 {
	o := uint64(0)
	switch in[0] {
	case 0:
		grow(out, in, &o)
	case 1:
		zeros(out, in, &o)
	case 2:
		copies(out, in, &o)
	case 3:
		bounds(out, in, &o)
	case 4:
		lists(out, in, &o)
	case 5:
		escapes(out, in, &o)
	case 6:
		literals(out, in, &o)
	case 7:
		aliasing(out, in, &o)
	case 8:
		touch(out, in, &o)
	default:
		return 7
	}
	return 0
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
