package stdlibvalues

import "errors"

// LangOps is the number of operations of the language group: the two
// language features the standard library needed, variadic functions
// (errors.Join) and pointers to pointers (errors.As(err, &p) for a
// pointer-typed p).
const LangOps = 4

// sum is a variadic function; it also changes its slice, which a caller
// passing s... sees.
func sum(base uint64, xs ...uint64) uint64 {
	for i, x := range xs {
		base += x * uint64(i+1)
		xs[i] = base
	}
	return base
}

// count reports how many values it got and whether the slice is nil.
func count[T any](xs ...T) (int, bool) { return len(xs), xs == nil }

// Acc accumulates through a variadic method.
type Acc struct{ total uint64 }

func (a *Acc) Add(xs ...uint64) *Acc {
	for _, x := range xs {
		a.total += x
	}
	return a
}

// texts is variadic over an interface type.
func texts(sep string, errs ...error) string {
	s := ""
	for i, err := range errs {
		if i > 0 {
			s += sep
		}
		if err == nil {
			s += "<nil>"
		} else {
			s += err.Error()
		}
	}
	return s
}

// set stores through a pointer to a pointer.
func set(pp **uint64, p *uint64) { *pp = p }

// pick returns, through a pointer to a pointer, either a fresh local or the
// value it already holds.
func pick(pp **uint64, fresh bool, v uint64) *uint64 {
	if fresh {
		local := v
		set(pp, &local) // local moves to the heap: it outlives pick
	}
	return *pp
}

func langGroup(w *W, v Input) {
	in := v.raw
	switch v.op % LangOps {
	case 0: // variadic calls: no extra arguments, several, a spread slice
		w.u64(sum(v.x))
		w.u64(sum(v.x, v.y))
		w.u64(sum(1, uint64(in[8]), uint64(in[9]), uint64(in[10])))
		s := []uint64{uint64(in[11]), uint64(in[12]), uint64(in[13])}
		w.u64(sum(2, s...))
		for _, x := range s {
			w.u64(x) // sum changed the caller's elements
		}
		w.u64(sum(3, s[:v.h0%4]...))
		var none []uint64
		w.u64(sum(4, none...))
		n, isNil := count[uint64]()
		w.int(n)
		w.flag(isNil)
		n, isNil = count("a", v.a, v.b)
		w.int(n)
		w.flag(isNil)
		n, isNil = count([]string{}...)
		w.int(n)
		w.flag(isNil)
		var a Acc
		w.u64(a.Add().Add(v.x).Add(1, 2, uint64(in[14])).total)
	case 1: // variadic interface parameters and deferred variadic calls
		w.str(texts(",", errors.New(v.a), nil, ErrA))
		w.str(texts("|"))
		errs := []error{ErrB, codeErr(v.h0 % 10)}
		w.str(texts(v.b, errs...))
		total := uint64(0)
		func() {
			defer func() { total = sum(total, uint64(in[15])) }()
			defer sum(0, uint64(in[16]), uint64(in[17]))
			total = sum(v.y, uint64(in[18]))
		}()
		w.u64(total)
	case 2: // pointers to pointers
		x, y := v.x, v.y
		p := &x
		pp := &p
		**pp += 7
		w.u64(x)
		*pp = &y
		**pp++
		w.u64(y)
		w.u64(*p)
		var q *uint64
		w.u64(*pick(&q, true, v.x>>3))
		w.u64(*pick(&q, false, 0))
		r := pick(&q, in[8]&1 == 1, v.y)
		*r ^= 0xff
		w.u64(*q)
		ppp := &pp
		***ppp = 99
		w.u64(y)
	case 3: // a nil pointer to a pointer panics when dereferenced
		var pp **uint64
		if in[8]&1 == 1 {
			p := new(uint64)
			pp = &p
		}
		w.u8(0x50)
		w.u64(**pp)
	}
}
