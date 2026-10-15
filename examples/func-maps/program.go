// Package funcmaps is an ordinary-Go oracle for maps combined with function
// values, closures, defer and named results in the on-chain subset: closures
// capturing maps by reference, maps of function values (dispatch tables,
// absent keys yielding nil function values), function values taking and
// returning maps, deferred calls that read and change maps, named map
// results, method values and method expressions of map-backed named types,
// generic code over both, maps next to strings and errors, and the heap cost
// of closures capturing maps and loop defers saving maps. Iteration over a
// map is never used. The same source runs natively and through goSVM; tests
// compare output bytes and status (or a panic) for every vector. See
// docs/GO_FUNCTIONS.md and docs/GO_MAPS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package funcmaps

// W writes little-endian values and length-prefixed strings into a byte
// slice held in a field.
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

// Mode 0: closures capturing maps by reference.

// counter returns closures sharing one captured map: inc changes it, get
// reads it, and reset replaces the captured variable itself.
func counter() (func(uint8), func(uint8) uint64, func()) {
	m := map[uint8]uint64{}
	inc := func(k uint8) { m[k]++ }
	get := func(k uint8) uint64 { return m[k] }
	reset := func() { m = make(map[uint8]uint64) }
	return inc, get, reset
}

// memo caches f in a captured map; calls counts the calls of f. f has a
// different signature from the result: a literal of type func(uint64)
// uint64 calling a func(uint64) uint64 value is rejected by the
// conservative recursion check (docs/GO_FUNCTIONS.md#recursion).
func memo(f func(uint8) uint64, calls *uint64) func(uint64) uint64 {
	cache := make(map[uint64]uint64)
	return func(x uint64) uint64 {
		if v, ok := cache[x]; ok {
			return v
		}
		*calls = *calls + 1
		v := f(uint8(x))
		cache[x] = v
		return v
	}
}

func square(x uint64) uint64 { return x * x }

func square8(x uint8) uint64 { return uint64(x) * uint64(x) }

func captures(w *W, in []byte) {
	inc, get, reset := counter()
	for j := 1; j < 9; j++ {
		inc(in[j] % 4)
	}
	for k := uint8(0); k < 4; k++ {
		w.u8(get(k))
	}
	reset()
	inc(in[9] % 4)
	w.u8(get(in[9] % 4))
	w.u8(get((in[9] + 1) % 4))
	calls := uint64(0)
	sq := memo(square8, &calls)
	total := uint64(0)
	for j := 10; j < 18; j++ {
		total = total + sq(uint64(in[j]%5))
	}
	w.u64(total)
	w.u8(calls)
	// The enclosing function sees writes made through a closure, and a
	// closure sees writes the enclosing function makes.
	seen := map[string]bool{}
	mark := func(s string) bool {
		old := seen[s]
		seen[s] = true
		return old
	}
	w.u8(flag(mark(string(in[18:20]))))
	w.u8(flag(mark(string(in[20:22]))))
	seen["zz"] = true
	w.u8(flag(mark("zz")))
	w.u8(uint64(len(seen)))
	// Nested literals capture a map through the enclosing literal.
	outer := func(base uint8) func() int {
		local := map[uint8]uint8{base: 1}
		return func() int {
			local[base+uint8(len(local))] = base
			return len(local)
		}
	}
	grow := outer(in[22])
	grow()
	w.u8(uint64(grow()))
	// Go 1.22: each iteration's closure captures its own map variable.
	var fs []func() int
	for i := 0; i < 3; i++ {
		m := map[int]int{i: i}
		fs = append(fs, func() int { m[i+10] = i; return len(m) + i })
	}
	for _, f := range fs {
		w.u8(uint64(f()))
	}
	// A captured nil map: reads work, a write panics.
	var nilMap map[uint8]uint8
	read := func(k uint8) uint8 { return nilMap[k] }
	w.u8(uint64(read(in[23])))
	if in[24]%4 == 0 {
		w.u8(0xaa)
		write := func() { nilMap[in[25]] = 1 }
		write()
	}
}

// Mode 1: maps of function values; function values over maps.

// Op is a named function type stored in maps.
type Op func(a, b uint64) uint64

func add(a, b uint64) uint64 { return a + b }
func sub(a, b uint64) uint64 { return a - b }
func mul(a, b uint64) uint64 { return a * b }

// handler holds a function value in a struct stored in a map.
type handler struct {
	name string
	run  func(uint64) uint64
}

// Table is a named map of function values.
type Table map[string]Op

// apply looks up an operation; an absent one is a nil Op.
func (t Table) apply(name string, a, b uint64) uint64 {
	return t[name](a, b)
}

// fold applies f to each key of keys in m, which it may change.
func fold(m map[uint8]uint64, keys []uint8, f func(map[uint8]uint64, uint8) uint64) uint64 {
	acc := uint64(0)
	for _, k := range keys {
		acc = acc*3 + f(m, k)
	}
	return acc
}

// builder returns a new map filled by f.
func builder(n int, f func(int) (uint8, uint64)) map[uint8]uint64 {
	m := make(map[uint8]uint64, n)
	for j := 0; j < n; j++ {
		k, v := f(j)
		m[k] = m[k] + v
	}
	return m
}

// Keys is a generic map-to-slice helper driven by a slice of keys.
func Keys[K comparable, V any](m map[K]V, keys []K, keep func(V) bool) []K {
	var out []K
	for _, k := range keys {
		if v, ok := m[k]; ok && keep(v) {
			out = append(out, k)
		}
	}
	return out
}

// Index builds a map with a generic key function.
func Index[T any, K comparable](xs []T, key func(T) K) map[K]int {
	m := map[K]int{}
	for _, x := range xs {
		m[key(x)]++
	}
	return m
}

func values(w *W, in []byte) {
	t := Table{"add": add, "sub": sub, "mul": Op(mul)}
	t["shl"] = func(a, b uint64) uint64 { return a << (b % 64) }
	a, b := uint64(in[1]), uint64(in[2])
	for _, name := range []string{"add", "sub", "mul", "shl"} {
		w.u64(t.apply(name, a, b))
	}
	f, ok := t["div"]
	w.u8(flag(ok))
	w.u8(flag(f == nil))
	delete(t, "sub")
	_, ok = t["sub"]
	w.u8(flag(ok))
	w.u8(uint64(len(t)))
	// Function values stored as struct fields inside map elements.
	scale := uint64(in[3]%7) + 1
	hs := map[uint8]handler{
		1: {"double", func(x uint64) uint64 { return x * 2 }},
		2: {"scaled", func(x uint64) uint64 { return x * scale }},
		3: {"square", square},
	}
	scale = scale + 1 // the closure captured scale by reference
	for k := uint8(1); k < 4; k++ {
		h := hs[k]
		w.str(h.name)
		w.u64(h.run(uint64(in[4])))
	}
	// Function values taking and returning maps.
	m := builder(int(in[5]%6), func(j int) (uint8, uint64) { return uint8(j) % 3, uint64(j + 1) })
	w.u8(uint64(len(m)))
	keys := []uint8{0, 1, 2, 3}
	w.u64(fold(m, keys, func(mm map[uint8]uint64, k uint8) uint64 {
		v := mm[k]
		mm[k+10] = v
		return v
	}))
	w.u8(uint64(len(m)))
	big := Keys(m, []uint8{0, 1, 2, 10, 11}, func(v uint64) bool { return v > 2 })
	w.u8(uint64(len(big)))
	for _, k := range big {
		w.u8(uint64(k))
	}
	idx := Index([]string{"a", string(in[6:7]), "a", string(in[7:8])}, func(s string) string { return s })
	w.u8(uint64(idx["a"]))
	w.u8(uint64(len(idx)))
	// A map of function values keyed by interface values.
	byKey := map[any]func() uint64{
		uint8(1): func() uint64 { return 11 },
		"two":    func() uint64 { return 22 },
		[2]uint8{3, 3}: func() uint64 {
			return 33
		},
	}
	var key any = uint8(in[8] % 3)
	if g, ok := byKey[key]; ok {
		w.u64(g())
	} else {
		w.u8(0xee)
	}
	w.u64(byKey[[2]uint8{3, 3}]())
	// A function value as an interface key is unhashable: Go panics.
	if in[9]%5 == 0 {
		w.u8(0xab)
		var fk any = square
		_ = byKey[fk]
	}
	// Calling the function value of an absent key calls nil: Go panics
	// after the arguments are evaluated.
	if in[10]%5 == 0 {
		arg := func() uint64 { w.u8(0xac); return 1 }
		w.u64(t["missing"](arg(), 2))
	}
}

// Mode 2: defer reading and changing maps; named map results.

// audit records its arguments in log through deferred calls; the map
// argument is evaluated at the defer statement but is a reference, so the
// deferred call sees later writes.
func audit(log map[string]uint64, v uint64) (out uint64) {
	defer note(log, "first", v)
	defer func() { log["out"] = out }()
	log["v"] = v
	v = v + 100
	defer note(log, "second", v)
	out = v * 2
	return out + 1
}

func note(log map[string]uint64, k string, v uint64) { log[k] = log[k]*1000 + v }

// collect builds its named map result in deferred calls.
func collect(n int, fail bool) (m map[uint8]uint8, err error) {
	defer func() {
		if err != nil {
			m[0xff] = uint8(len(m))
		}
	}()
	m = make(map[uint8]uint8)
	for j := 0; j < n; j++ {
		defer func() { m[uint8(j)] = uint8(j * 2) }()
	}
	if fail {
		return m, Code(n)
	}
	return
}

// Code is an error through a value receiver.
type Code uint8

func (c Code) Error() string { return "code " + string(rune('A'+c%26)) }

// later replaces its named result after the return value is set.
func later() (m map[string]int) {
	defer func() {
		old := m
		m = map[string]int{"replaced": len(old)}
	}()
	return map[string]int{"a": 1, "b": 2}
}

// deleteAll deletes keys in deferred calls in a loop (heap records).
func deleteAll(m map[uint8]uint64, keys []uint8) int {
	for _, k := range keys {
		defer delKey(m, k)
	}
	return len(m)
}

func delKey(m map[uint8]uint64, k uint8) { delete(m, k) }

// deferNilWrite writes to a nil map in a deferred call: Go panics when the
// function returns, after its body.
func deferNilWrite(w *W) {
	var m map[uint8]uint8
	defer func() { m[1] = 1 }()
	w.u8(0xbb)
}

// deferUnhashable looks up an unhashable interface key in a deferred call.
func deferUnhashable(w *W, m map[any]uint8) {
	var k any = []uint8{1}
	defer func() { w.u8(uint64(m[k])) }()
	w.u8(0xbc)
}

func defers(w *W, in []byte) {
	log := map[string]uint64{}
	w.u64(audit(log, uint64(in[1])))
	for _, k := range []string{"first", "second", "out", "v", "none"} {
		w.u64(log[k])
	}
	m, err := collect(int(in[2]%5), in[3]%2 == 0)
	w.u8(uint64(len(m)))
	for k := uint8(0); k < 5; k++ {
		v, ok := m[k]
		w.u8(uint64(v))
		w.u8(flag(ok))
	}
	w.u8(uint64(m[0xff]))
	w.u8(flag(err == nil))
	if err != nil {
		w.str(err.Error())
	}
	r := later()
	w.u8(uint64(len(r)))
	w.u8(uint64(r["replaced"]))
	d := map[uint8]uint64{1: 1, 2: 2, 3: 3, 4: 4}
	keys := []uint8{in[4] % 6, in[5] % 6}
	w.u8(uint64(deleteAll(d, keys)))
	w.u8(uint64(len(d)))
	// A deferred method value of a map-backed type binds the map.
	c := Counter{}
	func() {
		defer c.Inc("deferred")
		c.Inc("body")
		c["deferred"] = 10
	}()
	w.u64(c["deferred"])
	w.u64(c["body"])
	if in[6]%5 == 0 {
		deferNilWrite(w)
	}
	if in[7]%5 == 0 {
		deferUnhashable(w, map[any]uint8{})
	}
}

// Mode 3: method values and expressions of map-backed named types.

// Counter is a map-backed named type with value and pointer receivers.
type Counter map[string]uint64

func (c Counter) Inc(k string)         { c[k]++ }
func (c Counter) Get(k string) uint64  { return c[k] }
func (c Counter) Len() int             { return len(c) }
func (c *Counter) Reset()              { *c = Counter{} }
func (c *Counter) Add(k string, v int) { (*c)[k] = (*c)[k] + uint64(v) }

// Set is a generic map-backed set.
type Set[T comparable] map[T]struct{}

func (s Set[T]) Put(v T)           { s[v] = struct{}{} }
func (s Set[T]) Has(v T) bool      { _, ok := s[v]; return ok }
func (s *Set[T]) Clear()           { *s = nil }
func (s Set[T]) Size() int         { return len(s) }
func (s Set[T]) Len() int          { return len(s) }
func NewSet[T comparable]() Set[T] { return Set[T]{} }

// Lener is satisfied by map-backed types through value receivers.
type Lener interface{ Len() int }

func methods(w *W, in []byte) {
	c := Counter{}
	inc := c.Inc // binds the map reference (a copy of the 8-byte value)
	get := c.Get
	inc(string(in[1:2]))
	inc(string(in[1:2]))
	c = Counter{"fresh": 1} // the bound values keep the old map
	inc("x")
	w.u64(get(string(in[1:2])))
	w.u64(get("x"))
	w.u64(c.Get("x"))
	w.u8(uint64(c.Len()))
	reset := c.Reset // binds &c
	add := c.Add
	add("y", int(in[2]))
	reset()
	w.u8(uint64(c.Len()))
	add("z", 3) // still bound to &c: sees the reset map
	w.u64(c["z"])
	ie := Counter.Inc
	ie(c, "e")
	pe := (*Counter).Add
	pe(&c, "e", 5)
	w.u64(c["e"])
	vg := (*Counter).Get // dereferences its argument when called
	w.u64(vg(&c, "e"))
	// Generic map-backed set methods as values.
	s := NewSet[uint16]()
	put := s.Put
	has := s.Has
	for j := 3; j < 9; j++ {
		put(uint16(in[j] % 8))
	}
	n := 0
	for v := uint16(0); v < 8; v++ {
		if has(v) {
			n++
		}
	}
	w.u8(uint64(n))
	w.u8(uint64(s.Size()))
	clr := s.Clear
	clr()
	w.u8(flag(s == nil))
	w.u8(flag(has(uint16(in[3] % 8)))) // the bound value keeps the old map
	// Through interfaces: a map in an interface is its reference.
	var l Lener = c
	lf := func() int { return l.Len() }
	c["after"] = 1
	w.u8(uint64(lf()))
	var sl Lener = Set[string]{"a": {}}
	w.u8(uint64(sl.Len()))
	switch v := l.(type) {
	case Counter:
		v.Inc("via-switch")
		w.u64(c["via-switch"])
	}
	// A value-receiver method value through a nil *Counter: Go panics
	// when the value is made.
	var np *Counter
	if in[9]%4 == 0 {
		w.u8(0xcc)
		bad := np.Get
		w.u64(bad("x"))
	}
	// Inc through a nil Counter writes to a nil map: Go panics.
	if in[10]%4 == 0 {
		var nc Counter
		f := nc.Inc
		w.u8(0xcd)
		f("x")
	}
}

// Mode 4: maps, strings, errors and function values together.

// Registry maps names to validators returning errors.
type Registry map[string]func(string) error

func (r Registry) check(name, v string) (msg string) {
	defer func() {
		if msg == "" {
			msg = "ok"
		}
	}()
	f, ok := r[name]
	if !ok {
		return "no " + name
	}
	if err := f(v); err != nil {
		return err.Error()
	}
	return
}

func registry(w *W, in []byte) {
	limit := int(in[1] % 6)
	r := Registry{
		"short": func(s string) error {
			if len(s) > limit {
				return Code(len(s))
			}
			return nil
		},
		"lower": func(s string) error {
			for _, c := range []byte(s) {
				if c < 'a' || c > 'z' {
					return Code(c)
				}
			}
			return nil
		},
	}
	v := string(in[2 : 2+int(in[3]%5)])
	for _, name := range []string{"short", "lower", "none"} {
		w.str(r.check(name, v))
	}
	limit = 0 // seen by the captured limit
	w.str(r.check("short", "ab"))
	// Errors as map elements and keys.
	errs := map[error]uint8{Code(1): 1, Code(2): 2}
	errs[Code(in[4]%4)]++
	w.u8(uint64(errs[Code(1)]))
	w.u8(uint64(errs[Code(in[4]%4)]))
	w.u8(uint64(len(errs)))
	// String-keyed counters built by a closure.
	words := map[string]int{}
	count := func(s string) { words[s] = words[s] + len(s) }
	for j := 5; j < 11; j++ {
		count(string(in[j : j+1+int(in[j]%2)]))
	}
	w.u8(uint64(len(words)))
	w.u8(uint64(words[string(in[5:6])]))
	// Maps of maps updated through a function value.
	nested := map[string]map[string]uint8{}
	upd := func(a, b string) {
		inner := nested[a]
		if inner == nil {
			inner = map[string]uint8{}
			nested[a] = inner
		}
		inner[b]++
	}
	upd("x", "y")
	upd("x", "y")
	upd(string(in[11:12]), "z")
	w.u8(uint64(nested["x"]["y"]))
	w.u8(uint64(len(nested)))
	// An interface key holding a map is unhashable: Go panics.
	if in[12]%4 == 0 {
		w.u8(0xdd)
		var mk any = words
		errsByAny := map[any]bool{}
		errsByAny[mk] = true
	}
}

// Mode 5: heap cost of closures capturing maps and loop defers saving
// maps, counted until the heap runs out (on chain) or n values are made
// (natively).

func touch(m map[uint64]uint64, k uint64) { m[k] = m[k] + 1 }

func envs(out []byte, in []byte) {
	n := int(in[1]) | int(in[2])<<8
	kind := in[3]
	shared := map[uint64]uint64{0: 0} // 24-byte header + 8 slots of 24 bytes, captured below
	var keep func() int
	var c Counter = Counter{}
	for j := 0; j < n; j++ {
		switch kind {
		case 0:
			m := make(map[uint64]uint64)        // 8 (moved) + 24 (header)
			keep = func() int { return len(m) } // 16 (environment)
		case 1:
			keep = func() int { return len(shared) } // 16 (shared is moved once)
		case 2:
			defer touch(shared, 0) // a 32-byte record; key 0 exists, so no growth
		case 3:
			keep = c.Len // 8 + 8: a value-receiver method value of a map type
		case 4:
			keep = func() int { return 0 } // nothing
		}
		out[0], out[1] = byte(j+1), byte((j+1)>>8)
	}
	if keep != nil {
		out[3] = byte(keep())
	}
	out[2] = 1
}

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w := W{out: out}
	switch in[0] {
	case 0:
		captures(&w, in)
	case 1:
		values(&w, in)
	case 2:
		defers(&w, in)
	case 3:
		methods(&w, in)
	case 4:
		registry(&w, in)
	case 5:
		envs(out, in)
		return 3
	default:
		return 7
	}
	return uint64(w.o)
}

// Process is the byte-slice entrypoint: s is the output, i the input.
func Process(s, i []byte) uint64 { return Run(s, i) }
