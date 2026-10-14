// Package ifacevalues is an ordinary-Go oracle for interface values in the
// on-chain subset: dynamic dispatch through value and pointer receivers and
// embedded interfaces, Go's method-set rules, boxing copy semantics, type
// assertions (single and comma-ok, to concrete and interface types), type
// switches (nil, multi-type and interface cases, the per-case variable),
// comparisons (including uncomparable dynamic types, which panic), interfaces
// in structs, slices, arrays and behind pointers, generic types as dynamic
// types, generic functions over interface values, and signed values in
// interfaces. The same source runs natively and through goSVM; tests compare
// output bytes and status (or a panic) for every vector. See
// docs/GO_INTERFACES.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package ifacevalues

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

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// Shapes: value and pointer receivers, embedded interfaces.

type Shape interface {
	Area() uint64
	Perimeter() uint64
}

type Scaler interface {
	Scale(k uint64)
}

// Solid embeds two interfaces and adds a method.
type Solid interface {
	Shape
	Scaler
	Tag() int8
}

type Rect struct {
	W, H uint64
}

func (r Rect) Area() uint64      { return r.W * r.H }
func (r Rect) Perimeter() uint64 { return 2 * (r.W + r.H) }
func (r *Rect) Scale(k uint64)   { r.W = r.W * k; r.H = r.H * k }
func (r *Rect) Tag() int8        { return -int8(r.W & 63) }

// Bump mutates its value receiver: through an interface it changes a copy.
func (r Rect) Bump() uint64 { r.W = r.W + 100; return r.W }

type Sq uint32

func (s Sq) Area() uint64      { return uint64(s) * uint64(s) }
func (s Sq) Perimeter() uint64 { return 4 * uint64(s) }
func (s *Sq) Scale(k uint64)   { *s = Sq(uint64(*s) * k) }
func (s *Sq) Tag() int8        { return int8(*s) }

// Tri has only pointer-receiver methods: only *Tri is a Shape.
type Tri struct {
	A, B, C uint16
}

func (t *Tri) Area() uint64      { return uint64(t.A) * uint64(t.B) / 2 }
func (t *Tri) Perimeter() uint64 { return uint64(t.A) + uint64(t.B) + uint64(t.C) }

type Bumper interface{ Bump() uint64 }

// Pairer has a multiple-result method.
type Pairer interface {
	Pair(k int16) (int64, bool)
}

func (r Rect) Pair(k int16) (int64, bool) { return int64(r.W) * int64(k), r.H > 3 }

// Neg is a named signed type with a method.
type Neg int16

func (n Neg) Pair(k int16) (int64, bool) { return int64(n) - int64(k), n < 0 }

// Generic types as dynamic types: each instance is its own type.

type Getter[T any] interface {
	Get() T
}

type Box[T any] struct {
	V T
}

func (b Box[T]) Get() T            { return b.V }
func (b *Box[T]) Set(v T)          { b.V = v }
func (b Box[T]) Area() uint64      { return 7 }
func (b Box[T]) Perimeter() uint64 { return 8 }

type Setter[T any] interface {
	Set(v T)
}

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(v T)    { s.items = append(s.items, v) }
func (s *Stack[T]) Len() uint64 { return uint64(len(s.items)) }

type Lener interface{ Len() uint64 }

// Holder stores interfaces in fields.
type Holder struct {
	N  uint8
	A  any
	S  Shape
	Ps []Shape
}

// Pt is comparable; Bag holds a slice and is not.
type Pt struct {
	X int32
	Y int32
}

type Bag struct {
	B []byte
	N uint8
}

// Cell is comparable; comparing two Cells compares V's dynamic values.
type Cell struct {
	K uint8
	V any
}

type Empty struct{}

func (Empty) Area() uint64      { return 0 }
func (Empty) Perimeter() uint64 { return 1 }

// Generic functions over interface values.

func SumAreas[T Shape](xs []T) uint64 {
	t := uint64(0)
	for _, x := range xs {
		t = t + x.Area()
	}
	return t
}

func Total(xs []Shape) uint64 {
	t := uint64(0)
	for _, x := range xs {
		t = t + x.Area()*3 + x.Perimeter()
	}
	return t
}

func Describe[T any](v T) uint64 {
	var a any = v
	switch x := a.(type) {
	case nil:
		return 1
	case int8:
		return 2 + uint64(uint8(x))
	case uint8:
		return 300 + uint64(x)
	case Box[int8]:
		return 600 + uint64(uint8(x.V))
	case Box[uint8]:
		return 900 + uint64(x.V)
	case *Rect:
		return 1500 + flag(x == nil)
	case Shape:
		return 1200 + x.Area()
	}
	return 0
}

func Get[T any](g Getter[T]) T { return g.Get() }

func Same[T comparable](a, b T) bool { return a == b }

func pick(n byte, v byte) any {
	switch n % 12 {
	case 0:
		return nil
	case 1:
		return uint64(v)
	case 2:
		return int8(v)
	case 3:
		return Rect{uint64(v & 7), 2}
	case 4:
		return &Rect{uint64(v & 7), 2}
	case 5:
		return []byte{v}
	case 6:
		return Pt{int32(int8(v)), -1}
	case 7:
		return Bag{nil, v}
	case 8:
		var p *Rect
		return p
	case 9:
		return [2]uint8{v, 1}
	case 10:
		return Empty{}
	}
	return Box[int8]{int8(v)}
}

func shapeOf(n byte, v byte) Shape {
	switch n % 6 {
	case 0:
		return Rect{uint64(v & 15), uint64(v >> 4)}
	case 1:
		r := Rect{uint64(v & 7), 3}
		return &r
	case 2:
		s := Sq(v & 31)
		return s
	case 3:
		s := Sq(v & 15)
		return &s
	case 4:
		return &Tri{uint16(v), 4, 5}
	}
	return Box[uint16]{uint16(v)}
}

// dispatch: value and pointer receivers, embedded interfaces, method sets.
func dispatch(w *W, in []byte) {
	shapes := make([]Shape, 0, 8)
	for k := 0; k < 6; k++ {
		shapes = append(shapes, shapeOf(in[1+k], in[8+k]))
	}
	for _, s := range shapes {
		w.u64(s.Area())
		w.u64(s.Perimeter())
	}
	w.u64(Total(shapes))
	// Scale through Scaler where the dynamic type has a pointer method set.
	for _, s := range shapes {
		if sc, ok := s.(Scaler); ok {
			sc.Scale(uint64(in[14] & 3))
			w.u8(1)
		} else {
			w.u8(0)
		}
		w.u64(s.Area())
	}
	// Solid embeds Shape and Scaler.
	r := Rect{uint64(in[15] & 31), uint64(in[16] & 31)}
	var so Solid = &r
	so.Scale(2)
	w.u64(so.Area())
	w.u8(uint64(uint8(so.Tag())))
	w.u64(r.W)        // the pointer in the interface aliases r
	var sh Shape = so // interface to interface
	w.u64(sh.Perimeter())
	sq := Sq(in[17])
	so = &sq
	so.Scale(3)
	w.u64(uint64(sq))
	w.u8(uint64(uint8(so.Tag())))
	// A value-receiver method through *T copies *data.
	var bp Bumper = &r
	w.u64(bp.Bump())
	w.u64(r.W)
	// Multiple results, signed arguments.
	var ps []Pairer
	ps = append(ps, Rect{uint64(in[18]), uint64(in[19] & 7)}, Neg(int16(int8(in[20]))), &r)
	for _, p := range ps {
		v, ok := p.Pair(int16(int8(in[21])))
		w.u64(uint64(v))
		w.u8(flag(ok))
	}
	// Generic dynamic types.
	var g Getter[int8] = Box[int8]{int8(in[22])}
	w.u8(uint64(uint8(g.Get())))
	w.u8(uint64(uint8(Get[int8](g))))
	b := &Box[uint32]{uint32(in[23])}
	var st Setter[uint32] = b
	st.Set(b.V * 3)
	w.u64(uint64(b.V))
	var gs Getter[uint32] = b
	w.u64(uint64(gs.Get()))
	var ln Lener = &Stack[int16]{}
	for k := 0; k < int(in[24]&7); k++ {
		ln.(*Stack[int16]).Push(int16(k))
	}
	w.u64(ln.Len())
	w.u64(SumAreas([]Rect{{1, 2}, {uint64(in[25]), 3}}))
	w.u64(SumAreas([]Shape{Sq(in[26] & 15), Box[uint16]{9}}))
}

// boxing: conversion copies the value; later changes to the original are
// not visible through the interface, pointers alias.
func boxing(w *W, in []byte) {
	r := Rect{uint64(in[1]), uint64(in[2])}
	var a any = r
	var s Shape = r
	r.W = 999
	w.u64(a.(Rect).W)
	w.u64(s.Area())
	b := a // copies the interface value: the same box
	r2 := b.(Rect)
	r2.H = 5
	w.u64(a.(Rect).H)
	var bp Bumper = r
	w.u64(bp.Bump())
	w.u64(bp.Bump()) // the box is unchanged by the value receiver
	arr := [4]uint8{in[3], in[4], in[5], in[6]}
	var aa any = arr
	arr[0] = 77
	got := aa.([4]uint8)
	w.u8(uint64(got[0]))
	w.u8(uint64(arr[0]))
	p := &r
	var ap any = p
	p.H = uint64(in[7])
	w.u64(ap.(*Rect).H)
	var x int16 = int16(int8(in[8]))
	var ax any = x
	x = 5
	w.u64(uint64(ax.(int16)))
	w.u64(uint64(x))
	// A slice boxes its header: the elements stay shared.
	sl := []uint8{in[9], in[10]}
	var as any = sl
	sl[0] = 3
	sl = append(sl, 9)
	w.u8(uint64(as.([]uint8)[0]))
	w.u64(uint64(len(as.([]uint8))))
	// Structs holding interfaces copy the interface words.
	h := Holder{N: in[11], A: uint64(in[12]), S: Sq(in[13] & 15)}
	h2 := h
	h2.A = int8(-1)
	w.u64(h.A.(uint64))
	w.u64(h2.S.Area())
	var hb any = h
	h.N = 0
	w.u8(uint64(hb.(Holder).N))
	// Locals whose pointers enter interfaces move to the heap.
	var keep []any
	for k := 0; k < 3; k++ {
		v := uint64(k) + uint64(in[14])
		keep = append(keep, &v)
	}
	for _, kp := range keep {
		w.u64(*kp.(*uint64))
	}
	e := Empty{}
	var ae any = e
	var se Shape = e
	w.u64(se.Perimeter())
	w.u8(flag(ae == any(Empty{})))
}

// assertions: concrete and interface targets, single and comma-ok; a
// failing single-result assertion panics.
func assertions(w *W, in []byte) {
	a := pick(in[1], in[2])
	u, ok := a.(uint64)
	w.u64(u)
	w.u8(flag(ok))
	i8, ok := a.(int8)
	w.u8(uint64(uint8(i8)))
	w.u8(flag(ok))
	rc, ok := a.(Rect)
	w.u64(rc.W)
	w.u8(flag(ok))
	rp, ok := a.(*Rect)
	w.u8(flag(rp == nil))
	w.u8(flag(ok))
	sh, ok := a.(Shape)
	w.u8(flag(ok))
	w.u8(flag(sh == nil))
	if ok && in[3]&1 == 0 && (rp == nil || !ok) {
		w.u64(sh.Perimeter())
	}
	pa, ok := a.(Pt)
	w.u64(uint64(int64(pa.X)))
	w.u8(flag(ok))
	var e any
	e, ok = a.([2]uint8)
	w.u8(flag(ok))
	w.u8(flag(e == nil))
	var sv any = a
	_, ok = sv.(any)
	w.u8(flag(ok))
	// Single-result assertions: panic unless the dynamic type matches.
	switch in[3] % 6 {
	case 0:
		w.u64(a.(uint64))
	case 1:
		w.u8(uint64(uint8(a.(int8))))
	case 2:
		w.u64(a.(Shape).Area())
	case 3:
		w.u64(a.(Rect).Perimeter())
	case 4:
		w.u8(flag(a.(any) == a))
	default:
		g := a.(Getter[int8])
		w.u8(uint64(uint8(g.Get())))
	}
	w.u8(0xee)
}

// switches: type switches with nil, multi-type, interface and generic
// cases; the default clause runs last whatever its position.
func switches(w *W, in []byte) {
	for k := 0; k < 8; k++ {
		a := pick(in[1+k], in[9+k])
		switch v := a.(type) {
		default:
			w.u8(1)
		case nil:
			w.u8(2)
		case uint64, int8:
			w.u8(3)
			w.u8(flag(v == a))
		case Rect:
			w.u8(4)
			v.W = v.W + 1
			w.u64(v.Area())
		case *Rect:
			w.u8(5)
			w.u8(flag(v == nil))
		case Getter[int8]:
			w.u8(9)
			w.u8(uint64(uint8(v.Get())))
		case Shape:
			w.u8(6)
			w.u64(v.Perimeter())
		case []byte:
			w.u8(7)
			w.u64(uint64(len(v)))
		case Pt, Bag:
			w.u8(8)
		}
		w.u64(Describe(a))
		switch a.(type) {
		case [2]uint8:
			w.u8(10)
		}
	}
	w.u64(Describe(int8(in[17])))
	w.u64(Describe(uint8(in[18])))
	w.u64(Describe(Box[int8]{int8(in[19])}))
	w.u64(Describe(Box[uint8]{in[19]}))
	w.u64(Describe(Sq(in[20] & 7)))
	w.u64(Describe(Box[uint16]{1}))
	// A shadowing clause variable whose address escapes moves to the heap
	// (once per clause execution); break leaves the switch, continue the
	// loop.
	var ptrs []*uint64
	for k := 0; k < 6; k++ {
		x := pick(in[21+k], in[27+k])
		switch x := x.(type) {
		case uint64:
			ptrs = append(ptrs, &x)
		case int8:
			if x < 0 {
				continue
			}
			w.u8(uint64(x))
		default:
			w.u8(0xdd)
			if k > 2 {
				break
			}
			w.u8(0xde)
		}
		w.u8(uint64(k))
	}
	for _, p := range ptrs {
		*p = *p + 1
		w.u64(*p)
	}
}

// compare: interface equality, comparisons with concrete values and nil,
// typed nil pointers, and uncomparable dynamic types (which panic only when
// both sides hold the same uncomparable type).
func compare(w *W, in []byte) {
	a := pick(in[1], in[2])
	b := pick(in[3], in[4])
	w.u8(flag(a == nil))
	w.u8(flag(b != nil))
	var np *Rect
	w.u8(flag(a == np))
	w.u8(flag(a == uint64(in[2])))
	w.u8(flag(a == int8(in[2])))
	w.u8(flag(Rect{uint64(in[2] & 7), 2} == a))
	w.u8(flag(a == Pt{int32(int8(in[2])), -1}))
	w.u8(flag(a == [2]uint8{in[2], 1}))
	w.u8(flag(a == Box[int8]{int8(in[2])}))
	w.u8(flag(a == Empty{}))
	switch a {
	case nil:
		w.u8(1)
	case uint64(3):
		w.u8(2)
	case int8(-1), int8(1):
		w.u8(3)
	case Empty{}:
		w.u8(4)
	default:
		w.u8(5)
	}
	var s Shape = Sq(in[5] & 3)
	var t Shape = Sq(in[6] & 3)
	w.u8(flag(s == t))
	w.u8(flag(Same[any](s, t)))
	w.u8(flag(Same[Shape](s, Rect{1, 1})))
	var arr1, arr2 [3]any
	arr1[0], arr2[0] = uint64(in[7]&1), uint64(in[8]&1)
	arr1[1], arr2[1] = in[9]&1 == 0, in[10]&1 == 0
	w.u8(flag(arr1 == arr2))
	// A comparable struct with an interface field compares K first; equal
	// K values compare V, which panics for the same uncomparable type.
	c1 := Cell{K: in[11] & 1, V: pick(in[12], in[13])}
	c2 := Cell{K: in[14] & 1, V: pick(in[15], in[16])}
	var x, y any = c1, c2
	w.u8(flag(x == y))
	// Uncomparable dynamic types: different types compare false; the same
	// type (a slice, or Bag with its slice field) panics.
	w.u8(flag(a == b))
	w.u8(0xee)
}

// nilCalls: calling a method on a nil interface panics after the arguments
// are evaluated; a value method through a nil *T in an interface panics.
func nilCalls(w *W, in []byte) {
	var s Shape
	if in[1]&1 == 1 {
		s = Sq(in[2])
	}
	if in[1]&2 == 2 {
		var p *Tri
		s = p
	}
	if in[1]&4 == 4 {
		var r *Rect
		s = r
	}
	w.u8(flag(s == nil))
	var sc Scaler
	if in[1]&8 == 8 {
		sc = &Rect{1, 1}
	}
	sc.Scale(touch(w, in[3]))
	w.u64(s.Area())
	w.u8(0xee)
}

func touch(w *W, v byte) uint64 {
	w.u8(uint64(v))
	return uint64(v)
}

// containers: interfaces in struct fields, slices, arrays and behind
// pointers; multiple results converted to interfaces; range assignment.
func containers(w *W, in []byte) {
	h := &Holder{N: in[1]}
	h.S = Rect{uint64(in[2]), 2}
	h.Ps = append(h.Ps, Sq(in[3]&15), h.S, &Tri{4, uint16(in[4]), 1})
	w.u64(Total(h.Ps))
	h.Ps[0] = Box[uint16]{3}
	w.u64(h.Ps[0].Area())
	var arr [4]Shape
	arr[in[5]&3] = Sq(2)
	n := 0
	for _, s := range arr {
		if s != nil {
			w.u64(s.Area())
			n++
		}
	}
	w.u64(uint64(n))
	var vals [2]any
	var pv *any = &vals[1]
	*pv = int32(-7)
	w.u64(uint64(int64(vals[1].(int32))))
	p := &h.A
	*p = Pt{1, int32(int8(in[6]))}
	w.u64(uint64(int64(h.A.(Pt).Y)))
	var x any
	var ok bool
	x, ok = pair(in[7])
	w.u8(flag(ok))
	w.u8(flag(x == uint16(in[7])))
	var sh Shape
	sh, ok = pickShape(in[8])
	w.u8(flag(ok))
	if ok {
		w.u64(sh.Area())
	}
	var y any
	y, ok = x.(uint16)
	w.u8(flag(ok))
	w.u8(flag(y == x))
	var e any
	for _, e = range []int64{-1, int64(int8(in[9]))} {
		w.u64(uint64(e.(int64)))
	}
	for e = range 3 {
		w.u64(uint64(e.(int)))
	}
	list := []any{nil, uint8(1), &Rect{2, 2}, Rect{1, 1}}
	list = append(list, Sq(4))
	list[0] = list[3]
	cnt := uint64(0)
	for _, v := range list {
		if _, isShape := v.(Shape); isShape {
			cnt++
		}
	}
	w.u64(cnt)
	w.u8(flag(list[0] == list[3]))
	grid := [][]Shape{{Sq(1)}, {Rect{2, 2}, Sq(3)}}
	w.u64(Total(grid[1]))
	idx := int(in[10] % 5)
	w.u64(list[idx].(Shape).Area())
}

func pair(v byte) (uint16, bool) { return uint16(v), v > 100 }

func pickShape(v byte) (Rect, bool) { return Rect{uint64(v), 2}, v&1 == 1 }

// generics: generic functions taking interface values, generic types as
// distinct dynamic types, interfaces as type arguments.
func generics(w *W, in []byte) {
	a8 := Box[int8]{int8(in[1])}
	u8 := Box[uint8]{in[1]}
	var x, y any = a8, u8
	w.u8(flag(x == y))
	w.u8(flag(x == any(Box[int8]{int8(in[1])})))
	w.u64(Describe[any](x))
	w.u64(Describe[any](y))
	w.u64(Describe[Shape](Sq(in[2] & 7)))
	w.u64(Describe[any](nil))
	bs := []Box[any]{{V: uint64(in[3])}, {V: nil}}
	for _, b := range bs {
		w.u8(flag(b.Get() == nil))
	}
	var ga Getter[any] = bs[0]
	w.u64(ga.Get().(uint64))
	st := &Stack[Shape]{}
	st.Push(Sq(in[4] & 15))
	st.Push(&Rect{2, uint64(in[5] & 15)})
	w.u64(Total(st.items))
	var l Lener = st
	w.u64(l.Len())
	w.u8(flag(Same(any(st), any(l))))
	ps := []*Box[int16]{{V: int16(int8(in[6]))}, {V: 1}}
	var gs []Getter[int16]
	for _, p := range ps {
		gs = append(gs, p)
	}
	ps[0].V = ps[0].V * 2
	for _, g := range gs {
		w.u64(uint64(g.Get()))
	}
}

// boxes converts n = in[1] | in[2]<<8 values of one kind (in[3] % 6) to
// interfaces, keeping only the last, and records the completed count in
// out[0:2] after each conversion. Each conversion of a non-pointer value of
// nonzero size allocates its Go size rounded up to 8 bytes; pointers, zero-
// size values and interface-to-interface conversions allocate nothing. On
// chain the 32 KiB heap therefore runs out at an exact count; native Go
// never does.
func boxes(out, in []byte) {
	n := int(in[1]) | int(in[2])<<8
	kind := in[3] % 6
	var keep any
	for k := 0; k < n; k++ {
		switch kind {
		case 0:
			keep = uint64(k)
		case 1:
			keep = [3]uint64{uint64(k), 1, 2}
		case 2:
			keep = &in[4]
		case 3:
			keep = Empty{}
		case 4:
			keep = int8(k)
		default:
			var s Shape = Sq(k)
			keep = s
		}
		out[0], out[1] = byte(k+1), byte((k+1)>>8)
	}
	out[2] = byte(flag(keep != nil))
}

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		dispatch(&w, in)
	case 1:
		boxing(&w, in)
	case 2:
		assertions(&w, in)
	case 3:
		switches(&w, in)
	case 4:
		compare(&w, in)
	case 5:
		nilCalls(&w, in)
	case 6:
		containers(&w, in)
	case 7:
		generics(&w, in)
	case 8:
		boxes(out, in)
		return 3
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
