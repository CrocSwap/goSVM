// Package mapvalues is an ordinary-Go oracle for maps in the on-chain subset:
// random operation sequences (insert, overwrite, lookup, comma-ok lookup,
// delete, ++, len, clear) over keys of every admitted kind (integers of each
// width, bool, strings including "", arrays, structs, pointers, interfaces,
// named types), elements of every kind (structs, slices, strings, interfaces,
// arrays, pointers, nested maps, struct{}), map aliasing through assignment,
// fields, slices, arrays, pointers and interfaces, nil maps (reads, len,
// delete and clear work; writes panic), interface keys whose dynamic type is
// not comparable (Go panics), generic code over maps, large maps that grow,
// and Go's evaluation order. Iteration is never used: Go randomizes its
// order, so every result here is order-independent. The same source runs
// natively and through goSVM; tests compare output bytes and status (or a
// panic) for every vector. See docs/GO_MAPS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package mapvalues

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

// str writes len(s) as one byte, then at most 24 of its bytes.
func (w *W) str(s string) {
	w.u8(uint64(len(s)))
	for j := 0; j < len(s) && j < 24; j++ {
		w.out[w.o] = s[j]
		w.o = w.o + 1
	}
}

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// Obs folds observations into a checksum (FNV-1a over 64-bit words).
type Obs struct {
	sum uint64
	n   uint64
}

func (o *Obs) add(v uint64) {
	if o.n == 0 {
		o.sum = 14695981039346656037
	}
	o.sum = (o.sum ^ v) * 1099511628211
	o.n++
}

func (o *Obs) put(w *W) {
	w.u64(o.sum)
	w.u64(o.n)
}

// Rng is xorshift64*.
type Rng struct{ s uint64 }

func (r *Rng) next() uint64 {
	x := r.s
	x = x ^ (x >> 12)
	x = x ^ (x << 25)
	x = x ^ (x >> 27)
	r.s = x
	return x * 2685821657736338717
}

func seed(in []byte, at int) Rng {
	s := uint64(0x9E3779B97F4A7C15)
	for j := at; j < at+8; j++ {
		s = (s ^ uint64(in[j])) * 1099511628211
	}
	if s == 0 {
		s = 1
	}
	return Rng{s}
}

// Key types.

type P struct{ X, Y int16 }

// K is a struct key with a string and an array field.
type K struct {
	A uint8
	S string
	B [2]int32
}

// IK is a struct key with an interface field.
type IK struct {
	V any
	N int8
}

type Name string

type Shape interface{ Area() uint64 }

type Sq uint16

func (q Sq) Area() uint64 { return uint64(q) * uint64(q) }

type Rect struct{ W, H uint8 }

func (r *Rect) Area() uint64 { return uint64(r.W) * uint64(r.H) }

const alphabet = "abcabcdeffedcbaxyz"

// sub is a substring of alphabet (no allocation); x == 0 is "".
func sub(x uint64) string {
	lo := int(x % 7)
	n := int(x/7) % 9
	return alphabet[lo : lo+n]
}

// word is a non-empty key of 1 to 8 bytes, distinct for x below 56.
func word(x uint64) string {
	lo := int(x % 7)
	return alphabet[lo : lo+1+int(x/7)%8]
}

// Key makers: key(x) maps a small integer to a key; distinct x give distinct
// keys except where a type has fewer values (bool) or the maker folds
// values on purpose.

type U8s struct{}

func (U8s) key(x uint64) uint8 { return uint8(x * 37) }

type U16s struct{}

func (U16s) key(x uint64) uint16 { return uint16(x*4099 + 1) }

type U32s struct{}

func (U32s) key(x uint64) uint32 { return uint32(x * 2654435761) }

type U64s struct{}

func (U64s) key(x uint64) uint64 { return x << 40 }

type I8s struct{}

func (I8s) key(x uint64) int8 { return int8(int64(x) - 30) }

type I16s struct{}

func (I16s) key(x uint64) int16 { return int16(int64(x)*-1031 + 5) }

type I32s struct{}

func (I32s) key(x uint64) int32 { return -int32(x) }

type I64s struct{}

func (I64s) key(x uint64) int64 { return int64(x)*-7919 - 1 }

type Ints struct{}

func (Ints) key(x uint64) int { return int(x) - 20 }

type Uints struct{}

func (Uints) key(x uint64) uint { return uint(x) * 3 }

type Bools struct{}

func (Bools) key(x uint64) bool { return x%3 == 0 }

type Strs struct{}

func (Strs) key(x uint64) string { return sub(x) }

type Names struct{}

func (Names) key(x uint64) Name { return Name(sub(x + 3)) }

// Cats builds keys by concatenation (heap strings equal to other strings).
type Cats struct{}

func (Cats) key(x uint64) string { return sub(x%5) + sub(x/5) }

type Arrs struct{}

func (Arrs) key(x uint64) [3]byte { return [3]byte{byte(x), byte(x >> 2), 7} }

type SArrs struct{}

func (SArrs) key(x uint64) [2]string { return [2]string{sub(x % 4), sub(x / 4)} }

type Structs struct{}

func (Structs) key(x uint64) K {
	return K{A: uint8(x % 3), S: sub(x / 3), B: [2]int32{int32(x % 2), -1}}
}

type Points struct{}

func (Points) key(x uint64) P { return P{int16(x % 5), -int16(x / 5)} }

// Ptrs keys are pointers into a pool: equal only when identical.
type Ptrs struct{ pool []P }

func (p Ptrs) key(x uint64) *P { return &p.pool[x] }

// Ifaces mixes dynamic types; equal values of different types differ.
type Ifaces struct{}

func (Ifaces) key(x uint64) any {
	switch x % 8 {
	case 0:
		return uint8(x / 8)
	case 1:
		return int(x / 8)
	case 2:
		return sub(x / 8)
	case 3:
		return Name(sub(x / 8))
	case 4:
		return P{int16(x / 8), 1}
	case 5:
		return [2]int16{int16(x / 8), 2}
	case 6:
		if x/8 == 0 {
			return nil
		}
		return int8(x / 8)
	}
	return Sq(x / 8)
}

type Shapes struct{ pool []Rect }

func (s Shapes) key(x uint64) Shape {
	if x%2 == 0 {
		return Sq(x / 2)
	}
	return &s.pool[x/2]
}

type IKs struct{}

func (IKs) key(x uint64) IK {
	var v any = sub(x / 2)
	if x%2 == 1 {
		v = uint16(x / 2)
	}
	return IK{V: v, N: int8(x % 3)}
}

// Maker is the key maker constraint.
type Maker[T comparable] interface{ key(x uint64) T }

// script runs ops random operations on a map[T]uint64 over keys key(x) for x
// below space, then looks up every key. Every observation is folded into the
// checksum.
func script[T comparable, M Maker[T]](w *W, r *Rng, mk M, ops int, space uint64) {
	m := make(map[T]uint64)
	var o Obs
	for j := 0; j < ops; j++ {
		op := r.next() % 20
		k := mk.key(r.next() % space)
		switch {
		case op < 6:
			m[k] = r.next() % 1000
		case op < 9:
			o.add(m[k])
		case op < 12:
			v, ok := m[k]
			o.add(v)
			o.add(flag(ok))
		case op < 15:
			delete(m, k)
		case op < 17:
			m[k]++
		case op < 19:
			o.add(uint64(len(m)))
		default:
			if r.next()%4 == 0 {
				clear(m)
			}
		}
	}
	for x := uint64(0); x < space; x++ {
		v, ok := m[mk.key(x)]
		o.add(v)
		o.add(flag(ok))
	}
	o.add(uint64(len(m)))
	o.put(w)
	w.u64(uint64(len(m)))
}

// Mode 0: random scripts over a key kind (in[1]).
func scripts(w *W, in []byte) {
	r := seed(in, 8)
	space := 1 + uint64(in[2])%60
	ops := 50 + int(in[3])%200
	switch in[1] % 20 {
	case 0:
		script(w, &r, U8s{}, ops, space)
	case 1:
		script(w, &r, U16s{}, ops, space)
	case 2:
		script(w, &r, U32s{}, ops, space)
	case 3:
		script(w, &r, U64s{}, ops, space)
	case 4:
		script(w, &r, I8s{}, ops, space)
	case 5:
		script(w, &r, I16s{}, ops, space)
	case 6:
		script(w, &r, I32s{}, ops, space)
	case 7:
		script(w, &r, I64s{}, ops, space)
	case 8:
		script(w, &r, Ints{}, ops, space)
	case 9:
		script(w, &r, Uints{}, ops, space)
	case 10:
		script(w, &r, Bools{}, ops, space)
	case 11:
		script(w, &r, Strs{}, ops, space)
	case 12:
		script(w, &r, Names{}, ops, space)
	case 13:
		script(w, &r, Cats{}, ops, space)
	case 14:
		script(w, &r, Arrs{}, ops, space)
	case 15:
		script(w, &r, SArrs{}, ops, space)
	case 16:
		script(w, &r, Structs{}, ops, space)
	case 17:
		script(w, &r, Ptrs{pool: make([]P, space)}, ops, space)
	case 18:
		script(w, &r, Ifaces{}, ops, space)
	default:
		if in[4]%2 == 0 {
			script(w, &r, Shapes{pool: make([]Rect, space)}, ops, space)
		} else {
			script(w, &r, IKs{}, ops, space)
		}
	}
	script(w, &r, Points{}, ops/4, space)
}

// Element types.

// Rec is an element with a string, a slice and an array.
type Rec struct {
	Name string
	Data []byte
	Tag  [2]uint8
}

// vscript runs random operations on a map[uint16]V whose elements are
// built by mk.val and observed by mk.obs.
type Valuer[V any] interface {
	val(x uint64) V
	obs(v V) uint64
}

func vscript[V any, M Valuer[V]](w *W, r *Rng, mk M, ops int, space uint64) {
	m := map[uint16]V{}
	var o Obs
	for j := 0; j < ops; j++ {
		op := r.next() % 10
		k := uint16(r.next() % space)
		switch {
		case op < 4:
			m[k] = mk.val(r.next() % 50)
		case op < 6:
			o.add(mk.obs(m[k]))
		case op < 8:
			v, ok := m[k]
			o.add(mk.obs(v))
			o.add(flag(ok))
		case op < 9:
			delete(m, k)
		default:
			o.add(uint64(len(m)))
		}
	}
	for x := uint64(0); x < space; x++ {
		v, ok := m[uint16(x)]
		o.add(mk.obs(v))
		o.add(flag(ok))
	}
	o.put(w)
	w.u64(uint64(len(m)))
}

type Recs struct{}

func (Recs) val(x uint64) Rec {
	return Rec{Name: sub(x), Data: []byte(sub(x + 1)), Tag: [2]uint8{uint8(x), 9}}
}

func (Recs) obs(v Rec) uint64 {
	return uint64(len(v.Name))*1000 + uint64(len(v.Data))*10 + uint64(v.Tag[0]) + flag(v.Data == nil)<<20
}

type Slices struct{}

func (Slices) val(x uint64) []byte {
	if x%5 == 0 {
		return nil
	}
	return []byte(sub(x))
}

func (Slices) obs(v []byte) uint64 {
	n := uint64(len(v)) + flag(v == nil)<<8
	if len(v) > 0 {
		n = n + uint64(v[0])<<16
	}
	return n
}

type Strings struct{}

func (Strings) val(x uint64) string { return sub(x) }
func (Strings) obs(v string) uint64 { return uint64(len(v)) }

type Anys struct{}

func (Anys) val(x uint64) any {
	switch x % 4 {
	case 0:
		return nil
	case 1:
		return int8(x)
	case 2:
		return sub(x)
	}
	return P{1, int16(x)}
}

func (Anys) obs(v any) uint64 {
	switch t := v.(type) {
	case nil:
		return 1
	case int8:
		return 2 + uint64(t)<<8
	case string:
		return 3 + uint64(len(t))<<8
	case P:
		return 4 + uint64(t.Y)<<8
	}
	return 5
}

type Arrays struct{}

func (Arrays) val(x uint64) [4]int8 { return [4]int8{int8(x), -int8(x), 3, int8(x * 7)} }
func (Arrays) obs(v [4]int8) uint64 {
	return uint64(uint8(v[0])) | uint64(uint8(v[1]))<<8 | uint64(uint8(v[3]))<<16
}

type Ptrs2 struct{ pool []P }

func (p Ptrs2) val(x uint64) *P {
	if x%7 == 0 {
		return nil
	}
	return &p.pool[x%uint64(len(p.pool))]
}

func (p Ptrs2) obs(v *P) uint64 {
	if v == nil {
		return 77
	}
	v.X++
	return uint64(uint16(v.X))
}

type Inner struct{}

func (Inner) val(x uint64) map[uint8]uint8 {
	if x%3 == 0 {
		return nil
	}
	return map[uint8]uint8{uint8(x): uint8(x * 2), 0: 1}
}

func (Inner) obs(v map[uint8]uint8) uint64 {
	if v == nil {
		return 99
	}
	v[0]++
	return uint64(len(v))*256 + uint64(v[0])
}

type Bits struct{}

func (Bits) val(x uint64) bool { return x%2 == 1 }
func (Bits) obs(v bool) uint64 { return flag(v) + 5 }

type Units struct{}

func (Units) val(x uint64) struct{} { return struct{}{} }
func (Units) obs(v struct{}) uint64 { return 3 }

// Mode 1: random scripts over element kinds (in[1]), and element updates
// in place.
func values(w *W, in []byte) {
	r := seed(in, 8)
	space := 1 + uint64(in[2])%40
	ops := 30 + int(in[3])%120
	switch in[1] % 12 {
	case 0:
		vscript(w, &r, Recs{}, ops, space)
	case 1:
		vscript(w, &r, Slices{}, ops, space)
	case 2:
		vscript(w, &r, Strings{}, ops, space)
	case 3:
		vscript(w, &r, Anys{}, ops, space)
	case 4:
		vscript(w, &r, Arrays{}, ops, space)
	case 5:
		vscript(w, &r, Ptrs2{pool: make([]P, 5)}, ops, space)
	case 6:
		vscript(w, &r, Inner{}, ops, space)
	case 7:
		vscript(w, &r, Bits{}, ops, space)
	case 8:
		vscript(w, &r, Units{}, ops, space)
	case 9:
		// Appending to slice elements; the element shares its array.
		m := map[uint8][]uint16{}
		for j := 0; j < ops; j++ {
			k := uint8(r.next() % space)
			m[k] = append(m[k], uint16(j))
			if j%5 == 0 {
				m[k][0] = m[k][0] + 1000
			}
		}
		var o Obs
		for x := uint64(0); x < space; x++ {
			s := m[uint8(x)]
			o.add(uint64(len(s)))
			o.add(uint64(cap(s)))
			if len(s) > 0 {
				o.add(uint64(s[0]))
				o.add(uint64(s[len(s)-1]))
			}
		}
		o.put(w)
	case 10:
		// += on string elements, ++ and -- on integer elements.
		m := map[uint8]string{}
		c := map[uint8]int8{}
		u := map[uint8]uint8{}
		for j := 0; j < ops; j++ {
			k := uint8(r.next() % space)
			m[k] += sub(r.next() % 20)
			c[k]--
			u[k]++
			u[k]++
		}
		var o Obs
		for x := uint64(0); x < space; x++ {
			o.add(uint64(len(m[uint8(x)])))
			o.add(uint64(c[uint8(x)]))
			o.add(uint64(u[uint8(x)]))
		}
		o.put(w)
		w.str(m[0])
	default:
		// Nested maps: m[a][b] updates.
		m := map[uint8]map[uint8]int16{}
		for j := 0; j < ops; j++ {
			a, b := uint8(r.next()%space), uint8(r.next()%5)
			if m[a] == nil {
				m[a] = map[uint8]int16{}
			}
			m[a][b] = m[a][b] - int16(j)
			if j%7 == 0 {
				delete(m[a], b)
			}
			if j%11 == 0 {
				delete(m, a)
			}
		}
		var o Obs
		for x := uint64(0); x < space; x++ {
			in := m[uint8(x)]
			o.add(uint64(len(in)))
			for b := uint8(0); b < 5; b++ {
				v, ok := in[b]
				o.add(uint64(v))
				o.add(flag(ok))
			}
		}
		o.put(w)
	}
}

// Holder keeps a map in a field.
type Holder struct {
	M    map[string]int
	Name string
}

func (h Holder) put(k string, v int) { h.M[k] = v }

func fill(m map[string]int, k string) { m[k] = len(k) }

func fresh() map[string]int { return map[string]int{"fresh": 1} }

// Mode 2: aliasing and nil maps. in[1] selects a scenario; nil-map writes
// panic.
func aliasing(w *W, in []byte) {
	switch in[1] % 12 {
	case 0:
		// Assignment copies the reference.
		a := map[string]int{"x": 1}
		b := a
		b["y"] = 2
		delete(a, "x")
		w.u64(uint64(len(a)))
		w.u64(uint64(b["y"]))
		w.u64(uint64(a["y"]))
		_, ok := b["x"]
		w.u8(flag(ok))
	case 1:
		// Fields, value receivers and parameters share the table.
		h := Holder{M: map[string]int{}}
		h.put("a", 1)
		h2 := h
		h2.put("b", 2)
		fill(h.M, "ccc")
		w.u64(uint64(len(h.M)))
		w.u64(uint64(h.M["b"] + h2.M["ccc"]))
		h2.M = nil
		w.u8(flag(h.M == nil))
		w.u8(flag(h2.M == nil))
	case 2:
		// Slices and arrays of maps.
		ms := []map[uint8]bool{{}, nil, fresh2()}
		ms[0][1] = true
		ms2 := ms
		ms2[2][9] = true
		var arr [3]map[uint8]bool
		arr[0] = ms[0]
		cp := arr
		cp[0][5] = true
		w.u64(uint64(len(ms[0])))
		w.u64(uint64(len(ms[2])))
		w.u8(flag(arr[0][5]))
		w.u8(flag(ms[1] == nil && arr[1] == nil))
	case 3:
		// Pointers to maps and maps in interfaces.
		m := map[int]int{}
		p := &m
		(*p)[1] = 10
		*p = map[int]int{2: 20}
		var a any = m
		n := a.(map[int]int)
		n[3] = 30
		w.u64(uint64(len(m)))
		w.u64(uint64(m[3]))
		var b any = map[int]int(nil)
		w.u8(flag(b != nil))
		if q, ok := b.(map[int]int); ok {
			w.u8(flag(q == nil))
			w.u64(uint64(len(q)))
		}
		switch t := a.(type) {
		case map[string]int:
			w.u8(1)
		case map[int]int:
			w.u8(2 + uint64(len(t)))
		}
	case 4:
		// Nil map reads, len, delete and clear do not panic.
		var m map[string]uint64
		v, ok := m["a"]
		delete(m, "a")
		clear(m)
		w.u64(v + m["b"])
		w.u8(flag(ok))
		w.u64(uint64(len(m)))
		var h Holder
		w.u64(uint64(h.M["x"]))
		var ms []map[int]int
		ms = append(ms, nil)
		w.u64(uint64(ms[0][4]))
	case 5:
		var m map[string]int
		w.u8(1)
		m["a"] = 1 // panics
		w.u8(2)
	case 6:
		var m map[uint8]int8
		w.u8(1)
		m[3]++ // panics
	case 7:
		var m map[uint8]string
		w.u8(1)
		m[3] += "x" // panics
	case 8:
		m := map[uint8]map[uint8]int{}
		m[1][2] = 3 // m[1] is nil: panics
	case 9:
		var h Holder
		h.put("a", 1) // panics
	case 10:
		var m map[int]int
		var x int
		x, m[1] = 1, 2 // panics after x is assigned
		w.u64(uint64(x))
	default:
		// Clearing through one reference is visible through all.
		a := map[int]int{1: 1, 2: 2}
		b := a
		clear(b)
		a[3] = 3
		w.u64(uint64(len(a)))
		w.u64(uint64(len(b)))
		w.u64(uint64(b[3]))
	}
}

func fresh2() map[uint8]bool { return map[uint8]bool{} }

// Mode 3: interface keys whose dynamic type is not comparable panic in every
// operation that hashes the key, even on nil or empty maps (in[1]).
func unhashable(w *W, in []byte) {
	var bad any = []int{1}
	m := map[any]int{1: 1, "a": 2}
	var nilm map[any]int
	w.u8(1)
	switch in[1] % 12 {
	case 0:
		w.u64(uint64(m[bad]))
	case 1:
		w.u64(uint64(nilm[bad]))
	case 2:
		delete(nilm, bad)
	case 3:
		delete(m, bad)
	case 4:
		m[bad] = 3
	case 5:
		empty := map[any]bool{}
		_, ok := empty[bad]
		w.u8(flag(ok))
	case 6:
		k := map[IK]int{}
		k[IK{V: map[int]int{}}] = 1
	case 7:
		a := map[[2]any]int{}
		w.u64(uint64(a[[2]any{1, bad}]))
	case 8:
		// Comparable dynamic types work, including nil and pointers.
		p := &P{1, 2}
		m[p] = 4
		m[nil] = 5
		m[P{1, 2}] = 6
		w.u64(uint64(m[p] + m[nil] + m[P{1, 2}] + m[1]))
		w.u64(uint64(len(m)))
	case 9:
		var s Shape = &Rect{1, 2}
		ms := map[Shape]int{s: 1}
		w.u64(uint64(ms[s]))
		w.u64(uint64(ms[&Rect{1, 2}])) // a different pointer
	case 10:
		x := map[any]int{}
		x[Holder{}.Name] = 1 // a string key
		x[Rec{}.Tag] = 2     // an array key
		w.u64(uint64(x[""] + x[[2]uint8{}]))
		var f any = Holder{}
		w.u64(uint64(x[f])) // a struct with a map field: panics
	default:
		// Uncomparable values as map elements are fine.
		e := map[int]any{1: []int{1}, 2: map[int]int{}}
		w.u64(uint64(len(e)))
	}
	w.u8(2)
}

// Generic code over maps.

// Set is a generic set type.
type Set[T comparable] map[T]struct{}

func (s Set[T]) Add(v T)           { s[v] = struct{}{} }
func (s Set[T]) Has(v T) bool      { _, ok := s[v]; return ok }
func (s Set[T]) Remove(v T)        { delete(s, v) }
func (s Set[T]) Len() int          { return len(s) }
func NewSet[T comparable]() Set[T] { return make(Set[T]) }

// Counter counts keys in a map field allocated on first use.
type Counter[T comparable] struct {
	m     map[T]int
	total int
}

func (c *Counter[T]) Inc(k T) int {
	if c.m == nil {
		c.m = make(map[T]int, 4)
	}
	c.m[k]++
	c.total++
	return c.m[k]
}

func Get[K comparable, V any](m map[K]V, k K, def V) V {
	if v, ok := m[k]; ok {
		return v
	}
	return def
}

// Put works on any map type, including named ones; it allocates a nil map.
func Put[M ~map[K]V, K comparable, V any](m M, k K, v V) M {
	if m == nil {
		m = make(M)
	}
	m[k] = v
	return m
}

func Size[M ~map[K]V, K comparable, V any](m M) int { return len(m) }

// Invert builds the inverse of a slice of pairs.
type Pair[A, B comparable] struct {
	L A
	R B
}

func Invert[A, B comparable](ps []Pair[A, B]) map[B]A {
	out := make(map[B]A, len(ps))
	for _, p := range ps {
		out[p.R] = p.L
	}
	return out
}

type Scores map[string]int16

// Mode 4: generics.
func generics(w *W, in []byte) {
	r := seed(in, 8)
	s := NewSet[string]()
	n := NewSet[int8]()
	var c Counter[P]
	var d Counter[any]
	for j := 0; j < 40+int(in[2]%40); j++ {
		x := r.next()
		s.Add(sub(x % 30))
		n.Add(int8(x))
		if x%3 == 0 {
			s.Remove(sub(x % 7))
		}
		c.Inc(P{int16(x % 4), 0})
		d.Inc(sub(x % 3))
		d.Inc(int(x % 2))
	}
	w.u64(uint64(s.Len()))
	w.u64(uint64(n.Len()))
	w.u8(flag(s.Has("ab")))
	w.u8(flag(n.Has(-1)))
	w.u64(uint64(c.Inc(P{1, 0})))
	w.u64(uint64(c.total))
	w.u64(uint64(d.Inc("a")) + uint64(d.Inc(1))<<16)
	w.u64(uint64(len(d.m)))
	var sc Scores
	sc = Put(sc, "a", -3)
	sc = Put(sc, "b", int16(in[3]))
	w.u64(uint64(Get(sc, "a", 7)))
	w.u64(uint64(Get(sc, "zz", 7)))
	w.u64(uint64(Size(sc)))
	plain := Put(map[[2]uint8]bool(nil), [2]uint8{1, 2}, true)
	w.u8(flag(Get(plain, [2]uint8{1, 2}, false)))
	inv := Invert([]Pair[uint8, string]{{1, "a"}, {2, "b"}, {3, "a"}})
	w.u64(uint64(len(inv)))
	w.u64(uint64(inv["a"]))
	var x any = s
	if t, ok := x.(Set[string]); ok {
		t.Add("new")
	}
	w.u8(flag(s.Has("new")))
}

// Mode 5: large maps that grow many times. n keys (in[1..2]) are inserted,
// a third deleted, then all looked up.
func large(w *W, in []byte) {
	n := uint32(in[1]) | uint32(in[2])<<8
	m := make(map[uint32]uint32)
	for j := uint32(0); j < n; j++ {
		m[j*2654435761] = j
	}
	for j := uint32(0); j < n; j = j + 3 {
		delete(m, j*2654435761)
	}
	var o Obs
	for j := uint32(0); j < n+5; j++ {
		v, ok := m[j*2654435761]
		o.add(uint64(v))
		o.add(flag(ok))
	}
	o.put(w)
	w.u64(uint64(len(m)))
	// String keys: overwrite and reinsert after deletes.
	t := map[string]uint32{}
	for j := uint32(0); j < n/4; j++ {
		t[sub(uint64(j))+sub(uint64(j/63))] = j
	}
	w.u64(uint64(len(t)))
	w.u64(uint64(t["abca"]))
}

// Mode 6: heap use. n (in[1..3]) distinct keys are inserted into a map of
// kind in[4] (kind 1 with the make hint in[5..6]); native Go completes, on
// chain the heap budget decides.
func heapUse(out, in []byte) {
	n := uint64(in[1]) | uint64(in[2])<<8 | uint64(in[3])<<16
	var length int
	switch in[4] {
	case 0:
		m := make(map[uint64]uint64)
		for j := uint64(0); j < n; j++ {
			m[j] = j
		}
		length = len(m)
	case 1:
		m := make(map[uint64]uint64, int(in[5])|int(in[6])<<8)
		for j := uint64(0); j < n; j++ {
			m[j] = j
		}
		length = len(m)
	case 2:
		m := map[string]struct{}{}
		for j := uint64(0); j < n; j++ {
			m[sub(j)+alphabet[0:int(j/63)%16]] = struct{}{}
		}
		length = len(m)
	case 3:
		// Inserting and deleting the same keys never grows past the
		// live count.
		m := map[uint32]uint8{}
		for j := uint64(0); j < n; j++ {
			m[uint32(j%5)] = 1
			delete(m, uint32(j%5))
		}
		length = len(m)
	default:
		// Many small maps: each make allocates its header.
		var keep []map[uint8]uint8
		for j := uint64(0); j < n; j++ {
			keep = append(keep, make(map[uint8]uint8))
		}
		length = len(keep)
	}
	out[0] = byte(n)
	out[1] = byte(n >> 8)
	out[2] = byte(length)
	out[3] = byte(length >> 8)
}

// log records evaluation order.
type Log struct{ seq []byte }

func (l *Log) at(tag byte, v int) int {
	l.seq = append(l.seq, tag)
	return v
}

// Mode 7: forms and evaluation order.
func forms(w *W, in []byte) {
	// Wrapping ++ and --.
	u := map[bool]uint8{true: 255}
	u[true]++
	u[false]--
	i := map[int8]int8{0: 127}
	i[0]++
	i[1]--
	w.u64(uint64(u[true]) | uint64(u[false])<<8 | uint64(uint8(i[0]))<<16 | uint64(uint8(i[1]))<<24)
	// Comma-ok forms.
	m := map[string]int{"a": 1}
	var v int
	var ok bool
	v, ok = m["a"]
	w.u64(uint64(v) + flag(ok)<<8)
	var v2, ok2 = m["b"]
	w.u64(uint64(v2) + flag(ok2)<<8)
	var a any
	a, ok = m["a"]
	w.u8(flag(a == 1 && ok))
	_, ok = m["zz"]
	w.u8(flag(ok))
	if v, ok := m[string(in[1:2])]; ok || v == 0 {
		w.u8(2)
	}
	// Literals: struct elements with elided types, nested maps, keyed struct
	// values and duplicate computed keys (the last wins).
	pts := map[P][]P{{1, 2}: {{3, 4}}, {5, 6}: nil}
	w.u64(uint64(len(pts)) + uint64(len(pts[P{1, 2}]))<<8 + uint64(pts[P{1, 2}][0].Y)<<16)
	nest := map[string]map[string]int{"x": {"y": 1}, "z": {}}
	nest["z"]["w"] = 2
	w.u64(uint64(nest["x"]["y"] + nest["z"]["w"] + len(nest)))
	k1, k2 := int(in[2]%3), int(in[3]%3)
	dup := map[int]string{k1: "first", k2: "second"}
	w.u64(uint64(len(dup)))
	w.str(dup[k1])
	ptrs := map[string]*P{"p": {X: 9}}
	ptrs["p"].Y = 3
	w.u64(uint64(ptrs["p"].X + ptrs["p"].Y))
	// Hints: negative and huge hints are ignored, as Go does.
	neg := -int(in[4]) - 1
	h1 := make(map[uint8]uint8, neg)
	h1[1] = 1
	huge := uint64(1)<<63 + uint64(in[5])
	h2 := make(map[uint8]uint8, huge)
	h2[2] = 2
	h3 := make(map[uint8]uint8, int8(in[6]))
	h3[3] = 3
	w.u64(uint64(len(h1) + len(h2) + len(h3)))
	// clear then reuse.
	c := map[int]int{1: 1, 2: 2, 3: 3}
	clear(c)
	c[4] = 4
	w.u64(uint64(len(c) + c[4] + c[1]))
	// Evaluation order: the map and key operands, then the value, then the
	// insertion; a value call that writes the same key is overwritten.
	var l Log
	e := map[int]int{}
	e[l.at('k', 1)] = l.at('v', 2)
	e[l.at('a', 3)], e[l.at('b', 4)] = l.at('c', 5), l.at('d', 6)
	s := map[string]string{"a": "a"}
	s["a"] += setA(s)
	w.str(string(l.seq))
	w.u64(uint64(e[1] + e[3] + e[4]))
	w.str(s["a"])
	// Swaps through a map.
	sw := map[int]int{1: 10, 2: 20}
	sw[1], sw[2] = sw[2], sw[1]
	w.u64(uint64(sw[1]) + uint64(sw[2])<<16)
	// Keys and elements of every integer width, in one literal each.
	w8 := map[uint8]int64{0: -1, 255: 1}
	w64 := map[int64]uint8{-1 << 63: 1, 1<<63 - 1: 2}
	w.u64(uint64(w8[255]) + uint64(w64[-1<<63]) + uint64(w64[1<<63-1]))
}

func setA(s map[string]string) string {
	s["a"] = "X"
	return "y"
}

// Mode 8: compute cost probe: n operations of kind in[1] on a map of size
// in[2..3] (used on SBF to measure CU per operation; n in in[4..5]).
func probe(w *W, in []byte) {
	size := uint64(in[2]) | uint64(in[3])<<8
	n := uint64(in[4]) | uint64(in[5])<<8
	sum := uint64(0)
	switch in[1] {
	case 0:
		// Inserting new keys into a presized map.
		m := make(map[uint64]uint64, int(size+n))
		for j := uint64(0); j < size; j++ {
			m[j*0x9E3779B97F4A7C15] = j
		}
		for j := uint64(0); j < n; j++ {
			m[(size+j)*0x9E3779B97F4A7C15] = j
		}
		sum = uint64(len(m))
	case 1:
		// Lookups that hit.
		m := make(map[uint64]uint64, int(size))
		for j := uint64(0); j < size; j++ {
			m[j*0x9E3779B97F4A7C15] = j
		}
		for j := uint64(0); j < n; j++ {
			sum = sum + m[(j%size)*0x9E3779B97F4A7C15]
		}
	case 2:
		// Lookups that miss.
		m := make(map[uint64]uint64, int(size))
		for j := uint64(0); j < size; j++ {
			m[j*0x9E3779B97F4A7C15] = j
		}
		for j := uint64(0); j < n; j++ {
			_, ok := m[(size+j)*0x9E3779B97F4A7C15]
			sum = sum + flag(ok)
		}
	case 3:
		// Delete and reinsert.
		m := make(map[uint64]uint64, int(size))
		for j := uint64(0); j < size; j++ {
			m[j*0x9E3779B97F4A7C15] = j
		}
		for j := uint64(0); j < n; j++ {
			k := (j % size) * 0x9E3779B97F4A7C15
			delete(m, k)
			m[k] = j
		}
		sum = uint64(len(m))
	case 4:
		// String-key lookups that hit (keys of 1 to 8 bytes, built
		// before the loop).
		m := make(map[string]uint64, int(size))
		keys := make([]string, size)
		for j := uint64(0); j < size; j++ {
			keys[j] = word(j)
			m[keys[j]] = j
		}
		for j := uint64(0); j < n; j++ {
			sum = sum + m[keys[j%size]]
		}
	default:
		// The lookup loop without a map, for the baseline.
		for j := uint64(0); j < n; j++ {
			sum = sum + (j%size)*0x9E3779B97F4A7C15
		}
	}
	w.u64(sum)
}

// Mode 9: long scripts over large key spaces (in[1..2] keys, in[3..4] x 16
// operations) for uint64 (in[5] even) or string keys; for large heaps.
func stress(w *W, in []byte) {
	r := seed(in, 8)
	space := 1 + (uint64(in[1]) | uint64(in[2])<<8)
	ops := 16 * (int(in[3]) | int(in[4])<<8)
	if in[5]%2 == 0 {
		script(w, &r, U64s{}, ops, space)
	} else {
		script(w, &r, Cats{}, ops, space)
	}
}

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		scripts(&w, in)
	case 1:
		values(&w, in)
	case 2:
		aliasing(&w, in)
	case 3:
		unhashable(&w, in)
	case 4:
		generics(&w, in)
	case 5:
		large(&w, in)
	case 6:
		heapUse(out, in)
		return 3
	case 7:
		forms(&w, in)
	case 8:
		probe(&w, in)
	case 9:
		stress(&w, in)
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
