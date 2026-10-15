package stdlibvalues

import (
	"errors"
	"strconv"
)

// StrconvOps is the number of operations of the strconv group.
const StrconvOps = 7

// numErr writes a parse error: its text, what errors.Is and errors.As find
// in it and the fields of the *NumError.
func (w *W) numErr(err error) {
	w.err(err)
	w.flag(errors.Is(err, strconv.ErrRange))
	w.flag(errors.Is(err, strconv.ErrSyntax))
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		w.str(ne.Func)
		w.str(ne.Num)
		w.str(ne.Err.Error())
		w.flag(ne.Unwrap() == ne.Err)
	}
}

// bases are the bases parse and format calls use: valid ones, 0 (parse
// only: prefixes) and invalid ones (parse errors, format panics).
var bases = [...]int{10, 16, 2, 8, 36, 0, 3, 7, 1, 37, -1, 64}

// bitSizes are the bit sizes parse calls use, including invalid ones.
var bitSizes = [...]int{64, 32, 8, 16, 0, 1, 63, 65, -8, 7}

func strconvGroup(w *W, v Input) {
	a := v.a
	base := bases[v.h0%len(bases)]
	bitSize := bitSizes[v.h1%len(bitSizes)]
	switch v.op % StrconvOps {
	case 0: // ParseInt and ParseUint
		i, err := strconv.ParseInt(a, base, bitSize)
		w.u64(uint64(i))
		w.numErr(err)
		u, err := strconv.ParseUint(a, base, bitSize)
		w.u64(u)
		w.numErr(err)
	case 1: // Atoi and ParseBool
		n, err := strconv.Atoi(a)
		w.int(n)
		w.numErr(err)
		ok, err := strconv.ParseBool(a)
		w.flag(ok)
		w.numErr(err)
		w.int(strconv.IntSize)
	case 2: // formatting with a valid base (2 to 36)
		b := 2 + v.h0%35
		w.str(strconv.FormatInt(int64(v.x), b))
		w.str(strconv.FormatUint(v.y, b))
		w.str(strconv.FormatInt(int64(v.n), 10))
		w.str(strconv.Itoa(int(v.x)))
		w.str(strconv.Itoa(v.m))
		w.str(strconv.FormatBool(v.n&1 == 1))
		w.bs(strconv.AppendInt(fresh(v.b), int64(v.y), b))
		w.bs(strconv.AppendUint(nil, v.x, b))
		w.bs(strconv.AppendUint(fresh(v.b), uint64(v.m&0x7f), 10))
		w.bs(strconv.AppendBool(fresh(v.b), v.m&1 == 1))
	case 3: // formatting with an invalid base panics
		b := bases[v.h0%len(bases)]
		w.u8(0x46)
		w.str(strconv.FormatUint(v.x, b))
		w.str(strconv.FormatInt(int64(v.y), b))
		w.bs(strconv.AppendInt(nil, int64(v.n), b))
	case 4: // round trips through the formatted text
		b := 2 + v.h0%35
		s := strconv.FormatInt(int64(v.x), b)
		i, err := strconv.ParseInt(s, b, 64)
		w.u64(uint64(i))
		w.err(err)
		s = strconv.FormatUint(v.y, b)
		u, err := strconv.ParseUint(s, b, 64)
		w.u64(u)
		w.err(err)
		u, err = strconv.ParseUint(s, 0, bitSize)
		w.u64(u)
		w.numErr(err)
	case 5: // errors.As and Unwrap through a nested *NumError
		_, err := strconv.ParseInt(a, base, bitSize)
		outer := &strconv.NumError{Func: "Outer", Num: v.b, Err: err}
		if err == nil {
			outer.Err = strconv.ErrRange
		}
		var e error = outer
		w.err(e)
		w.flag(errors.Is(e, strconv.ErrSyntax))
		w.flag(errors.Is(e, strconv.ErrRange))
		w.err(errors.Unwrap(e))
		w.err(errors.Unwrap(errors.Unwrap(e)))
		var ne *strconv.NumError
		w.flag(errors.As(e, &ne))
		w.flag(ne == outer)
	case 6: // a NumError with another kind of cause, or none (Error panics)
		var cause error
		switch v.h2 % 4 {
		case 0:
			cause = codeErr(v.x)
		case 1:
			cause = &wrapErr{msg: v.b, inner: strconv.ErrRange}
		case 2:
			cause = &strconv.NumError{Func: "Inner", Num: v.a, Err: codeErr(v.y)}
		}
		e := &strconv.NumError{Func: v.a, Num: v.b, Err: cause}
		w.u8(0x4e)
		w.flag(errors.Is(e, codeErr(v.x)))
		w.str(e.Error())
	}
}
