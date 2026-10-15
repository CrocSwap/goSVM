// Package valuetypes is an ordinary-Go oracle for struct and array
// comparison, type aliases, arrays of composite elements and panic in the
// on-chain subset:
//
//   - == and != on structs of every comparable field kind (integers, bools,
//     strings, pointers, interfaces, arrays, nested structs, struct{}, blank
//     fields, which Go ignores), switch on a struct tag, interface values
//     holding structs and arrays, and uncomparable dynamic values (Go
//     panics);
//   - arrays of structs, arrays, pointers, slices, maps and strings: zero
//     values, positional, keyed and [...] literals, element and field
//     updates (=, op=, ++), copies (arrays are values), parameters and
//     results, methods on elements, &a[i], range (over a copy, a pointer and
//     index-only), slicing, slices of arrays, arrays in fields, maps keyed by
//     arrays of structs, equality and bounds failures;
//   - type aliases of scalars, structs, arrays, maps, function types and
//     generic instantiations, with methods declared through an alias;
//   - panic with constant, string, integer, bool, named, error, struct and
//     nil values, in closures, methods, generic code and deferred calls, and
//     a uint128/uint256-style library core (struct ==, [4]uint64 words,
//     tables of [...]Int, overflow panics).
//
// The same source runs natively and through goSVM; tests compare output
// bytes, status and, for panics, the panic's log line for every vector.
// See docs/GO_TYPES.md and docs/GO_ARRAYS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package valuetypes

import "math/bits"

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

// le reads 8 little-endian bytes.
func le(b []byte) uint64 {
	v := uint64(0)
	for j := 7; j >= 0; j-- {
		v = v<<8 | uint64(b[j])
	}
	return v
}

// U128 is a lukechampine.com/uint128-style value.
type U128 struct {
	Lo, Hi uint64
}

func (u U128) IsZero() bool       { return u == U128{} }
func (u U128) Equals(v U128) bool { return u == v }

func (u U128) Cmp(v U128) int {
	if u == v {
		return 0
	} else if u.Hi < v.Hi || (u.Hi == v.Hi && u.Lo < v.Lo) {
		return -1
	}
	return 1
}

func (u U128) Add(v U128) U128 {
	lo, carry := bits.Add64(u.Lo, v.Lo, 0)
	hi, carry := bits.Add64(u.Hi, v.Hi, carry)
	if carry != 0 {
		panic("overflow")
	}
	return U128{lo, hi}
}

func (u U128) Sub(v U128) U128 {
	lo, borrow := bits.Sub64(u.Lo, v.Lo, 0)
	hi, borrow := bits.Sub64(u.Hi, v.Hi, borrow)
	if borrow != 0 {
		panic("underflow")
	}
	return U128{lo, hi}
}

func (u U128) Mul64(v uint64) U128 {
	hi, lo := bits.Mul64(u.Lo, v)
	p0, p1 := bits.Mul64(u.Hi, v)
	hi, c0 := bits.Add64(hi, p1, 0)
	if p0 != 0 || c0 != 0 {
		panic("overflow")
	}
	return U128{lo, hi}
}

// Int is a holiman/uint256-style value: four little-endian words.
type Int [4]uint64

func (z *Int) Add(x, y *Int) *Int {
	var carry uint64
	z[0], carry = bits.Add64(x[0], y[0], 0)
	z[1], carry = bits.Add64(x[1], y[1], carry)
	z[2], carry = bits.Add64(x[2], y[2], carry)
	z[3], _ = bits.Add64(x[3], y[3], carry)
	return z
}

func (z *Int) Eq(x *Int) bool { return *z == *x }
func (z *Int) IsZero() bool   { return *z == Int{} }

// MulSmall sets z = x * y for a one-word y and reports overflow.
func (z *Int) MulSmall(x *Int, y uint64) bool {
	var carry uint64
	for j := 0; j < 4; j++ {
		hi, lo := bits.Mul64(x[j], y)
		lo, c := bits.Add64(lo, carry, 0)
		z[j] = lo
		carry = hi + c
	}
	return carry != 0
}

// Rich has a field of every comparable kind and a blank field.
type Rich struct {
	A  uint8
	_  uint32
	S  string
	P  *uint64
	I  any
	Ar [2]int16
	N  U128
	B  bool
}

// Empty has an empty-struct field and a zero-length array field.
type Empty struct {
	A uint8
	E struct{}
	Z [0]uint64
}

// Pair is a small struct for arrays.
type Pair struct {
	K uint32
	V int64
}

func (p *Pair) Bump(d int64) { p.V += d; p.K++ }
func (p Pair) Sum() int64    { return p.V + int64(p.K) }

// Book holds an array of structs, compared with ==.
type Book struct {
	Entries [4]Pair
	Count   uint8
}

// Node holds an array of pointers to its own type.
type Node struct {
	Kids [2]*Node
	N    uint64
}

// Grid is an array of arrays.
type Grid [3][4]uint8

// Box is generic.
type Box[T any] struct{ V T }

// Aliases of every kind of type.
type (
	Word   = uint64
	Pair2  = Pair
	Words  = [4]Word
	Table  = map[Word]Pair2
	Fn     = func(Word) Word
	IntBox = Box[Word]
	Small  = U128
	Shelf  = [3]Pair2
	Bytes  = []byte
	Any    = any
)

// Weight is declared through an alias receiver: it is Pair's method.
func (p Pair2) Weight() Word { return Word(p.K)*3 + Word(p.V) }

// Code, Label and Flag are named panic values.
type (
	Code  uint16
	Label string
	Flag  bool
)

// Fault is an error type; panicked through an error value.
type Fault struct{ code uint8 }

func (f Fault) Error() string { return "fault" }

func pair(in []byte, j int) Pair {
	return Pair{K: uint32(in[j]) % 4, V: int64(int8(in[j+1])) % 3}
}

// structs compares structs of every field kind.
func structs(w *W, in []byte) {
	x, y := uint64(in[1]), uint64(in[2])
	var p *uint64
	if in[3]%3 == 1 {
		p = &x
	} else if in[3]%3 == 2 {
		p = &y
	}
	strs := [3]string{"", "a", "ab"}
	var ifaces [4]any
	ifaces[1] = uint64(in[4] % 2)
	ifaces[2] = "s"
	ifaces[3] = U128{uint64(in[4] % 2), 0}
	mk := func(k int) Rich {
		r := Rich{A: in[5+k] % 2, S: strs[in[7+k]%3], I: ifaces[in[9+k]%4], B: in[11+k]%2 == 0}
		r.Ar[in[13+k]%2] = int16(in[15+k] % 2)
		r.N = U128{uint64(in[17+k] % 2), uint64(in[19+k] % 2)}
		if in[21+k]%2 == 0 {
			r.P = p
		}
		return r
	}
	a, b := mk(0), mk(1)
	w.u8(flag(a == b))
	w.u8(flag(a != b))
	w.u8(flag(a == a))
	c := a
	c.N.Hi++
	w.u8(flag(c == a))
	// Positional literals set the blank field; == ignores it.
	b1 := Rich{1, 7, "x", nil, nil, [2]int16{}, U128{}, false}
	b2 := Rich{1, uint32(in[22]), "x", nil, nil, [2]int16{}, U128{}, false}
	w.u8(flag(b1 == b2))
	m := map[Rich]uint8{b1: 3}
	w.u8(uint64(m[b2]))
	// U128 methods.
	u := U128{uint64(in[23] % 3), uint64(in[24] % 2)}
	v := U128{uint64(in[25] % 3), uint64(in[26] % 2)}
	w.u8(flag(u.IsZero()))
	w.u8(flag(u.Equals(v)))
	w.u8(uint64(u.Cmp(v) + 1))
	// switch on a struct tag: cases in order, the first match wins.
	switch u {
	case U128{}:
		w.u8(10)
	case U128{1, 0}, U128{0, 1}:
		w.u8(11)
	case v:
		w.u8(12)
	default:
		w.u8(13)
	}
	// Interface values holding structs and arrays.
	var ia, ib any = u, v
	w.u8(flag(ia == ib))
	w.u8(flag(ia == u))
	w.u8(flag(ia == U128{}))
	var ja, jb any = [2]Pair{{K: 1}}, [2]Pair{{K: uint32(in[27] % 2)}}
	w.u8(flag(ja == jb))
	// Empty structs and nested arrays of structs.
	var e1, e2 struct{}
	w.u8(flag(e1 == e2))
	em1, em2 := Empty{A: in[32] % 2}, Empty{}
	w.u8(flag(em1 == em2))
	bk1, bk2 := Book{Count: in[28] % 2}, Book{}
	bk2.Entries[in[29]%4].V = int64(in[30] % 2)
	w.u8(flag(bk1 == bk2))
	w.u8(flag(bk1 != bk2))
	// Comparing a struct whose interface field holds a slice panics.
	if in[31]%4 == 0 {
		s1 := Rich{I: []byte{1}}
		s2 := Rich{I: []byte{1}}
		w.u8(flag(s1 == s2))
	}
	// A different interface field type compares unequal without panicking.
	s3, s4 := Rich{I: []byte{1}}, Rich{I: uint8(1)}
	w.u8(flag(s3 == s4))
}

// update applies an operation to element i of a local array of structs.
func arrays(w *W, in []byte) {
	var ps [4]Pair
	for j := range ps {
		ps[j] = pair(in, 2+2*j)
	}
	i := int(in[1] % 5) // 4 is out of range
	ps[i%4].V += int64(in[10])
	ps[i%4].K++
	p := &ps[(i+1)%4]
	p.V = -p.V
	ps[(i+2)%4].Bump(int64(in[11]))
	w.u64(uint64(ps[(i+3)%4].Sum()))
	cp := ps
	cp[0].K = 99
	w.u8(flag(cp == ps))
	w.u8(flag(cp != ps))
	w.u64(uint64(ps[0].K))
	// Range over a copy, over a pointer and index-only.
	total := int64(0)
	for j, q := range ps {
		ps[(j+1)%4].V++ // not seen by the copy
		q.V *= 2
		total += q.V
	}
	w.u64(uint64(total))
	for _, q := range &ps {
		total += q.V
	}
	w.u64(uint64(total))
	for j := range ps {
		ps[j].K *= 2
	}
	// Keyed and [...] literals.
	kl := [...]Pair{2: {K: 1}, 0: {V: -1}, {K: 7, V: 8}}
	w.u8(uint64(len(kl)))
	w.u64(uint64(kl[0].V) + uint64(kl[2].K) + uint64(kl[1].V))
	// Arrays of arrays.
	var g Grid
	for r := range g {
		for c := range g[r] {
			g[r][c] = in[12+r] ^ uint8(c)
		}
	}
	row := g[in[16]%3]
	row[0] = 77
	g[(in[16]+1)%3] = row
	g[in[17]%3][in[18]%4]++
	g[in[19]%3][in[20]%4] += 5
	w.u8(uint64(g[0][0]) + uint64(g[1][1]) + uint64(g[2][3]))
	g2 := g
	w.u8(flag(g == g2))
	g2[2][3]--
	w.u8(flag(g == g2))
	// Index-only range over an element: Go evaluates nothing, so an out of
	// range row does not panic.
	n := 0
	k := int(in[21] % 5)
	for c := range g[k] {
		n += c
	}
	w.u8(uint64(n))
	// Arrays of pointers (one nil).
	ptrs := [3]*Pair{&ps[0], nil, &ps[2]}
	ptrs[0].V = 5
	w.u64(uint64(ps[0].V))
	if in[22]%5 == 0 {
		w.u64(uint64(ptrs[1].V)) // nil dereference panics
	}
	// Arrays of slices, maps and strings.
	bs := [2][]byte{in[23:25], nil}
	bs[1] = append(bs[1], 9, 8)
	bs[0][0] = 3
	w.u8(uint64(len(bs[0]) + len(bs[1])))
	w.u8(uint64(in[23]))
	ms := [2]map[uint8]uint8{{1: 2}, nil}
	w.u8(uint64(ms[0][1]) + uint64(ms[1][1]))
	if in[25]%5 == 0 {
		ms[1][1] = 1 // assignment to entry in nil map panics
	}
	names := [...]string{"x", "yy", "zzz"}
	w.u8(uint64(len(names[in[26]%3])))
	// Parameters, results and fields.
	sw := swapPairs(ps)
	w.u64(uint64(sw[0].V) ^ uint64(ps[0].V))
	var bk Book
	bk.Entries = sw
	bk.Entries[1].Bump(2)
	bk.Count = uint8(len(bk.Entries))
	w.u64(uint64(bk.Entries[1].Sum()))
	// Slices of arrays of structs, and views of arrays of structs.
	rows := make([][2]Pair, 3)
	rows[in[27]%3][in[28]%2].V = 6
	rows[1] = [2]Pair{{K: 1, V: 2}, {K: 3, V: 4}}
	w.u64(uint64(rows[0][0].V + rows[0][1].V + rows[1][1].V + rows[2][0].V))
	view := ps[1:3]
	view[0].K = 55
	w.u64(uint64(ps[1].K))
	// Maps keyed by arrays of structs; values that are arrays.
	km := map[[2]Pair]uint8{}
	km[[2]Pair{{K: 1}, {V: 2}}] = 4
	k2 := [2]Pair{{K: 1}, {V: int64(in[29] % 3)}}
	w.u8(uint64(km[k2]))
	vm := map[uint8][2]Pair{}
	vm[1] = [2]Pair{{K: 9}}
	got := vm[in[30]%2]
	w.u8(uint64(got[0].K))
	// Arrays of pointers to their own type.
	leaf := &Node{N: 4}
	root := Node{N: 1}
	root.Kids[1] = leaf
	root.Kids[1].Kids[0] = &Node{N: 2}
	w.u64(root.Kids[1].N + root.Kids[1].Kids[0].N)
	// Arrays of arrays of structs.
	var aa [2][2]Pair
	aa[1][0] = Pair{K: 3}
	aa[in[31]%2][1].V = 1
	w.u8(uint64(aa[1][0].K) + uint64(aa[0][1].V) + uint64(aa[1][1].V))
	// Bounds failure on an array of structs.
	w.u64(uint64(ps[i].V))
}

func swapPairs(a [4]Pair) [4]Pair {
	a[0], a[3] = a[3], a[0]
	return a
}

// aliases uses every alias kind.
func aliases(w *W, in []byte) {
	var x Word = Word(in[1])
	var p Pair2 = Pair{K: uint32(in[2]), V: 3}
	w.u64(p.Weight())
	w.u64(x + Word(p.K))
	var ws Words
	ws[in[3]%4] = x
	w.u64(ws[0] + ws[1] + ws[2] + ws[3])
	var ws2 [4]uint64 = ws
	w.u8(flag(ws2 == ws))
	t := Table{1: p}
	t[x] = Pair2{K: 1}
	w.u64(uint64(t[1].K) + uint64(len(t)))
	var f Fn = func(v Word) Word { return v * 2 }
	w.u64(f(x))
	b := IntBox{V: x}
	var b2 Box[uint64] = b
	w.u64(b2.V)
	s := Small{Lo: x}
	w.u8(flag(s == U128{Lo: x}))
	var sh Shelf
	sh[in[4]%3] = p
	w.u64(uint64(sh[0].K + sh[1].K + sh[2].K))
	var bb Bytes = in[5:7]
	w.u8(uint64(len(bb)))
	var a Any = p
	w.u8(flag(a == Pair{K: uint32(in[2]), V: 3}))
	am := map[Any]Word{p: 1}
	w.u64(am[Pair2{K: uint32(in[2]), V: 3}])
}

// must is generic code that panics.
func must[T comparable](v, zero T) T {
	if v == zero {
		panic("zero value")
	}
	return v
}

// positive ends with a panic, a terminating statement.
func positive(v int8) int8 {
	if v >= 0 {
		return v
	}
	panic(int64(v))
}

// panics panics in one of many ways, selected by in[1].
func panics(w *W, in []byte) {
	sel := in[1] % 20
	w.u8(uint64(sel))
	n := int(in[2] % 40)
	switch sel {
	case 0:
		panic("boom")
	case 1:
		panic(string(in[3 : 3+n]))
	case 2:
		panic(int64(-int64(in[3])))
	case 3:
		panic(uint64(in[3]) << 56)
	case 4:
		panic(Code(in[3]))
	case 5:
		panic(Label(string(in[3:5])))
	case 6:
		panic(in[3] > 100)
	case 7:
		panic(Flag(in[3]%2 == 0))
	case 8:
		var err error = Fault{in[3]}
		panic(err)
	case 9:
		f := func(v uint8) {
			if v > 3 {
				panic("closure")
			}
			w.u8(uint64(v))
		}
		f(in[3] % 8)
	case 10:
		u := U128{^uint64(0), ^uint64(0) >> (in[3] % 2)}
		r := u.Add(U128{1, 0})
		w.u64(r.Hi)
	case 11:
		w.u64(must(uint64(in[3]%3), 0))
		w.u64(uint64(len(must("ab"[:in[4]%2], ""))))
	case 12:
		func() {
			defer func() {
				if in[3]%2 == 0 {
					panic("deferred")
				}
			}()
			w.u8(1)
		}()
	case 13:
		panic(nil)
	case 14:
		long := make([]byte, 600)
		for j := range long {
			long[j] = 'a' + byte(j%26)
		}
		long[510] = 0xE2 // a multibyte sequence cut by the 512-byte limit
		long[511] = 0x82
		long[512] = 0xAC
		panic(string(long[:int(in[3])%200+400]))
	case 15:
		panic(Pair{K: 1})
	case 16:
		w.u64(uint64(1 + int64(positive(int8(in[3])))))
	case 17:
		var err error
		if in[3]%2 == 0 {
			err = Fault{1}
		}
		panic(err)
	case 18:
		u := U128{uint64(in[3]), 0}
		w.u64(u.Sub(U128{uint64(in[4]), 0}).Lo)
	case 19:
		panic(int8(in[3]))
	}
}

// library is a uint256/uint128-style core: tables of [...]Int, [4]uint64
// arithmetic, struct comparisons and overflow panics.
func library(w *W, in []byte) {
	pows := [...]Int{{1}, {10}, {100}, {1000}, {10000}, {100000}}
	var x Int
	for j := range x {
		x[j] = le(in[1+8*j:])
	}
	if in[33]%2 == 0 {
		x[3] = 0
		x[2] = 0
	}
	var z Int
	z.Add(&x, &pows[in[34]%6])
	w.u64(z[0])
	w.u64(z[3])
	var m Int
	over := m.MulSmall(&x, pows[in[35]%6][0])
	w.u8(flag(over))
	w.u8(flag(m.Eq(&z)))
	w.u8(flag(z.IsZero()))
	w.u8(flag(pows[1] == Int{10}))
	// Decimal digits of a small Int, by repeated comparison with the table.
	small := Int{uint64(in[36]) * 1000}
	digits := 0
	for j := len(pows) - 1; j >= 0; j-- {
		if small[0] >= pows[j][0] {
			digits = j + 1
			break
		}
	}
	w.u8(uint64(digits))
	// U128 arithmetic with overflow and underflow panics.
	a := U128{le(in[37:]), uint64(in[45] % 4)}
	b := U128{le(in[46:]), uint64(in[54]) << 60}
	sum := a.Add(b)
	w.u64(sum.Lo)
	w.u64(sum.Hi)
	w.u8(uint64(sum.Cmp(a) + 1))
	prod := a.Mul64(uint64(in[55]))
	w.u64(prod.Lo)
	if in[56]%3 == 0 {
		w.u64(a.Sub(b).Lo)
	}
}

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		structs(&w, in)
	case 1:
		arrays(&w, in)
	case 2:
		aliases(&w, in)
	case 3:
		panics(&w, in)
	case 4:
		library(&w, in)
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
