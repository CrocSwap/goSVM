// Package byvalvalues is an ordinary-Go oracle for aggregate parameters
// (strings, slices, structs, arrays, interfaces) that a callee writes
// (docs/GO_STDLIB.md#aggregate-parameters-on-sbf). Go passes every argument
// by value, so a callee's writes to its parameter never reach its caller.
// Clang passes C struct parameters byval and the SBF backend does not copy
// them, so before goSVM copied written parameters at entry, a caller whose
// variable lives in memory (a package variable, a heap object's field, an
// address-taken local, an interface's boxed value, a deferred call's saved
// argument) could see a callee's writes on SBF while the host was correct.
//
// Every case calls writer functions twice on the same variable and writes
// what the writers returned and what the variable holds afterwards. The
// writers modify their parameter (s = s[1:], p.a++, a[i] = v, a value
// receiver's field) and pass it on to another call, which keeps it in
// memory. The same source runs natively and through goSVM; tests compare
// the output bytes.
//
// Run(out, in) runs case in[0]; in[1:] are the values.
package byvalvalues

// W writes little-endian results.
type W struct {
	out []byte
	o   int
}

func (w *W) u64(v uint64) {
	if w.o+8 > len(w.out) {
		return
	}
	for j := 0; j < 8; j++ {
		w.out[w.o+j] = byte(v >> (8 * j))
	}
	w.o += 8
}

func (w *W) str(s string) {
	w.u64(uint64(len(s)))
	for j := 0; j < len(s) && j < 16; j++ {
		w.u64(uint64(s[j]))
	}
}

func (w *W) sl(s []uint64) {
	w.u64(uint64(len(s)))
	for _, v := range s {
		w.u64(v)
	}
}

// P is a struct passed by value.
type P struct {
	a, b, c uint64
	name    string
}

// Box holds aggregates in a heap object.
type Box struct {
	s  string
	xs []uint64
	p  P
	a  [4]uint64
}

// I is implemented by P with a value receiver that writes its receiver.
type I interface{ Bump(k uint64) uint64 }

var (
	gs  string
	gxs []uint64
	gp  P
	ga  [4]uint64
	gb  *Box
	gi  I
)

// peek reads a string where the caller keeps it in memory. Readers and
// writers are large enough that LLVM does not inline them.
func peek(s string) uint64 {
	h := uint64(len(s))
	for j := 0; j < len(s) && j < 3; j++ {
		h = h*131 + uint64(s[(j+0)%len(s)])
		h ^= h >> 3
		h = h*40503 + uint64(s[(j+1)%len(s)])
		h ^= h >> 4
		h = h*31 + uint64(s[(j+2)%len(s)])
		h ^= h >> 5
		h = h*977 + uint64(s[(j+3)%len(s)])
		h ^= h >> 6
		h = h*6151 + uint64(s[(j+4)%len(s)])
		h ^= h >> 7
		h = h*40503 + uint64(s[(j+5)%len(s)])
		h ^= h >> 8
		h = h*193 + uint64(s[(j+6)%len(s)])
		h ^= h >> 9
		h = h*389 + uint64(s[(j+7)%len(s)])
		h ^= h >> 10
		h = h*769 + uint64(s[(j+8)%len(s)])
		h ^= h >> 11
		h = h*1543 + uint64(s[(j+9)%len(s)])
		h ^= h >> 12
		h = h*3079 + uint64(s[(j+10)%len(s)])
		h ^= h >> 13
		h = h*12289 + uint64(s[(j+11)%len(s)])
		h ^= h >> 3
		h = h*24593 + uint64(s[(j+12)%len(s)])
		h ^= h >> 4
		h = h*49157 + uint64(s[(j+13)%len(s)])
		h ^= h >> 5
		h = h*98317 + uint64(s[(j+14)%len(s)])
		h ^= h >> 6
	}
	return h % 1000003
}

// countAll consumes s one byte at a time, passing what is left on.
func countAll(s string, c byte) uint64 {
	s = s[1:]
	n := peek(s)
	for j := uint64(0); j < 2; j++ {
		n = n*131 + j + 0
		n ^= n >> 3
		n = n*40503 + j + 1
		n ^= n >> 4
		n = n*31 + j + 2
		n ^= n >> 5
		n = n*977 + j + 3
		n ^= n >> 6
		n = n*6151 + j + 4
		n ^= n >> 7
		n = n*40503 + j + 5
		n ^= n >> 8
		n = n*193 + j + 6
		n ^= n >> 9
		n = n*389 + j + 7
		n ^= n >> 10
		n = n*769 + j + 8
		n ^= n >> 11
		n = n*1543 + j + 9
		n ^= n >> 12
		n = n*3079 + j + 10
		n ^= n >> 13
		n = n*12289 + j + 11
		n ^= n >> 3
		n = n*24593 + j + 12
		n ^= n >> 4
		n = n*49157 + j + 13
		n ^= n >> 5
		n = n*98317 + j + 14
		n ^= n >> 6
	}
	for len(s) > 0 {
		if s[0] == c {
			n++
		}
		s = s[1:]
		n = n*3 + peek(s)
	}
	return n
}

func headSum(xs []uint64) uint64 {
	h := uint64(len(xs))
	for j := 0; j < len(xs) && j < 3; j++ {
		h = h*131 + xs[(j+0)%len(xs)]
		h ^= h >> 3
		h = h*40503 + xs[(j+1)%len(xs)]
		h ^= h >> 4
		h = h*31 + xs[(j+2)%len(xs)]
		h ^= h >> 5
		h = h*977 + xs[(j+3)%len(xs)]
		h ^= h >> 6
		h = h*6151 + xs[(j+4)%len(xs)]
		h ^= h >> 7
		h = h*40503 + xs[(j+5)%len(xs)]
		h ^= h >> 8
		h = h*193 + xs[(j+6)%len(xs)]
		h ^= h >> 9
		h = h*389 + xs[(j+7)%len(xs)]
		h ^= h >> 10
		h = h*769 + xs[(j+8)%len(xs)]
		h ^= h >> 11
		h = h*1543 + xs[(j+9)%len(xs)]
		h ^= h >> 12
		h = h*3079 + xs[(j+10)%len(xs)]
		h ^= h >> 13
		h = h*12289 + xs[(j+11)%len(xs)]
		h ^= h >> 3
		h = h*24593 + xs[(j+12)%len(xs)]
		h ^= h >> 4
		h = h*49157 + xs[(j+13)%len(xs)]
		h ^= h >> 5
		h = h*98317 + xs[(j+14)%len(xs)]
		h ^= h >> 6
	}
	return h % 1000003
}

// drain consumes xs one element at a time, passing what is left on.
func drain(xs []uint64) uint64 {
	xs = xs[1:]
	n := headSum(xs)
	for j := uint64(0); j < 2; j++ {
		n = n*131 + j + 0
		n ^= n >> 3
		n = n*40503 + j + 1
		n ^= n >> 4
		n = n*31 + j + 2
		n ^= n >> 5
		n = n*977 + j + 3
		n ^= n >> 6
		n = n*6151 + j + 4
		n ^= n >> 7
		n = n*40503 + j + 5
		n ^= n >> 8
		n = n*193 + j + 6
		n ^= n >> 9
		n = n*389 + j + 7
		n ^= n >> 10
		n = n*769 + j + 8
		n ^= n >> 11
		n = n*1543 + j + 9
		n ^= n >> 12
		n = n*3079 + j + 10
		n ^= n >> 13
		n = n*12289 + j + 11
		n ^= n >> 3
		n = n*24593 + j + 12
		n ^= n >> 4
		n = n*49157 + j + 13
		n ^= n >> 5
		n = n*98317 + j + 14
		n ^= n >> 6
	}
	for len(xs) > 0 {
		n = n*7 + xs[0]
		xs = xs[1:]
		n += headSum(xs)
	}
	return n
}

func useP(p P) uint64 {
	h := p.a*1000000 + p.b*1000 + p.c
	for j := uint64(0); j < 2; j++ {
		h = h*131 + p.a + j
		h ^= h >> 3
		h = h*40503 + p.b + j
		h ^= h >> 4
		h = h*31 + p.c + j
		h ^= h >> 5
		h = h*977 + uint64(len(p.name)) + j
		h ^= h >> 6
		h = h*6151 + p.a + j
		h ^= h >> 7
		h = h*40503 + p.b + j
		h ^= h >> 8
		h = h*193 + p.c + j
		h ^= h >> 9
		h = h*389 + uint64(len(p.name)) + j
		h ^= h >> 10
		h = h*769 + p.a + j
		h ^= h >> 11
		h = h*1543 + p.b + j
		h ^= h >> 12
		h = h*3079 + p.c + j
		h ^= h >> 13
		h = h*12289 + uint64(len(p.name)) + j
		h ^= h >> 3
		h = h*24593 + p.a + j
		h ^= h >> 4
		h = h*49157 + p.b + j
		h ^= h >> 5
		h = h*98317 + p.c + j
		h ^= h >> 6
	}
	return h%1000003 + peek(p.name)
}

// bumpP writes fields of its parameter and passes it on.
func bumpP(p P, k uint64) uint64 {
	p.a += k
	r := useP(p)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	p.b = p.a * 2
	p.name = p.name[1:]
	return r + useP(p)
}

func sumA(a [4]uint64) uint64 {
	h := a[0] + a[1]*10 + a[2]*100 + a[3]*1000
	for j := uint64(0); j < 2; j++ {
		h = h*131 + a[(j+0)%4]
		h ^= h >> 3
		h = h*40503 + a[(j+1)%4]
		h ^= h >> 4
		h = h*31 + a[(j+2)%4]
		h ^= h >> 5
		h = h*977 + a[(j+3)%4]
		h ^= h >> 6
		h = h*6151 + a[(j+4)%4]
		h ^= h >> 7
		h = h*40503 + a[(j+5)%4]
		h ^= h >> 8
		h = h*193 + a[(j+6)%4]
		h ^= h >> 9
		h = h*389 + a[(j+7)%4]
		h ^= h >> 10
		h = h*769 + a[(j+8)%4]
		h ^= h >> 11
		h = h*1543 + a[(j+9)%4]
		h ^= h >> 12
		h = h*3079 + a[(j+10)%4]
		h ^= h >> 13
		h = h*12289 + a[(j+11)%4]
		h ^= h >> 3
		h = h*24593 + a[(j+12)%4]
		h ^= h >> 4
		h = h*49157 + a[(j+13)%4]
		h ^= h >> 5
		h = h*98317 + a[(j+14)%4]
		h ^= h >> 6
	}
	return h % 1000003
}

// setA writes an element of its array parameter and passes it on.
func setA(a [4]uint64, i, v uint64) uint64 {
	a[i%4] = v
	r := sumA(a)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	a[(i+1)%4] += v
	return r + sumA(a)
}

// Bump has a value receiver: it writes its own copy of the receiver.
func (p P) Bump(k uint64) uint64 {
	p.c += k
	r := useP(p)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	p.name = p.name[1:]
	return r + useP(p)
}

// swapP returns its parameter rearranged: the result's fields are written
// while the parameter is still read.
func swapP(p P) P {
	r := useP(p)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	if r == 1 {
		return p
	}
	return P{p.b, p.c, p.a, p.name}
}

// rest returns its parameter without its first byte.
func rest(s string) string {
	s = s[1:]
	r := peek(s)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	if r == 1 {
		return s[1:]
	}
	return s
}

func rec(w *W, s string, xs []uint64, p P) {
	w.u64(countAll(s, 'a'))
	w.u64(drain(xs))
	w.u64(bumpP(p, 3))
	w.str(s)
	w.sl(xs)
	w.u64(useP(p))
}

func reset(in []byte) {
	n := 4 + int(in[1]%8)
	b := make([]byte, n)
	for j := range b {
		b[j] = 'a' + in[2+j]%3
	}
	gs = string(b)
	gxs = []uint64{uint64(in[10]), uint64(in[11]), uint64(in[12]), uint64(in[13])}[:2+int(in[14]%3)]
	gp = P{uint64(in[15] % 10), uint64(in[16] % 10), uint64(in[17] % 10), gs}
	ga = [4]uint64{uint64(in[18] % 10), uint64(in[19] % 10), uint64(in[20] % 10), uint64(in[21] % 10)}
	gb = &Box{s: gs, xs: gxs, p: gp, a: ga}
	gi = gp
}

// Cases is the number of cases Run dispatches on.
const Cases = 21

// Run runs case in[0] and records the variables afterwards.
func Run(out, in []byte) uint64 {
	reset(in)
	w := &W{out: out}
	k := uint64(in[22]%5) + 1
	switch in[0] % Cases {
	case 0: // package variables
		w.u64(countAll(gs, 'a'))
		w.u64(countAll(gs, 'b'))
		w.str(gs)
		w.u64(drain(gxs))
		w.u64(drain(gxs))
		w.sl(gxs)
	case 1:
		w.u64(bumpP(gp, k))
		w.u64(bumpP(gp, k))
		w.u64(useP(gp))
		w.u64(setA(ga, k, 9))
		w.u64(setA(ga, k, 9))
		w.u64(sumA(ga))
	case 2: // fields of a heap object
		w.u64(countAll(gb.s, 'a'))
		w.u64(countAll(gb.s, 'c'))
		w.str(gb.s)
		w.u64(drain(gb.xs))
		w.u64(drain(gb.xs))
		w.sl(gb.xs)
	case 3:
		w.u64(bumpP(gb.p, k))
		w.u64(bumpP(gb.p, k))
		w.u64(useP(gb.p))
		w.u64(setA(gb.a, k, 7))
		w.u64(setA(gb.a, k, 7))
		w.u64(sumA(gb.a))
	case 4: // address-taken locals
		p, a := gp, ga
		pp, pa := &p, &a
		bx := Box{s: gs, xs: gxs}
		pb := &bx
		w.u64(countAll(bx.s, 'a'))
		w.u64(countAll(pb.s, 'a'))
		w.u64(drain(bx.xs))
		w.u64(drain(pb.xs))
		w.u64(bumpP(p, k))
		w.u64(bumpP(*pp, k))
		w.u64(setA(a, k, 5))
		w.u64(setA(*pa, k, 5))
		w.str(pb.s)
		w.sl(pb.xs)
		w.u64(useP(*pp))
		w.u64(sumA(*pa))
	case 5: // value receivers
		w.u64(gp.Bump(k))
		w.u64(gp.Bump(k))
		w.u64(gb.p.Bump(k))
		w.u64(gb.p.Bump(k))
		w.u64(useP(gp))
		w.u64(useP(gb.p))
	case 6: // an interface's boxed value
		w.u64(gi.Bump(k))
		w.u64(gi.Bump(k))
		w.u64(useP(gi.(P)))
	case 7: // method values and function values
		f := gp.Bump
		w.u64(f(k))
		w.u64(f(k))
		g := countAll
		w.u64(g(gs, 'a'))
		w.u64(g(gs, 'a'))
		w.u64(useP(gp))
		w.str(gs)
	case 8: // deferred calls
		func() {
			defer rec(w, gs, gxs, gp)
			defer rec(w, gb.s, gb.xs, gb.p)
		}()
		w.str(gs)
		w.u64(useP(gp))
	case 9: // closures
		bump := func(p P) uint64 {
			p.a++
			return useP(p) + useP(p)
		}
		w.u64(bump(gp))
		w.u64(bump(gp))
		w.u64(useP(gp))
		cut := func(s string) uint64 {
			s = s[2:]
			return peek(s)
		}
		w.u64(cut(gs))
		w.u64(cut(gs))
		w.str(gs)
	case 10: // results replacing the argument's variable
		gp = swapP(gp)
		w.u64(useP(gp))
		gb.p = swapP(gb.p)
		w.u64(useP(gb.p))
		p := gp
		pp := &p
		p = swapP(p)
		w.u64(useP(*pp))
	case 11:
		gs = rest(gs)
		w.str(gs)
		gb.s = rest(gb.s)
		w.str(gb.s)
		w.u64(countAll(gs, 'a'))
		w.str(gs)
	case 12: // elements of slices and arrays of aggregates
		ps := []P{gp, gp}
		w.u64(bumpP(ps[0], k))
		w.u64(bumpP(ps[0], k))
		w.u64(useP(ps[0]))
		ss := [2]string{gs, gs}
		w.u64(countAll(ss[1], 'b'))
		w.u64(countAll(ss[1], 'b'))
		w.str(ss[1])
	case 13: // a map's values
		m := map[uint64]P{1: gp}
		w.u64(bumpP(m[1], k))
		w.u64(bumpP(m[1], k))
		w.u64(useP(m[1]))
	case 14: // the same variable passed twice
		w.u64(two(gp, gp))
		w.u64(useP(gp))
	case 16: // callees that change the caller's variable by another path
		w.u64(clobberP(gp))
		w.u64(useP(gp))
		w.u64(clobberBox(gb.p, gb.s, gb.xs, gb.a, gb))
		w.u64(useP(gb.p))
		w.str(gb.s)
		w.sl(gb.xs)
		w.u64(sumA(gb.a))
	case 17:
		p := gp
		pp := &p
		w.u64(clobberPtr(p, pp))
		w.u64(useP(p))
	case 18:
		w.u64(clobberP(gi.(P)))
		var j I = gp
		w.u64(clobberI(j, &j))
	case 19: // a parameter read after its variable was replaced
		w.u64(clobberS(gs))
		w.str(gs)
	case 20: // append with spare capacity leaves its operand alone
		t := append(gxs, 7)
		u := append(gb.xs, 8, 9)
		v := append(gxs[:1], 5)
		x := append(gb.xs[:2], 6)
		w.sl(t)
		w.sl(u)
		w.sl(v)
		w.sl(x)
		w.sl(gxs)
		w.sl(gb.xs)
	case 15: // a parameter passed on to a writer by its own callee
		w.u64(outer(gp, gs))
		w.u64(outer(gp, gs))
		w.u64(useP(gp))
		w.str(gs)
	}
	return uint64(w.o)
}

func two(p, q P) uint64 {
	p.a = 100
	r := useP(p)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	return r + useP(q)
}

// The clobber functions read their (unwritten) parameters after changing
// the variable the caller passed, through a package variable or a pointer:
// the parameters must keep the values they had at the call.
func clobberP(p P) uint64 {
	gp.a += 50
	gp.name = "zz"
	r := useP(p)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	return r + useP(p)
}

func clobberBox(p P, s string, xs []uint64, a [4]uint64, b *Box) uint64 {
	b.p.b += 60
	b.s = b.s[2:]
	b.xs = b.xs[1:]
	b.a[0] += 70
	r := useP(p) + peek(s) + headSum(xs) + sumA(a)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	return r + useP(p) + peek(s) + headSum(xs) + sumA(a)
}

func clobberPtr(p P, q *P) uint64 {
	*q = P{1, 2, 3, "q"}
	r := useP(p)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	return r + useP(p)
}

func clobberI(i I, q *I) uint64 {
	*q = P{4, 5, 6, "i"}
	r := i.Bump(1)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	return r + i.Bump(2)
}

func clobberS(s string) uint64 {
	gs = "q"
	r := peek(s)
	for j := uint64(0); j < 2; j++ {
		r = r*131 + j + 0
		r ^= r >> 3
		r = r*40503 + j + 1
		r ^= r >> 4
		r = r*31 + j + 2
		r ^= r >> 5
		r = r*977 + j + 3
		r ^= r >> 6
		r = r*6151 + j + 4
		r ^= r >> 7
		r = r*40503 + j + 5
		r ^= r >> 8
		r = r*193 + j + 6
		r ^= r >> 9
		r = r*389 + j + 7
		r ^= r >> 10
		r = r*769 + j + 8
		r ^= r >> 11
		r = r*1543 + j + 9
		r ^= r >> 12
		r = r*3079 + j + 10
		r ^= r >> 13
		r = r*12289 + j + 11
		r ^= r >> 3
		r = r*24593 + j + 12
		r ^= r >> 4
		r = r*49157 + j + 13
		r ^= r >> 5
		r = r*98317 + j + 14
		r ^= r >> 6
	}
	return r + peek(s)
}

// outer does not write its parameters; it passes them on to writers.
func outer(p P, s string) uint64 {
	return bumpP(p, 1) + countAll(s, 'b') + bumpP(p, 2) + countAll(s, 'c') + useP(p) + peek(s)
}

func Process(s, i []byte) uint64 { return Run(s, i) }
