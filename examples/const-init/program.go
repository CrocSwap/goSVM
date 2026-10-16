// Package constinit is an ordinary-Go oracle for compile-time evaluation
// of package initialization (docs/GO_GLOBALS.md#compile-time-evaluation):
// computed constants parsed from text, aliasing between package-level
// variables (two variables holding one object, slices and pointers into a
// global array, map values and slice elements that are other variables'
// objects), maps built and grown at initialization, closures and method
// values over heap state, sentinel errors compared by identity, interface
// values, strings built at initialization, and a variable whose
// initializer the compiler cannot evaluate (a map keyed by pointers), after
// which the remaining initialization runs at every invocation and changes
// variables evaluated earlier. The same source runs natively (a fresh
// process per vector) and through goSVM, with and without compile-time
// evaluation; tests compare output bytes and status (or a panic).
//
// Run(out, in) reads a mode from in[0] (0: dump, 1: mutate then dump, 2:
// faults, other: nothing) and writes results into out.
package constinit

import (
	"errors"

	"gosvm/examples/const-init/num"
	"gosvm/examples/const-init/reg"
)

// W writes little-endian values into the output.
type W struct {
	out []byte
	o   int
}

var w W

func put(v uint64, width int) {
	for j := range width {
		w.out[w.o+j] = byte(v >> (8 * j))
	}
	w.o = w.o + width
}

func u8(v uint64)  { put(v, 1) }
func u64(v uint64) { put(v, 8) }

func flag(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

func str(s string) {
	u8(uint64(len(s)))
	for j := 0; j < len(s); j++ {
		u8(uint64(s[j]))
	}
}

func u128(x *num.U128) {
	u64(x.Lo)
	u64(x.Hi)
}

// Values built from the imported packages' variables.
var (
	Text   = "max=" + num.Max.Hex()
	Sub    = Text[4:8]
	Bytes  = []byte(Text[:3])
	Empty  = []byte("")
	Total  = num.Sum.Add(num.E30)
	Counts = [3]uint64{reg.First, reg.Main.N, uint64(len(reg.Squares))}
	Lookup = reg.Ops["add"](40, 2)
)

// Node is a linked structure built at initialization.
type Node struct {
	V    uint64
	Next *Node
}

var List = &Node{1, &Node{2, &Node{3, nil}}}

// seen is keyed by pointers: its table depends on addresses, so the
// compiler leaves this initializer and every later one to run on each
// invocation (the rest above is evaluated at compile time).
var seen = map[*Node]uint8{List: 1, List.Next: 2}

// After is initialized at run time, after seen.
var After = reg.Main.N + uint64(len(seen))

// This init function runs at run time and changes variables that were
// evaluated at compile time: the image and the run-time steps compose.
func init() {
	reg.Bump(100)
	reg.Ops["mul"] = func(a, b uint64) uint64 { return a * b }
	delete(reg.Ops, "sub")
	reg.Row[0] = 7
	num.One.Hi = 5
	Counts[2] += uint64(len(seen))
	List.Next.V = 20
}

func Process(s, i []byte) uint64 { return Run(s, i) }

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w = W{out: out}
	switch in[0] {
	case 0:
		return dump()
	case 1:
		script(in[1:])
		return dump()
	case 2:
		return faults(in[1:])
	}
	return 0 // initialization only
}

// dump writes the state of every package's variables.
func dump() uint64 {
	u128(num.One)
	u128(num.Max)
	u128(num.Q64)
	u128(num.E30)
	u128(num.Sum)
	u128(&num.Half)
	u64(uint64(len(num.Table)))
	u64(uint64(cap(num.Table)))
	for i, x := range num.Table {
		if i < 6 {
			u128(x)
		}
	}
	u64(flag(num.Table[0] == num.One))
	u64(flag(reg.Named["one"] == num.One))
	u64(flag(reg.Named["e30"] == num.Table[3]))
	u64(uint64(len(reg.Named)))
	str(num.ErrEmpty.Error())
	u64(reg.Main.N)
	u64(flag(reg.Alias == reg.Main))
	u64(uint64(len(reg.Main.Log)))
	for i, d := range reg.Main.Log {
		if i < 8 {
			u8(d)
		}
	}
	u64(reg.Snapshot())
	u64(reg.First)
	u64(uint64(len(reg.Ops)))
	_, hasSub := reg.Ops["sub"]
	u64(flag(hasSub))
	if mul, ok := reg.Ops["mul"]; ok {
		u64(mul(6, 7))
	}
	u64(reg.Ops["add"](1, 2))
	str(reg.ErrMissing.Error())
	str(reg.ErrBoth.Error())
	u64(flag(errors.Is(reg.ErrBoth, num.ErrRange)))
	u64(flag(errors.Is(reg.ErrBoth, reg.ErrMissing)))
	u64(flag(errors.Is(reg.ErrBoth, num.ErrSyntax)))
	var nf *reg.NotFound
	u64(flag(errors.As(reg.ErrBoth, &nf)))
	if nf != nil {
		str(nf.Key)
	}
	for _, err := range reg.Errors {
		str(err.Error())
	}
	for i := range reg.Grid {
		for j := range reg.Grid[i] {
			put(uint64(reg.Grid[i][j]), 2)
		}
	}
	u64(uint64(len(reg.Row)))
	u64(uint64(reg.Row[0]))
	u64(uint64(*reg.Cell))
	u64(uint64(reg.ByKey[reg.Key{A: 1, B: "x"}]))
	u64(uint64(reg.ByKey[reg.Key{A: 2, B: "y"}]))
	u64(uint64(reg.ByKey[reg.Key{A: 3, B: "z"}]))
	u64(uint64(len(reg.Squares)))
	for i := uint64(0); i < 25; i++ {
		u8(flag(reg.Squares[i]))
	}
	for _, b := range reg.Boxes {
		switch v := b.(type) {
		case uint8:
			u64(uint64(v))
		case string:
			str(v)
		case reg.Key:
			u64(uint64(v.A))
			str(v.B)
		case reg.Empty:
			str(v.Name())
		case *reg.Counter:
			u64(v.N)
			u64(flag(v == reg.Main))
		case num.U128:
			u128(&v)
		}
	}
	u64(flag(reg.Boxes[3] == any(reg.Empty{})))
	u64(flag(reg.Boxes[2] == any(reg.Key{A: 4, B: "z"})))
	str(reg.It.Name())
	for i, s := range reg.Words {
		if i < 6 {
			str(s)
		}
	}
	str(Text)
	str(Sub)
	u64(uint64(len(Bytes)))
	u64(uint64(cap(Bytes)))
	str(string(Bytes))
	u64(flag(Empty != nil))
	u64(uint64(len(Empty)))
	u128(Total)
	for _, c := range Counts {
		u64(c)
	}
	u64(Lookup)
	for n := List; n != nil; n = n.Next {
		u64(n.V)
	}
	u64(uint64(len(seen)))
	u64(uint64(seen[List.Next]))
	u64(After)
	return uint64(w.o)
}

// script mutates the variables as directed by in.
func script(in []byte) {
	for k := 0; k+2 < len(in) && k < 60; k += 3 {
		op, a, b := in[k]%8, uint64(in[k+1]), uint64(in[k+2])
		switch op {
		case 0:
			if f, ok := reg.Ops["add"]; ok {
				u64(f(a, b))
			}
			reg.Ops["sub"] = func(x, y uint64) uint64 { return x - y }
		case 1:
			u64(reg.Bump(a))
			u64(reg.Alias.N)
		case 2:
			u64(reg.Next())
		case 3:
			for j := uint64(0); j < b%40; j++ {
				reg.Squares[1000+a*64+j] = j%2 == 0
			}
			u64(uint64(len(reg.Squares)))
		case 4:
			num.Table = append(num.Table, num.Pow10(int(a%38)))
			reg.Row = append(reg.Row, uint16(b))
			reg.Words = append(reg.Words, Sub)
			u64(uint64(cap(num.Table)))
		case 5:
			*reg.Cell += uint16(a)
			reg.Row[b%4] = uint16(a)
			u64(uint64(reg.Grid[1][b%4]))
		case 6:
			num.One.Lo += a
			num.Half.Hi = b
			u128(reg.Named["one"])
		case 7:
			Text += Sub
			Bytes = append(Bytes, byte(a))
			delete(reg.Named, "max")
			reg.Named["new"] = num.Pow10(int(b % 38))
		}
	}
}

// faults panics as directed by in.
func faults(in []byte) uint64 {
	switch in[0] % 4 {
	case 0:
		u128(num.Table[in[1]%8]) // index out of range for 4..7
	case 1:
		u64(uint64(reg.Boxes[in[1]%6].(uint8))) // failed assertion unless 0
	case 2:
		if in[1]%2 == 0 {
			num.MustParse("0xzz")
		}
		u128(num.MustParse("0x" + string([]byte{'1', in[1]%16 + 'a'})))
	case 3:
		var nilCounter *reg.Counter
		if in[1]%3 == 0 {
			reg.Alias = nilCounter
		}
		u64(reg.Alias.N)
	}
	return 1
}
