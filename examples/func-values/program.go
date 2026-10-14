// Package funcvalues is an ordinary-Go oracle for function values, closures
// and defer in the on-chain subset: captured variables shared by reference,
// Go 1.22 per-iteration loop variables, method values (receiver bound when
// the value is made, copied for value receivers) and method expressions,
// function values in fields, slices, arrays and interfaces, nil function
// calls, defer ordering and argument evaluation, deferred closures changing
// named results, defer in loops, defer of copy, generic functions taking and
// returning function values, and the heap cost of closure environments. The
// same source runs natively and through goSVM; tests compare output bytes and
// status (or a panic) for every vector. See docs/GO_FUNCTIONS.md.
//
// A panic runs deferred calls natively but aborts on chain without running
// them (the transaction rolls back), so deferred calls here never write the
// output directly: they record into a Log that a completed mode copies out.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package funcvalues

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

// side writes v, so a test can see that an argument was evaluated.
func side(w *W, v byte) uint64 {
	w.u8(uint64(v))
	return uint64(v)
}

// Log collects what deferred calls do.
type Log struct {
	buf []byte
}

func (l *Log) add(v uint64) { l.buf = append(l.buf, byte(v)) }

func (l *Log) flush(w *W) {
	w.u8(uint64(len(l.buf)))
	for _, b := range l.buf {
		w.u8(uint64(b))
	}
}

// Mode 0: captured variables are shared by reference.

type Pair struct {
	A, B uint64
}

type Acc struct {
	n uint64
}

// adder captures its pointer receiver and its parameter.
func (a *Acc) adder(d uint64) func() {
	return func() { a.n = a.n + d }
}

// counter returns a closure over its own variable: each call of counter
// makes a new one.
func counter(start uint64) func() uint64 {
	n := start
	return func() uint64 {
		n++
		return n
	}
}

func captures(w *W, in []byte) {
	x := uint64(in[1])
	inc := func(d uint64) { x = x + d }
	get := func() uint64 { return x }
	inc(uint64(in[2]))
	w.u64(get())
	x = x * 3
	w.u64(get())
	c1 := counter(uint64(in[3]))
	c2 := counter(100)
	w.u64(c1())
	w.u64(c1())
	w.u64(c2())
	w.u64(c1())
	// Nested literals capture through the enclosing literal.
	k := int8(in[4])
	outer := func(a int8) func() int8 {
		return func() int8 {
			k = k + a
			return k
		}
	}
	f := outer(int8(in[5]))
	g := outer(-3)
	w.u64(uint64(f()))
	w.u64(uint64(g()))
	w.u64(uint64(int64(k)))
	// A captured struct and array, changed through the closure.
	p := Pair{A: uint64(in[6]), B: uint64(in[7])}
	var arr [4]uint16
	swap := func() {
		p.A, p.B = p.B, p.A
		arr[in[8]%4] = uint16(p.A)
	}
	swap()
	w.u64(p.A)
	w.u64(p.B)
	w.u64(uint64(arr[0]) + uint64(arr[1])<<16 + uint64(arr[2])<<32 + uint64(arr[3])<<48)
	// A captured receiver and parameter.
	acc := Acc{n: uint64(in[9])}
	add := acc.adder(uint64(in[10]))
	add()
	add()
	w.u64(acc.n)
	// A captured slice and pointer.
	buf := make([]byte, 3)
	ptr := &buf[1]
	fill := func(v byte) {
		buf[0] = v
		*ptr = v + 1
	}
	fill(in[11])
	w.u8(uint64(buf[0]))
	w.u8(uint64(buf[1]))
	w.u8(uint64(buf[2]))
	// A bounds failure inside a closure panics (in[12]%4 == 3).
	idx := func(i byte) byte { return buf[i] }
	w.u8(uint64(idx(in[12] % 4)))
	// Closures assigned to a variable declared before them, then replaced.
	var op func(uint64) uint64
	op = func(v uint64) uint64 { return v + x }
	first := op
	op = func(v uint64) uint64 { return v * x }
	w.u64(first(2))
	w.u64(op(2))
}

// Mode 1: loop variables are per iteration (Go 1.22).

func loops(w *W, in []byte) {
	n := int(in[1] % 9)
	var fs []func() uint64
	for i := 0; i < n; i++ {
		fs = append(fs, func() uint64 { return uint64(i) * 10 })
	}
	for j := range uint8(n) {
		w.u8(fs[j]())
	}
	data := in[2:7]
	var gs [5]func() byte
	for i, v := range data {
		gs[i] = func() byte { return v ^ byte(i) }
	}
	for _, g := range gs {
		w.u8(uint64(g()))
	}
	// A closure that changes the loop variable: the next iteration starts
	// from the changed value.
	var hs []func() int
	for i := 0; i < 20; i++ {
		bump := func() { i = i + int(in[3]%3) }
		bump()
		hs = append(hs, func() int { return i })
		if in[4]%2 == 0 {
			continue
		}
		i++
	}
	w.u8(uint64(len(hs)))
	for _, h := range hs {
		w.u8(uint64(h()))
	}
	// Each iteration's &i is distinct.
	var ps []*int
	for i := 0; i < 3; i++ {
		ps = append(ps, &i)
	}
	for _, p := range ps {
		w.u8(uint64(*p))
	}
	// A loop variable captured but never changed after the closure is made.
	total := uint64(0)
	for i := uint64(1); i <= uint64(in[5]%5); i++ {
		add := func() { total = total + i*i }
		add()
	}
	w.u64(total)
}

// Mode 2: method values bind the receiver when they are made.

type Ctr struct {
	n   uint64
	tag int8
}

func (c Ctr) Get() uint64                { return c.n + uint64(int64(c.tag)) }
func (c Ctr) Plus(d uint64) uint64       { return c.n + d }
func (c *Ctr) Inc(d uint64) uint64       { c.n = c.n + d; return c.n }
func (c Ctr) Bump() uint64               { c.n = c.n + 1000; return c.n }
func (c *Ctr) Record(l *Log)             { l.add(c.n) }
func (c Ctr) Pair(k int16) (int64, bool) { return int64(c.n) * int64(k), c.tag < 0 }

type Box[T any] struct {
	V T
}

func (b Box[T]) Get() T   { return b.V }
func (b *Box[T]) Set(v T) { b.V = v }

type Sq uint32

func (s Sq) Area() uint64  { return uint64(s) * uint64(s) }
func (s Sq) Note(v uint64) {}

// Holder stores function values in fields.
type Holder struct {
	f    func() uint64
	op   Op
	next *Holder
}

func methods(w *W, in []byte) {
	c := Ctr{n: uint64(in[1]), tag: int8(in[2])}
	get := c.Get
	inc := c.Inc
	c.n = c.n + 5
	w.u64(get())
	w.u64(c.Get())
	w.u64(inc(uint64(in[3])))
	w.u64(c.n)
	bump := c.Bump
	w.u64(bump())
	w.u64(bump())
	pair := c.Pair
	v, neg := pair(int16(in[4]) - 100)
	w.u64(uint64(v))
	w.u8(flag(neg))
	// Through a pointer: a value method binds *p now.
	p := &c
	pget := p.Get
	p.n = 7
	w.u64(pget())
	w.u64(p.Get())
	// Nil pointers: a value method dereferences when bound (panic); a
	// pointer method binds nil and panics only when it dereferences.
	var q *Ctr
	if in[5]%3 == 0 {
		q = &Ctr{n: 1}
	}
	qi := q.Inc
	w.u8(1)
	if in[5]%3 == 1 {
		w.u64(qi(1))
	}
	if in[5]%3 == 2 {
		qg := q.Get
		w.u8(2)
		w.u64(qg())
	}
	// A generic type's method values and a named scalar's.
	b := Box[int16]{V: int16(in[6]) - 100}
	bg := b.Get
	bs := b.Set
	b.V = 0
	w.u64(uint64(int64(bg())))
	bs(int16(in[7]))
	w.u64(uint64(int64(b.V)))
	w.u64(uint64(int64(bg())))
	s := Sq(in[8])
	area := s.Area
	s = 0
	w.u64(area())
	// Method values in fields and slices.
	h := Holder{f: c.Get}
	hs := []func() uint64{c.Get, p.Get, s.Area, Sq(3).Area}
	c.n = 1
	w.u64(h.f())
	for _, f := range hs {
		w.u64(f())
	}
}

// Mode 3: method expressions.

func exprs(w *W, in []byte) {
	c := Ctr{n: uint64(in[1])}
	get := Ctr.Get
	pget := (*Ctr).Get
	inc := (*Ctr).Inc
	plus := Ctr.Plus
	w.u64(get(c))
	w.u64(inc(&c, uint64(in[2])))
	w.u64(pget(&c))
	w.u64(plus(c, 9))
	var np *Ctr
	if in[3]%2 == 1 {
		w.u64(pget(np)) // nil dereference when called
	}
	ops := [3]func(Ctr) uint64{Ctr.Get, Ctr.Bump, Ctr.Get}
	for _, op := range ops {
		w.u64(op(c))
	}
	w.u64(c.n)
	w.u64(Ctr.Get(c))
	w.u64((*Ctr).Inc(&c, 2))
	bget := Box[uint8].Get
	w.u64(uint64(bget(Box[uint8]{V: in[4]})))
}

// Mode 4: function values in containers and interfaces, nil calls.

type Op func(a, b uint64) uint64

func add(a, b uint64) uint64 { return a + b }
func mul(a, b uint64) uint64 { return a * b }
func sub(a, b uint64) uint64 { return a - b }

func containers(w *W, in []byte) {
	ops := []Op{add, mul, sub, Op(add)}
	ops = append(ops, func(a, b uint64) uint64 { return a ^ b })
	w.u64(uint64(len(ops)))
	w.u64(uint64(cap(ops)))
	a, b := uint64(in[1]), uint64(in[2])
	for _, op := range ops {
		w.u64(op(a, b))
	}
	var table [4]Op
	table[in[3]%4] = mul
	for i := range table {
		w.u8(flag(table[i] == nil))
		if table[i] != nil {
			w.u64(table[i](a, b))
		}
	}
	h := &Holder{op: sub}
	h.next = &Holder{op: add}
	w.u64(h.op(a, b) + h.next.op(a, b))
	// Interfaces holding function values.
	var x any = Op(mul)
	var y any = add
	if f, ok := x.(Op); ok {
		w.u64(f(a, b))
	}
	_, isOp := y.(Op)
	w.u8(flag(isOp))
	g := y.(func(uint64, uint64) uint64)
	w.u64(g(a, b))
	switch v := y.(type) {
	case Op:
		w.u8(1)
	case func(uint64, uint64) uint64:
		w.u64(v(b, a))
	}
	var nilOp Op
	var z any = nilOp
	w.u8(flag(z != nil))
	if in[4]%4 == 0 {
		w.u8(flag(x == y)) // different dynamic types: false
	}
	if in[4]%4 == 1 {
		w.u8(flag(x == x)) // uncomparable dynamic type: panic
	}
	// Calling a nil function value panics after its arguments run.
	var f func(uint64) uint64
	if in[5]%2 == 0 {
		f = func(v uint64) uint64 { return v + 1 }
	}
	w.u8(flag(f == nil))
	w.u64(f(side(w, in[6])))
	// A slice of function values grows like any other pointer slice.
	var grow []func() uint64
	for j := range int(in[7] % 40) {
		grow = append(grow, c8(uint64(j)))
	}
	w.u64(uint64(cap(grow)))
	if len(grow) > 0 {
		w.u64(grow[len(grow)-1]())
	}
}

func c8(v uint64) func() uint64 { return func() uint64 { return v * 8 } }

// Mode 5: defer.

type Noter interface{ Note(v uint64) }

// order: arguments are evaluated at the defer statement; deferred calls
// run last-in first-out after the result is set, and may change it.
func order(l *Log, in []byte) (r uint64) {
	x := uint64(in[1])
	defer l.add(x)
	x = x + 1
	defer func() { l.add(x) }()
	defer func(v uint64) {
		l.add(v)
		r = r * 2
	}(x)
	x = x + 10
	return x
}

// named: a deferred closure swaps the named results, after either form of
// return.
func named(l *Log, v uint64) (a, b uint64) {
	defer func() {
		a, b = b, a
		l.add(a)
	}()
	a = v
	b = v + 1
	if v%2 == 0 {
		return 100, 200
	}
	return
}

// loopDefers defers in a loop: every iteration's call runs, newest first.
func loopDefers(l *Log, n int) (sum uint64) {
	for i := 0; i < n; i++ {
		defer func() { sum = sum*10 + uint64(i) }()
		defer l.add(uint64(i))
	}
	for _, v := range []uint64{7, 8} {
		if v == uint64(n) {
			defer l.add(v * 3)
		}
	}
	return 0
}

// deferCopy defers the copy builtin; it sees later writes to src.
func deferCopy(in []byte) (r byte) {
	dst := make([]byte, 2)
	src := []byte{in[6], in[7]}
	defer func() { r = r + dst[0] + dst[1] }()
	defer copy(dst, src)
	src[0] = 1
	return 3
}

// methodDefers: receivers are bound at the defer statement.
func methodDefers(l *Log, in []byte) (r uint64) {
	c := Ctr{n: uint64(in[8])}
	defer func() { r = c.n }()
	defer c.Inc(5)
	var s Noter = Sq(in[9])
	defer s.Note(uint64(in[10]))
	defer c.Record(l)
	defer l.add(c.Get())
	c.n = c.n + 1
	g := c.Get
	defer l.add(g())
	defer Ctr.Get(c)
	return 0
}

// nested: a literal with its own defer and named result.
func nested(l *Log, v byte) (r byte) {
	f := func(x byte) (y byte) {
		defer func() {
			y = y + 1
			l.add(uint64(y))
		}()
		return x * 2
	}
	defer func() { r = f(r) }()
	return f(v)
}

// early: defers registered before an early return run; later ones are not
// registered.
func early(l *Log, v byte) uint64 {
	defer l.add(1)
	if v%2 == 0 {
		return 10
	}
	defer l.add(2)
	return 20
}

func nilDefer(w *W) {
	var f func(uint64)
	defer f(side(w, 7)) // panics when the function returns
	w.u8(9)
}

func nilIfaceDefer(w *W) {
	var n Noter
	defer n.Note(side(w, 5)) // panics here, after the argument
	w.u8(9)
}

func nilValueDefer(w *W) {
	var q *Ctr
	defer q.Plus(side(w, 6)) // dereferences here, after the argument
	w.u8(9)
}

func panicky(w *W, l *Log, in []byte) {
	defer l.add(1)
	w.u8(4)
	w.u8(uint64(in[200%len(in)+128])) // out of range: panics with a pending defer
}

func defers(w *W, in []byte) {
	var log Log
	w.u64(order(&log, in))
	a, b := named(&log, uint64(in[2]))
	w.u64(a)
	w.u64(b)
	w.u64(loopDefers(&log, int(in[3]%9)))
	w.u64(uint64(deferCopy(in)))
	w.u64(methodDefers(&log, in))
	w.u64(uint64(nested(&log, in[4])))
	w.u64(early(&log, in[11]))
	log.flush(w)
	switch in[5] % 6 {
	case 0:
		nilDefer(w)
	case 1:
		nilIfaceDefer(w)
	case 2:
		nilValueDefer(w)
	case 3:
		panicky(w, &log, in)
	}
}

// Mode 6: generics with function values.

func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func Fold[T, A any](xs []T, a A, f func(A, T) A) A {
	for _, x := range xs {
		a = f(a, x)
	}
	return a
}

type Ordered interface {
	~int8 | ~uint16 | ~int64 | ~uint64
}

func Max[T Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Adder makes a closure per instance, capturing a T.
func Adder[T Ordered](k T) func(T) T {
	return func(x T) T { return x + k }
}

type Pipe[T any] struct {
	stages []func(T) T
}

func (p *Pipe[T]) Add(f func(T) T) { p.stages = append(p.stages, f) }

func (p Pipe[T]) Count() uint64 { return uint64(len(p.stages)) }

func (p Pipe[T]) Run(x T) T {
	for _, f := range p.stages {
		x = f(x)
	}
	return x
}

func generics(w *W, in []byte) {
	xs := []int8{int8(in[1]), int8(in[2]), int8(in[3])}
	ys := Map(xs, func(x int8) int16 { return int16(x) * 3 })
	for _, y := range ys {
		w.u64(uint64(int64(y)))
	}
	us := Map(xs, Adder(int8(in[4])))
	for _, u := range us {
		w.u8(uint64(uint8(u)))
	}
	sum := Fold(ys, int64(in[5]), func(a int64, y int16) int64 { return a*2 - int64(y) })
	w.u64(uint64(sum))
	m := Max[int8]
	m2 := Max[uint16]
	var m3 func(int64, int64) int64 = Max
	w.u64(uint64(int64(m(int8(in[6]), int8(in[7])))))
	w.u64(uint64(m2(uint16(in[6])<<4, uint16(in[7]))))
	w.u64(uint64(m3(-1, int64(in[8])-200)))
	var p Pipe[uint64]
	p.Add(Adder(uint64(in[9])))
	p.Add(func(x uint64) uint64 { return x * 3 })
	count := p.Count // binds a copy of p: two stages
	before := p.Run(uint64(in[10]))
	p.Add(Adder[uint64](1))
	w.u64(count())
	w.u64(before)
	w.u64(p.Run(uint64(in[10])))
	var pi Pipe[int8]
	pi.Add(Adder(int8(-5)))
	w.u64(uint64(uint8(pi.Run(int8(in[11])))))
}

// Mode 7: the heap cost of closure environments, counted until the heap
// runs out (on chain) or n values are made (natively).

type Big struct {
	a, b, c uint64
}

func (b Big) Sum() uint64  { return b.a + b.b + b.c }
func (b *Big) Ptr() uint64 { return b.a }
func sink(v uint64)        {}
func top() uint64          { return 1 }

func envs(out []byte, in []byte) {
	n := int(in[1]) | int(in[2])<<8
	kind := in[3]
	a, b, c := uint64(in[4]), uint64(in[5]), uint64(in[6])
	big := Big{a: 1}
	pb := &Big{a: 2}
	var keep func() uint64
	for j := 0; j < n; j++ {
		switch kind {
		case 0:
			keep = func() uint64 { return a } // 16 bytes
		case 1:
			keep = func() uint64 { return a + b + c } // 32 bytes
		case 2:
			keep = top // nothing
		case 3:
			keep = func() uint64 { return 3 } // nothing
		case 4:
			keep = big.Sum // 8 + 24 bytes
		case 5:
			keep = pb.Ptr // 16 bytes
		case 6:
			defer sink(uint64(j)) // a 24-byte record
		}
		out[0], out[1] = byte(j+1), byte((j+1)>>8)
	}
	if keep != nil {
		out[3] = byte(keep())
	}
	out[2] = 1
}

// Mode 8: higher-order functions.

// compose returns a function of another signature than its operands, so
// the call graph through function values stays acyclic.
func compose(f, g func(uint64) uint64) func(uint8) uint64 {
	return func(x uint8) uint64 { return g(f(uint64(x))) }
}

func curry(a uint64) func(uint64) func(uint64) uint64 {
	return func(b uint64) func(uint64) uint64 {
		return func(c uint64) uint64 { return a*100 + b*10 + c }
	}
}

func divmod(a, b uint64) (uint64, uint64) { return a / b, a % b }

func pick(b byte) func(uint64) uint64 {
	if b%3 == 0 {
		return nil
	}
	return func(x uint64) uint64 { return x + uint64(b) }
}

func apply(f func(uint64) uint64, x uint64) uint64 { return f(x) }

func higher(w *W, in []byte) {
	inc := func(x uint64) uint64 { return x + 1 }
	dbl := func(x uint64) uint64 { return x * 2 }
	h := compose(inc, dbl)
	h2 := compose(dbl, inc)
	w.u64(h(in[1]))
	w.u64(h2(in[1]))
	w.u64(curry(uint64(in[2]))(uint64(in[3]))(uint64(in[4])))
	dm := divmod
	q, r := dm(uint64(in[5])+1000, uint64(in[6]%8))
	w.u64(q)
	w.u64(r)
	pair := func() (int8, bool) { return int8(in[7]), in[8] > 9 }
	x, ok := pair()
	w.u64(uint64(int64(x)))
	w.u8(flag(ok))
	w.u64(func(a, b uint64) uint64 { return a - b }(uint64(in[9]), uint64(in[10])))
	var op Op = Op(func(a, b uint64) uint64 { return a | b })
	var raw func(uint64, uint64) uint64 = op
	w.u64(raw(uint64(in[11]), uint64(in[12])))
	choose := [3]func(uint64) uint64{inc, dbl, pick(1)}
	w.u64(choose[in[13]%3](uint64(in[14])))
	get := pick(in[15])
	if get != nil {
		w.u64(get(5))
	} else {
		w.u8(0xee)
	}
	w.u64(apply(dbl, uint64(in[16])))
	w.u64(apply(Adder(uint64(3)), uint64(in[16])))
}

func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		captures(&w, in)
	case 1:
		loops(&w, in)
	case 2:
		methods(&w, in)
	case 3:
		exprs(&w, in)
	case 4:
		containers(&w, in)
	case 5:
		defers(&w, in)
	case 6:
		generics(&w, in)
	case 7:
		envs(out, in)
		return 3
	case 8:
		higher(&w, in)
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
