// Package genericvalues is an ordinary-Go oracle for generics in the on-chain
// subset: generic functions with inferred and explicit instantiation, type
// sets, comparable and any constraints, method constraints, generic structs
// and arrays with methods, generic types as fields and behind pointers, and
// nested generic calls, each instantiated with several unsigned widths.
package genericvalues

// Unsigned is a type-set constraint: arithmetic, shifts and ordering are
// permitted on its type parameters.
type Unsigned interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Wide is a narrower union.
type Wide interface{ ~uint32 | ~uint64 }

// Sizer is a method constraint, dispatched statically per instance.
type Sizer interface{ Size() uint64 }

type Port uint16

func (p Port) Size() uint64 { return uint64(p) * 2 }

type Key [4]byte

func Min[T Unsigned](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func Max[T Unsigned](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Clamp nests generic calls; its instances call Min and Max instances.
func Clamp[T Unsigned](x, lo, hi T) T { return Min(Max(x, lo), hi) }

func SumArray[T Unsigned](a *[4]T) T {
	var total T
	for _, v := range a {
		total = total + v
	}
	return total
}

func MaxArray[T Unsigned](a [4]T) T {
	best := a[0]
	for i := range a {
		best = Max(best, a[i])
	}
	return best
}

// SumView reads a byte view and accumulates in T, wrapping at T's width.
func SumView[T Unsigned](s []byte) T {
	var total T
	for _, b := range s {
		total = total + T(b)*3
	}
	return total
}

func Div[T Unsigned](a, b T) (T, T) { return a / b, a % b }

func Shift[T Unsigned](x T, n uint8) (T, T) { return x << n, x >> n }

func Neg[T Unsigned](x T) T { return -x ^ 0x0f }

// Widen converts between two type parameters.
func Widen[T, U Unsigned](x T) U { return U(x) + 1 }

func Index[T comparable](a *[4]T, v T) uint64 {
	for i, x := range a {
		if x == v {
			return uint64(i)
		}
	}
	return 99
}

func Swap[T any](a, b *T) {
	t := *a
	*a = *b
	*b = t
}

func TotalSize[T Sizer](a, b T) uint64 { return a.Size() + b.Size() }

// Stack is a fixed-capacity generic stack: its storage is an array of T.
type Stack[T Unsigned] struct {
	items [4]T
	n     uint64
}

func (s *Stack[T]) Push(v T) {
	s.items[s.n] = v // a full stack panics with an index out of range
	s.n++
}

func (s *Stack[T]) Pop() (T, bool) {
	if s.n == 0 {
		var zero T
		return zero, false
	}
	s.n--
	return s.items[s.n], true
}

func (s Stack[T]) Sum() T { return SumArray(&s.items) }

func (s *Stack[T]) Top() *T { return &s.items[s.n-1] }

// Pair has two type parameters; Flip returns another instance.
type Pair[K, V Unsigned] struct {
	Key K
	Val V
}

func (p Pair[K, V]) Flip() Pair[V, K] { return Pair[V, K]{Key: p.Val, Val: p.Key} }

func (p *Pair[K, V]) Bump() { p.Key++; p.Val-- }

// Vec is a generic named array with a method.
type Vec[T Wide] [4]T

func (v *Vec[T]) Dot(w Vec[T]) T {
	var total T
	for i := range v {
		total = total + v[i]*w[i]
	}
	return total
}

// Book holds generic types as fields.
type Book struct {
	Bids Stack[uint32]
	Asks Stack[uint64]
	Meta Pair[uint8, uint16]
}

func Fill[T Unsigned](s *Stack[T], v T, n uint64) {
	for i := uint64(0); i < n; i++ {
		s.Push(v + T(i))
	}
}

func read64(s []byte, off uint64) uint64 {
	var v uint64
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(s[off+j])<<(8*j)
	}
	return v
}

func write64(s []byte, off uint64, v uint64) {
	for j := uint64(0); j < 8; j++ {
		s[off+j] = byte(v >> (8 * j))
	}
}

func write32(s []byte, off uint64, v uint32) { write64(s, off, uint64(v)) }

func Process(s, i []byte) uint64 {
	a, b := read64(i, 1), read64(i, 8)
	c := i[15]
	mode := i[0] % 8
	if mode == 0 {
		// Min, Max and Clamp at every width, inferred and explicit.
		write64(s, 0, Min(a, b))
		write32(s, 8, Max(uint32(a), uint32(b)))
		s[12] = Min[uint8](uint8(a), uint8(b))
		s[13] = byte(Max[uint16](uint16(a), uint16(b)) >> 3)
		s[14] = Clamp(uint8(a), 10, 200)
		s[15] = byte(Clamp[uint16](uint16(b), uint16(c), 4000) >> 4)
		write32(s, 16, uint32(Clamp(Port(a), Port(c), Port(b))))
		return Clamp(a%7, 1, 5)
	}
	if mode == 1 {
		// Sums over arrays and views, wrapping at each width.
		u8 := [4]uint8{uint8(a), uint8(a >> 8), uint8(b), c}
		u16 := [4]uint16{uint16(a), uint16(b), 65535, uint16(c)}
		u32 := [4]uint32{uint32(a), uint32(b), uint32(a >> 32), 7}
		u64 := [4]uint64{a, b, a * b, uint64(c)}
		s[0] = SumArray(&u8)
		s[1] = byte(SumArray(&u16) >> 1)
		write32(s, 2, SumArray(&u32))
		write64(s, 6, SumArray(&u64)^MaxArray(u64))
		s[14] = MaxArray(u8)
		s[15] = SumView[uint8](i[1:9])
		write32(s, 16, uint32(SumView[uint16](i[2:c%16]))+SumView[uint32](s[0:4]))
		return uint64(SumView[uint64](i[:2]) % 3)
	}
	if mode == 2 {
		// Division (by zero panics), shifts and negation per width.
		q, r := Div(a, b%64)
		write64(s, 0, q)
		s[8], s[9] = Div(uint8(a), uint8(b))
		h, l := Shift(uint16(a), c%20)
		write32(s, 10, uint32(h)<<16|uint32(l))
		x, y := Shift(uint32(b), c)
		write32(s, 14, x^y)
		s[18] = Neg(uint8(a))
		write32(s, 19, uint32(Neg(uint16(b))))
		return r % 5
	}
	if mode == 3 {
		// Generic stack: pushes past capacity panic; pops, sums and pointers.
		var st Stack[uint16]
		Fill(&st, uint16(a), uint64(c%6))
		top, ok := st.Pop()
		if !ok {
			return 3
		}
		write32(s, 0, uint32(top))
		write32(s, 4, uint32(st.Sum()))
		if st.n > 0 {
			p := st.Top()
			*p = *p * 3
		}
		var wide Stack[uint64]
		wide.Push(b)
		wide.Push(a)
		write64(s, 8, wide.Sum())
		st.Push(9)
		write64(s, 16, uint64(st.Sum())+st.n)
		return 0
	}
	if mode == 4 {
		// Generic types as fields; instances returning other instances.
		var book Book
		Fill(&book.Bids, uint32(a), 2)
		book.Asks.Push(b)
		book.Meta = Pair[uint8, uint16]{Key: uint8(a), Val: uint16(b)}
		book.Meta.Bump()
		flipped := book.Meta.Flip()
		write32(s, 0, book.Bids.Sum())
		write64(s, 4, book.Asks.Sum())
		write32(s, 12, uint32(flipped.Key)<<8|uint32(flipped.Val))
		back := flipped.Flip()
		s[16] = back.Key
		write32(s, 17, uint32(back.Val))
		if back.Key != book.Meta.Key || back.Val != book.Meta.Val {
			return 1
		}
		return 0
	}
	if mode == 5 {
		// comparable and any constraints, with arrays, structs and bools.
		keys := [4]uint32{uint32(a), uint32(b), 7, uint32(c)}
		flags := [4]bool{a > b, a == b, c == 0, true}
		ports := [4]Port{Port(a), Port(b), 1, 2}
		write64(s, 0, Index(&keys, 7)+Index(&flags, true)*10+Index(&ports, Port(c))*100)
		p, q := Pair[uint32, uint8]{Key: 1, Val: 2}, Pair[uint32, uint8]{Key: uint32(a), Val: c}
		Swap(&p, &q)
		k1, k2 := Key{1, 2, 3, 4}, Key{byte(a), byte(b), 0, 0}
		Swap(&k1, &k2)
		x, y := a, b
		Swap(&x, &y)
		write64(s, 8, x-y)
		write32(s, 16, p.Key+uint32(q.Val))
		s[20], s[21] = k1[0], k2[3]
		return TotalSize(Port(a), Port(c)) % 7
	}
	if mode == 6 {
		// Generic named arrays with a method; conversions between type parameters.
		v := Vec[uint64]{a, b, 3, 4}
		w := Vec[uint32]{uint32(a), uint32(b), uint32(c), 1}
		write64(s, 0, v.Dot(Vec[uint64]{1, 2, 3, uint64(c)}))
		write32(s, 8, w.Dot(w))
		write32(s, 12, Widen[uint8, uint32](uint8(a)))
		write64(s, 16, Widen[uint16, uint64](uint16(b)))
		return uint64(Widen[uint64, uint8](a)) % 4
	}
	// Index out of range through a generic method on a full stack.
	var st Stack[uint8]
	for j := uint8(0); j < c%7; j++ {
		st.Push(j)
	}
	s[0] = st.Sum()
	return uint64(st.n)
}
