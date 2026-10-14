// Package mixedvalues is an ordinary-Go oracle for programs that combine
// signed integers, generics and invocation-heap allocation in the on-chain
// subset: generic functions and types that make, append, copy and slice
// []T, generic linked structures whose nodes escape to the heap, generic
// locals, parameters and receivers moved to the heap, signed make lengths
// and capacities, signed indexes and slice bounds, and signed division and
// shifts over allocated slices. The same source runs natively and through
// goSVM; tests compare output bytes and status (or a panic) for every
// vector. See docs/GO_ALLOCATION.md, GO_GENERICS.md and GO_INTEGERS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package mixedvalues

// Signed and Integer are type-set constraints.
type Signed interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~int
}

type Integer interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~int | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uint
}

// Tick is a named signed type.
type Tick int16

// P mixes a signed field and a pointer field.
type P struct {
	A int16
	B *int64
}

// Node and Stack are a generic linked structure: every node lives on the heap.
type Node[T any] struct {
	Next *Node[T]
	Val  T
}

type Stack[T any] struct {
	top *Node[T]
	n   int
}

func (s *Stack[T]) Push(v T) {
	s.top = &Node[T]{Next: s.top, Val: v}
	s.n++
}

func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if s.top == nil {
		return zero, false
	}
	v := s.top.Val
	s.top = s.top.Next
	s.n--
	return v, true
}

// Vec is a generic struct with a slice field.
type Vec[T any] struct {
	items []T
}

func (v *Vec[T]) Add(x T) { v.items = append(v.items, x) }

// First returns the address of an element: heap storage, nothing moves.
func (v Vec[T]) First() *T { return &v.items[0] }

// Pair's KeyRef returns the address of its value receiver's field, so the
// receiver of each instance moves to the heap.
type Pair[K, V any] struct {
	K K
	V V
}

func (p Pair[K, V]) KeyRef() *K { return &p.K }

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

func putInt[T Integer](w *W, v T, width int) { w.put(uint64(v), width) }

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// Generic functions that allocate.

func Scale[T Signed](s []T, k T) []T {
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = v * k
	}
	return out
}

func Negatives[T Signed](s []T) []T {
	var out []T
	for _, v := range s {
		if v < 0 {
			out = append(out, v)
		}
	}
	return out
}

func Reverse[T any](s []T) []T {
	out := make([]T, 0, len(s))
	for i := len(s) - 1; i >= 0; i-- {
		out = append(out, s[i])
	}
	return out
}

func Clone[T any](s []T) []T {
	out := make([]T, len(s))
	n := copy(out, s)
	return out[:n]
}

func Sum[T Integer](s []T) T {
	var t T
	for _, v := range s {
		t = t + v
	}
	return t
}

// Repeat makes a slice of signed length n: a negative n panics.
func Repeat[T any](v T, n int8) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// MakeLC panics when l < 0, c < 0 or l > c.
func MakeLC[T any](l, c int8) []T { return make([]T, l, c) }

func At[T any](s []T, k int16) T { return s[k] }

func Window[T any](s []T, lo, hi int8) []T { return s[lo:hi] }

func Window3[T any](s []T, lo, hi, mx int) []T { return s[lo:hi:mx] }

// Box returns its parameter's address: each instance moves the parameter.
func Box[T any](v T) *T { return &v }

// Ptrs keeps every iteration's variable: each is a distinct heap cell.
func Ptrs[T any](s []T) []*T {
	var ps []*T
	for _, v := range s {
		ps = append(ps, &v)
	}
	return ps
}

// Chain links stack-declared nodes that escape through head.
func Chain[T any](vals []T) *Node[T] {
	var head *Node[T]
	for _, v := range vals {
		n := Node[T]{Next: head, Val: v}
		head = &n
	}
	return head
}

func Len[T any](n *Node[T]) int {
	c := 0
	for ; n != nil; n = n.Next {
		c++
	}
	return c
}

// Grow appends n zero values one at a time and hashes every capacity.
func Grow[T any](n int) (int, int, uint64) {
	var s []T
	var zero T
	h := uint64(0)
	for range n {
		s = append(s, zero)
		h = h*31 + uint64(cap(s))
	}
	return len(s), cap(s), h
}

// Div and Shift trap like Go: division by zero, negative shift counts.
func Div[T Signed](s []T, d T) []T {
	out := make([]T, 0, len(s))
	for _, v := range s {
		out = append(out, v/d, v%d)
	}
	return out
}

func Shift[T Signed](s []T, k int8) []T {
	out := []T{}
	for _, v := range s {
		out = append(out, v>>k, v<<k)
	}
	return out
}

func putAll[T Integer](w *W, s []T, width int) {
	putInt(w, len(s), 1)
	for _, v := range s {
		putInt(w, v, width)
	}
}

func int8s(in []byte, from, n int) []int8 {
	s := make([]int8, n)
	for i := range n {
		s[i] = int8(in[from+i])
	}
	return s
}

func widen16(s []int8, k int16) []int16 {
	var out []int16
	for _, v := range s {
		out = append(out, int16(v)*k)
	}
	return out
}

func widen64(s []int8) []int64 {
	out := make([]int64, 0)
	for _, v := range s {
		out = append(out, int64(v)<<40|int64(v))
	}
	return out
}

// generic: generic functions over allocated slices of several widths.
func generic(w *W, in []byte) {
	n := int(in[1] % 24)
	a := int8s(in, 8, n)
	k := int8(in[2])
	b := widen16(a, int16(in[3])-128)
	c := widen64(a)
	ints := make([]int, 0, 2)
	for _, v := range a {
		ints = append(ints, int(v)*1000003)
	}
	putAll(w, Scale(a, k), 1)
	putAll(w, Negatives(b), 2)
	putAll(w, Reverse(c), 8)
	putInt(w, Sum(Clone(ints)), 8)
	putInt(w, Sum(Scale(ints, -7)), 8)
	us := make([]uint, len(a))
	for i := range us {
		us[i] = uint(a[i])
	}
	putInt(w, Sum(us), 8)
	ticks := Scale([]Tick{Tick(k), -3, 32767}, Tick(-2))
	putAll(w, ticks, 2)
	putInt(w, cap(Negatives(a)), 1)
	w.put(flag(Negatives([]int32{1, 2}) == nil), 1)
	ps := []P{{A: int16(k)}, {A: -1, B: Box(int64(in[4]) - 300)}}
	rs := Reverse(ps)
	putInt(w, rs[0].A, 2)
	putInt(w, *rs[0].B, 8)
	w.put(flag(rs[1].B == nil), 1)
}

// stacks: generic linked structures whose nodes are heap allocated.
func stacks(w *W, in []byte) {
	var s Stack[int32]
	var g Stack[[]int8]
	var q Stack[P]
	n := int(in[1] % 12)
	for i := range n {
		s.Push(int32(int8(in[8+i])) * -65537)
		g.Push(int8s(in, 8+i, i%5))
		q.Push(P{A: int16(i), B: Box(int64(i) - 4)})
	}
	putInt(w, s.n, 1)
	for {
		v, ok := s.Pop()
		if !ok {
			break
		}
		putInt(w, v, 4)
	}
	putInt(w, s.n, 1)
	for g.n > 1 {
		v, _ := g.Pop()
		putInt(w, len(v), 1)
		putInt(w, Sum(v), 1)
	}
	total := int64(0)
	for q.top != nil {
		v, _ := q.Pop()
		total = total*3 + int64(v.A) + *v.B
	}
	putInt(w, total, 8)
	head := Chain(int8s(in, 20, n))
	putInt(w, Len(head), 1)
	if head != nil {
		putInt(w, head.Val, 1)
	}
	mixed := Chain([]Tick{-1, 2, -3})
	putInt(w, mixed.Next.Val, 2)
}

// escapes: generic locals, parameters and receivers moved to the heap.
func escapes(w *W, in []byte) {
	vals := int8s(in, 8, int(in[1]%10))
	ptrs := Ptrs(vals)
	for i := range vals {
		vals[i] = 0
	}
	sum := int64(0)
	for i, p := range ptrs {
		*p = *p - int8(i)
		sum = sum*7 + int64(*p)
	}
	putInt(w, sum, 8)
	w.put(flag(len(ptrs) > 1 && ptrs[0] != ptrs[1]), 1)
	b1, b2 := Box(int16(in[2])-200), Box(int16(in[2])-200)
	*b1 = *b1 * -3
	putInt(w, *b1, 2)
	putInt(w, *b2, 2)
	pr := Pair[int32, []int8]{K: int32(int8(in[3])), V: vals}
	kr := pr.KeyRef()
	*kr = *kr + 1
	putInt(w, *kr, 4)
	putInt(w, pr.K, 4)
	var v Vec[Tick]
	for i := range int(in[4] % 9) {
		v.Add(Tick(i) - 4)
	}
	if len(v.items) > 0 {
		f := v.First()
		*f = *f * 5
		putInt(w, v.items[0], 2)
	}
	putInt(w, len(v.items), 1)
	putInt(w, cap(v.items), 1)
}

// growth: Go 1.22 append growth for generic element types of several sizes.
func growth(w *W, in []byte) {
	n := int(in[1])
	var l, c int
	var h uint64
	switch in[2] % 7 {
	case 0:
		l, c, h = Grow[int8](n)
	case 1:
		l, c, h = Grow[int16](n)
	case 2:
		l, c, h = Grow[Tick](n)
	case 3:
		l, c, h = Grow[int32](n)
	case 4:
		l, c, h = Grow[int](n)
	case 5:
		l, c, h = Grow[P](n)
	case 6:
		l, c, h = Grow[[]int8](n)
	}
	putInt(w, l, 2)
	putInt(w, c, 2)
	putInt(w, h, 8)
}

// makes: signed make lengths and capacities abort exactly where Go panics.
func makes(w *W, in []byte) {
	l, c := int8(in[1]), int8(in[2])
	w.put(uint64(in[3]%6), 1)
	switch in[3] % 6 {
	case 0:
		s := Repeat(int64(l), l)
		putInt(w, len(s), 1)
	case 1:
		s := MakeLC[int16](l, c)
		putInt(w, len(s), 1)
		putInt(w, cap(s), 1)
	case 2:
		s := MakeLC[P](l, c)
		putInt(w, cap(s), 1)
	case 3:
		// A length beyond Go's maxAlloc panics (makeslice: len out of range).
		big := int64(in[4]%4) << 61
		s := make([]int32, big)
		putInt(w, len(s), 8)
	case 4:
		n := int(l) - int(c)
		s := make([]Tick, n, 130)
		putInt(w, len(s), 1)
	case 5:
		s := make([]int8, uint8(l))
		s = append(s, MakeLC[int8](c, 5)...)
		putInt(w, len(s), 2)
	}
	w.put(9, 1)
}

// bounds: signed indexes and slice bounds on generic slices.
func bounds(w *W, in []byte) {
	s := int8s(in, 8, 10)
	t := Clone(widen64(s))
	k := int16(int8(in[1]))
	putInt(w, At(t, k%12), 8)
	from := int8(in[2]) % 8
	win := Window(s, from, from+int8(in[3])%5)
	putInt(w, len(win), 1)
	putInt(w, cap(win), 1)
	lo := int(int8(in[4])) % 4
	hi := lo + int(int8(in[5]))%5
	mx := hi + int(int8(in[6]))%4
	u := Window3(t, lo, hi, mx)
	putInt(w, len(u), 1)
	putInt(w, cap(u), 1)
	u = append(u, -1)
	putInt(w, t[hi%10], 8)
	j := int32(int8(in[7]))
	putInt(w, s[j%11], 1)
}

// arith: signed division and shifts inside generic code over allocated slices.
func arith(w *W, in []byte) {
	a := int8s(in, 8, int(in[1]%6))
	a = append(a, -128, 127)
	d := int8(in[2]) % 4
	putAll(w, Div(a, d), 1)
	b := widen16(a, 1)
	b = append(b, -32768)
	putAll(w, Div(b, int16(d)-2), 2)
	putAll(w, Shift(widen64(a), int8(in[3])%70), 8)
	ints := []int{-1 << 63, -7, 7}
	putAll(w, Div(ints, int(d)), 8)
}

// copies: copy with signed offsets and overlapping generic slices.
func copies(w *W, in []byte) {
	s := Clone(widen16(int8s(in, 8, 8), -3))
	off := int(int8(in[1])) % 10
	n := copy(s[off:], s)
	putInt(w, n, 1)
	putAll(w, s, 2)
	m := copy(s, s[2:])
	putInt(w, m+copy(s[:0], s), 1)
	putAll(w, s, 2)
	r := Reverse(s[:off%8])
	putAll(w, r, 2)
}

// Run dispatches on in[0] and writes results into out from offset 0.
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		generic(&w, in)
	case 1:
		stacks(&w, in)
	case 2:
		escapes(&w, in)
	case 3:
		growth(&w, in)
	case 4:
		makes(&w, in)
	case 5:
		bounds(&w, in)
	case 6:
		arith(&w, in)
	case 7:
		copies(&w, in)
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
