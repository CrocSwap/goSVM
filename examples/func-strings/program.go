// Package funcstrings is an ordinary-Go oracle for function values combined
// with strings and error values in the on-chain subset: closures capturing
// strings by reference, function values taking and returning strings and
// errors, defer with string operands and deferred closures changing named
// string and error results, method values and method expressions of types
// with Error() string, function values in interfaces next to errors, a named
// function type implementing error, and the heap cost of closures capturing
// strings. The same source runs natively and through goSVM; tests compare
// output bytes and status (or a panic) for every vector. See
// docs/GO_FUNCTIONS.md and docs/GO_STRINGS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package funcstrings

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

// str writes len(s) as one byte, then at most 40 of its bytes.
func (w *W) str(s string) {
	w.u8(uint64(len(s)))
	for j := 0; j < len(s) && j < 40; j++ {
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

// Mode 0: closures capturing strings by reference.

// builder returns two closures sharing one captured string.
func builder(prefix string) (func(string), func() string) {
	s := prefix
	add := func(t string) { s += t }
	get := func() string { return s }
	return add, get
}

// tagger captures a parameter and a local string; each call sees changes.
func tagger(tag string, sep byte) func(string) string {
	n := 0
	return func(v string) string {
		n = n + 1
		return tag + string(rune('0'+n%10)) + string(rune(sep)) + v
	}
}

func captures(w *W, in []byte) {
	a := string(in[2 : 2+int(in[1])%6])
	add, get := builder(a)
	add("-")
	add(string(in[10:12]))
	w.str(get())
	add("")
	w.str(get())
	// The enclosing function sees a change made through a closure.
	s := "x"
	set := func(v string) { s = v + s }
	set(string(in[12:13]))
	set("é")
	w.str(s)
	s = "reset"
	w.str(func() string { return s + "!" }())
	t := tagger(string(in[13:15]), in[15])
	w.str(t("a"))
	w.str(t(string(in[16:17])))
	// Nested literals capture through the enclosing literal.
	outer := func(x string) func() string {
		y := x + x
		return func() string { y += "."; return y + a }
	}
	inner := outer(string(in[17:18]))
	inner()
	w.str(inner())
	// Go 1.22: each iteration's closure captures its own variable.
	var fs []func() string
	for i, r := range string(in[20:24]) {
		fs = append(fs, func() string { return string(rune('a'+i)) + string(r) })
	}
	for _, f := range fs {
		w.str(f())
	}
	// Captured strings sliced in a closure: Go panics out of range.
	lo := int(in[4]) % 8
	cut := func() string { return a[lo:] }
	w.str(cut())
}

// Mode 1: function values taking and returning strings and errors.

// ParseErr is an error through a pointer receiver.
type ParseErr struct {
	At  int
	Msg string
}

func (e *ParseErr) Error() string { return "parse: " + e.Msg + " at " + string(rune('0'+e.At%10)) }

// Code is an error through a value receiver of a named integer.
type Code uint8

func (c Code) Error() string { return "code " + string(rune('A'+c%26)) }

// Step is a named function type over strings and errors.
type Step func(string) (string, error)

func upper(s string) (string, error) {
	b := []byte(s)
	for j := range b {
		if b[j] >= 'a' && b[j] <= 'z' {
			b[j] = b[j] - 32
		}
	}
	return string(b), nil
}

func nonEmpty(s string) (string, error) {
	if s == "" {
		return "", &ParseErr{At: 0, Msg: "empty"}
	}
	return s, nil
}

func ascii(s string) (string, error) {
	for i, r := range s {
		if r >= 0x80 {
			return "", &ParseErr{At: i, Msg: "non-ascii"}
		}
	}
	return s, nil
}

// pipeline applies each step, stopping at the first error.
func pipeline(s string, steps []Step) (string, error) {
	for _, st := range steps {
		var err error
		s, err = st(s)
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

func suffix(t string) Step {
	return func(s string) (string, error) {
		if len(s)+len(t) > 12 {
			return s, Code(len(s))
		}
		return s + t, nil
	}
}

// MapS is a generic map over a slice with a function value.
func MapS[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func values(w *W, in []byte) {
	n := int(in[1]) % 7
	s := string(in[2 : 2+n])
	steps := []Step{nonEmpty, ascii, upper, suffix(string(in[10:12]))}
	if in[3]%2 == 0 {
		steps = steps[:3]
	}
	r, err := pipeline(s, steps)
	w.str(r)
	w.u8(flag(err == nil))
	if err != nil {
		w.str(err.Error())
		if pe, ok := err.(*ParseErr); ok {
			w.u8(uint64(pe.At))
		}
		switch e := err.(type) {
		case Code:
			w.u8(uint64(e))
		case *ParseErr:
			w.str(e.Msg)
		}
	}
	lens := MapS([]string{"a", s, s + s}, func(x string) int { return len(x) })
	for _, l := range lens {
		w.u8(uint64(l))
	}
	errs := MapS([]byte{in[4], in[5]}, func(b byte) error {
		if b%3 == 0 {
			return nil
		}
		return Code(b)
	})
	for _, e := range errs {
		w.u8(flag(e == nil))
	}
	var f func(string) string
	if in[6]%4 != 0 {
		f = func(x string) string { return "<" + x + ">" }
	}
	// A nil function value is called after its argument is evaluated.
	arg := func() string { w.u8(0xaa); return s }
	w.str(f(arg()))
	var check func(string) error = func(x string) error {
		if len(x) > 3 {
			return &ParseErr{At: len(x), Msg: x[:3]}
		}
		return nil
	}
	e := check(s)
	w.u8(flag(e == nil))
	if e != nil {
		w.str(e.Error())
	}
}

// Mode 2: defer with strings, named string and error results.

// trace returns its argument after appending to a log through deferred
// closures; deferred calls see the value of named results.
func trace(log *string, v string) (out string) {
	defer func() { *log += "[" + out + "]" }()
	defer func() { out = out + "!" }()
	out = v
	return out + "?"
}

// wrap turns a failing step into an error in a deferred closure.
func wrap(s string, fail bool) (res string, err error) {
	defer func() {
		if err != nil {
			res = "failed:" + res
			err = &ParseErr{At: len(res), Msg: res}
		}
	}()
	res = s
	if fail {
		return s + "#", Code(len(s))
	}
	return
}

// argsAtDefer: deferred arguments are evaluated at the defer statement.
func argsAtDefer(log *string, s string) {
	defer appendTo(log, "first:"+s)
	s = s + "+"
	defer appendTo(log, "second:"+s)
	s = "changed"
}

func appendTo(log *string, s string) { *log += s + ";" }

// loopDefer defers a call with a string argument in a loop (heap records).
func loopDefer(log *string, s string) (count int) {
	for i, r := range s {
		defer appendTo(log, string(r)+string(rune('0'+i%10)))
		count = count + 1
	}
	return
}

// methodDefer defers method values bound to string-holding receivers.
func methodDefer(log *string, e *ParseErr, c Code) {
	defer appendTo(log, e.Error())
	f := c.Error
	defer func() { *log += f() }()
	e.Msg = "later"
	c = c + 1
}

func defers(w *W, in []byte) {
	log := ""
	w.str(trace(&log, string(in[2:4])))
	w.str(log)
	r, err := wrap(string(in[4:6]), in[1]%2 == 0)
	w.str(r)
	w.u8(flag(err == nil))
	if err != nil {
		w.str(err.Error())
	}
	r2, err2 := wrap("", false)
	w.str(r2)
	w.u8(flag(err2 == nil))
	log = ""
	argsAtDefer(&log, string(in[6:8]))
	w.str(log)
	log = ""
	w.u8(uint64(loopDefer(&log, string(in[8:8+int(in[3])%6]))))
	w.str(log)
	log = ""
	methodDefer(&log, &ParseErr{At: 1, Msg: string(in[14:16])}, Code(in[16]))
	w.str(log)
	// A deferred nil function value aborts when the function returns.
	if in[12]%5 == 0 {
		func() {
			var f func(string)
			defer f("x")
			w.u8(0xbb)
		}()
	}
}

// Mode 3: method values and method expressions of error types.

// Pair is an error through a value receiver of a struct holding strings.
type Pair struct {
	A, B string
}

func (p Pair) Error() string { return p.A + "/" + p.B }

func (p *Pair) SetA(s string) { p.A = s }

// Label is a named string with methods.
type Label string

func (l Label) Error() string        { return "label " + string(l) }
func (l Label) Wrap(s string) string { return string(l) + s + string(l) }
func (l Label) Str() string          { return string(l) }

func methods(w *W, in []byte) {
	p := Pair{A: string(in[1:3]), B: "b"}
	f := p.Error // binds a copy of p now
	p.A = "changed"
	w.str(f())
	pp := &p
	g := pp.Error // copies *pp now
	set := pp.SetA
	set(string(in[3:4]))
	w.str(g())
	w.str(p.Error())
	h := (*Pair).Error // dereferences its argument when called
	w.str(h(pp))
	k := Pair.Error
	w.str(k(Pair{A: "x", B: string(in[4:5])}))
	l := Label(string(in[5:7]))
	le := l.Error
	lw := l.Wrap
	l = "z"
	w.str(le())
	w.str(lw("-"))
	w.str(Label.Wrap(l, string(in[7:8])))
	c := Code(in[8])
	ce := c.Error
	w.str(ce())
	errs := []func() string{f, le, ce, Code(in[9]).Error}
	for _, e := range errs {
		w.str(e())
	}
	// Binding a value-receiver method value through a nil pointer: Go
	// panics when the value is made.
	var np *Pair
	if in[10]%4 == 0 {
		w.u8(0xcc)
		bad := np.Error
		w.str(bad())
	}
	// A pointer-receiver method value of a nil pointer binds; calling the
	// error method expression through it panics.
	var pe *ParseErr
	pm := pe.Error
	if in[11]%4 == 0 {
		w.u8(0xdd)
		w.str(pm())
	}
}

// Mode 4: function values in interfaces next to errors.

// ErrFunc is a named function type that implements error.
type ErrFunc func() string

func (f ErrFunc) Error() string { return "errfunc:" + f() }

func lookup(b byte) error {
	switch b % 5 {
	case 0:
		return nil
	case 1:
		return Code(b)
	case 2:
		s := string(rune('a' + b%26))
		return ErrFunc(func() string { return s + s })
	case 3:
		return Label("L")
	}
	var f ErrFunc // a non-nil error holding a nil ErrFunc
	return f
}

func ifaces(w *W, in []byte) {
	for k := 0; k < 3; k++ {
		err := lookup(in[1+k])
		w.u8(flag(err == nil))
		if err == nil {
			continue
		}
		switch e := err.(type) {
		case ErrFunc:
			w.u8(flag(e == nil))
			if e != nil {
				w.str(e.Error())
				w.str(e())
			}
		case Code:
			w.str(e.Error())
		default:
			w.str(err.Error())
		}
	}
	var a any = func(s string) error {
		if s == "" {
			return Code(0)
		}
		return nil
	}
	if f, ok := a.(func(string) error); ok {
		w.u8(flag(f("") == nil))
		w.u8(flag(f(string(in[5:6])) == nil))
	}
	_, isStr := a.(func(string) string)
	w.u8(flag(isStr))
	var b any = Label(string(in[6:8])).Wrap
	switch v := b.(type) {
	case func(string) string:
		w.str(v("."))
	case func(string) error:
		w.u8(1)
	}
	var e1 error = ErrFunc(func() string { return "x" })
	var e2 error = Code(in[8])
	w.u8(flag(e1 == nil))
	w.u8(flag(e2 == Code(in[8])))
	// Comparing two errors holding function values of the same dynamic
	// type panics: ErrFunc is not comparable.
	if in[9]%3 == 0 {
		w.u8(0xee)
		var e3 error = ErrFunc(func() string { return "y" })
		w.u8(flag(e1 == e3))
	}
	// Calling Error on a nil ErrFunc calls a nil function value.
	if in[10]%3 == 0 {
		w.u8(0xef)
		w.str(lookup(4).Error())
	}
	// A failed assertion to a function type panics.
	if in[11]%4 == 0 {
		w.str(b.(func(string) error)("z").Error())
	}
}

// Mode 5: heap cost of closures capturing strings, counted until the heap
// runs out (on chain) or n values are made (natively).

func noteS(s string) {}

func envs(out []byte, in []byte) {
	n := int(in[1]) | int(in[2])<<8
	kind := in[3]
	a := "fixed" // a literal: rodata, but captured below, so 16 bytes
	var keep func() string
	for j := 0; j < n; j++ {
		switch kind {
		case 0:
			s := a // 16 bytes moved to the heap, 16 for the environment
			keep = func() string { return s }
		case 1:
			keep = func() string { return a } // 16 bytes (a is moved once)
		case 2:
			s := a
			keep = func() string { // 16 + 24
				s += "" // one non-empty operand: no allocation
				if len(a) == 0 {
					return a
				}
				return s
			}
		case 3:
			defer noteS(a) // a 32-byte record
		case 4:
			keep = Label(a).Str // 8 + 16
		case 5:
			keep = func() string { return "lit" } // nothing
		}
		out[0], out[1] = byte(j+1), byte((j+1)>>8)
	}
	if keep != nil {
		out[3] = byte(len(keep()))
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
		ifaces(&w, in)
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
