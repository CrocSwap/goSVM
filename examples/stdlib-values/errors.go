package stdlibvalues

import (
	"errors"
	"strconv"
)

// ErrorsOps is the number of operations of the errors group.
const ErrorsOps = 4

// Sentinel errors.
var (
	ErrA = errors.New("a")
	ErrB = errors.New("b")
)

// codeErr is a comparable error with a value receiver.
type codeErr uint64

func (c codeErr) Error() string { return "code " + strconv.FormatUint(uint64(c), 10) }

// wrapErr wraps one error. Its Error does not call inner's (that would be
// recursion through the error interface, which goSVM rejects).
type wrapErr struct {
	msg   string
	inner error
}

func (e *wrapErr) Error() string { return "wrap " + e.msg }
func (e *wrapErr) Unwrap() error { return e.inner }

// multiErr wraps several errors (Unwrap() []error).
type multiErr struct{ errs []error }

func (e *multiErr) Error() string   { return "multi" }
func (e *multiErr) Unwrap() []error { return e.errs }

// listErr is an uncomparable error type (it holds a slice).
type listErr struct{ items []byte }

func (e listErr) Error() string { return "list " + string(e.items) }

// isErr matches codeErr targets with the same code mod 7.
type isErr struct{ code uint64 }

func (e *isErr) Error() string { return "is" }
func (e *isErr) Is(target error) bool {
	c, ok := target.(codeErr)
	return ok && uint64(c)%7 == e.code%7
}

// asErr sets *codeErr targets.
type asErr struct{ v uint64 }

func (e asErr) Error() string { return "as" }
func (e asErr) As(target any) bool {
	if p, ok := target.(*codeErr); ok {
		*p = codeErr(e.v)
		return true
	}
	return false
}

// buildErr runs a small stack program over p and returns the error on top
// (nil when the stack is empty) and the stack.
func buildErr(p []byte) (error, []error) {
	var stack []error
	top := func() error {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	for j := 0; j+1 < len(p); j += 2 {
		arg := p[j+1]
		switch p[j] % 9 {
		case 0:
			stack = append(stack, errors.New(string(rune('a'+arg%4))))
		case 1:
			stack = append(stack, codeErr(arg%10))
		case 2:
			switch arg % 4 {
			case 0:
				stack = append(stack, ErrA)
			case 1:
				stack = append(stack, ErrB)
			case 2:
				stack = append(stack, errors.ErrUnsupported)
			default:
				stack = append(stack, strconv.ErrSyntax)
			}
		case 3:
			e := &wrapErr{msg: string(rune('w' + arg%3)), inner: top()}
			if len(stack) > 0 {
				stack[len(stack)-1] = e
			} else {
				stack = append(stack, e)
			}
		case 4:
			k := int(arg%3) + 1
			if k > len(stack) {
				k = len(stack)
			}
			errs := append([]error(nil), stack[len(stack)-k:]...)
			if arg&8 != 0 {
				errs = append(errs, nil)
			}
			stack = append(stack[:len(stack)-k], &multiErr{errs: errs})
		case 5:
			stack = append(stack, listErr{items: []byte{'0' + arg%3}})
		case 6:
			stack = append(stack, &isErr{code: uint64(arg)})
		case 7:
			stack = append(stack, asErr{v: uint64(arg % 10)})
		case 8:
			if arg%5 == 0 {
				stack = append(stack, nil)
			} else if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return top(), stack
}

// asInto calls errors.As with a target of any pointer type; As panics when
// T is neither an interface nor an error type, or the pointer is nil.
func asInto[T any](err error, target *T) bool { return errors.As(err, target) }

func errorsGroup(w *W, v Input) {
	err, stack := buildErr(v.raw[8 : 8+2*(v.h0%24)])
	w.err(err)
	w.int(len(stack))
	switch v.op % ErrorsOps {
	case 0: // Is against sentinels, comparable, uncomparable and pointer targets
		targets := []error{nil, ErrA, ErrB, errors.ErrUnsupported, strconv.ErrSyntax, codeErr(v.h1 % 10),
			listErr{items: []byte{'0' + byte(v.h1%3)}}, &wrapErr{msg: "w"}, errors.New("a")}
		targets = append(targets, stack...)
		for _, t := range targets {
			w.flag(errors.Is(err, t))
		}
		w.err(errors.Unwrap(err))
		w.str(errors.ErrUnsupported.Error())
	case 1: // As into concrete, pointer, interface and any targets
		var c codeErr = 99
		w.flag(errors.As(err, &c))
		w.u64(uint64(c))
		var we *wrapErr
		w.flag(errors.As(err, &we))
		if we != nil {
			w.str(we.msg)
			w.err(we.inner)
		}
		var me *multiErr
		w.flag(errors.As(err, &me))
		if me != nil {
			w.int(len(me.errs))
		}
		var le listErr
		w.flag(errors.As(err, &le))
		w.str(string(le.items))
		var ie *isErr
		w.flag(errors.As(err, &ie))
		var u interface{ Unwrap() error }
		w.flag(errors.As(err, &u))
		if u != nil {
			w.err(u.Unwrap())
		}
		var x any
		w.flag(errors.As(err, &x))
		var ae asErr
		w.flag(errors.As(err, &ae))
		var e error
		w.flag(asInto(err, &e)) // vet rejects errors.As(err, &e) for e of type error
		w.flag(e == err) // panics when err holds an uncomparable listErr
	case 2: // As panics for a nil pointer or a type that is not an error
		w.u8(0x41)
		switch v.h1 % 3 {
		case 0:
			w.flag(asInto[codeErr](err, nil))
		case 1:
			var n uint64
			w.flag(asInto(err, &n))
		default:
			var s string
			w.flag(asInto(err, &s))
		}
	case 3: // Unwrap chains
		e := err
		for j := 0; j < 6; j++ {
			w.err(e)
			e = errors.Unwrap(e)
		}
	}
}
