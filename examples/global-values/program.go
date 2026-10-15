// Package globalvalues is an ordinary-Go oracle for package-level variables
// and init functions in the on-chain subset: Go's initialization order
// (dependency order within a package, imported packages first in Go 1.21's
// package order, init functions in file and source order, blank and
// multiple-value initializers), variables of every kind (integers, bools,
// strings, arrays, structs, slices, maps, pointers, interfaces, function
// values, empty structs with methods), mutation that persists for the rest
// of the invocation, closures and method values over package variables,
// read-only tables, and combinations with maps, interfaces, strings and
// generics. The same source runs natively (a fresh process per vector, as
// every SBF invocation is fresh) and through goSVM; tests compare output
// bytes and status (or a panic). See docs/GO_GLOBALS.md.
//
// Run(out, in) reads a mode from in[0] and writes results into out.
package globalvalues

import (
	"gosvm/examples/global-values/alpha"
	"gosvm/examples/global-values/beta"
	"gosvm/examples/global-values/log"
	"gosvm/examples/global-values/zeta"
)

// W writes little-endian values into the output; the writer itself is a
// package-level variable holding the output slice.
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

// Initialization order within the package: r1 needs r3, which needs r4
// through compute; r2 has no dependencies. Go runs r4, r3, r1, r2.
var r1 = log.Mark('1') + r3
var r2 = log.Mark('2')
var r3 = compute()
var r4 = log.Mark('4')

func compute() byte { return log.Mark('3') + r4 }

// A blank initializer still runs; a multiple-value call sets two variables.
var _ = log.Mark('_')
var m1, m2 = two()

func two() (uint64, uint64) { log.Mark('m'); return 11, 22 }

// Values of imported packages are final: their init functions have run.
var seenCounter = alpha.Counter
var seenB = beta.Mutable[3]

// late is set by an init function after early copies it.
var late uint64 = 5
var early = late

// Closures: makeCounter's local lives on as the closure's environment.
var next = makeCounter(100)

func makeCounter(start uint64) func() uint64 {
	n := start
	return func() uint64 {
		n++
		return n
	}
}

// Errors and interfaces.
type Err struct{ msg string }

func (e *Err) Error() string { return e.msg }

type CodeErr struct{ code uint8 }

func (e CodeErr) Error() string { return "code" }

var ErrNotFound error = &Err{"not found"}
var ErrOther = &Err{"other"}
var ErrCode error = CodeErr{7}
var anything any = uint32(77)

// Maps.
var registry = map[string]uint64{"a": 1, "b": 2, "c": 3}
var handlers = map[uint8]func(uint64) uint64{
	1: func(x uint64) uint64 { return x + 1 },
	2: func(x uint64) uint64 { return x * 2 },
}
var nilMap map[uint8]uint8

// Function values: a nil one, and one taken from another package.
var nilFn func() uint64
var hook = zeta.Hook

// Strings: a constant (read-only data) and one built at initialization.
var greeting = "hello, " + "world"
var built string

// Pointers to package storage and to the heap.
var writable = [4]uint64{1, 2, 3, 4}
var wp = &writable
var view = writable[1:3]
var cell = new(uint32)

// Generic code in an initializer.
func Max[T uint8 | uint32 | int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

var biggest = Max[int64](-3, 9)

// deferred is changed by a deferred closure in an init function.
var deferred uint64

func init() {
	log.Mark('I')
	defer func() { deferred = deferred + 100 }()
	built = greeting + "!"
	late = 50
	*cell = 9
	deferred = 1
}

func Process(s, i []byte) uint64 { return Run(s, i) }

// Run dispatches on in[0].
func Run(out, in []byte) uint64 {
	w = W{out: out}
	switch in[0] {
	case 0:
		return dump()
	case 1:
		return script(in[1:])
	case 2:
		return tables(in[1:])
	case 3:
		return functions(in[1:])
	case 4:
		return errorsAndMaps(in[1:])
	case 5:
		return crossPackage(in[1:])
	case 6:
		return heap(in[1:])
	}
	return 0
}

// dump writes every variable's value after initialization.
func dump() uint64 {
	str(string(log.Trace))
	u64(log.Steps)
	u8(uint64(log.Last))
	u8(uint64(r1))
	u8(uint64(r2))
	u8(uint64(r3))
	u8(uint64(r4))
	u64(m1)
	u64(m2)
	u64(seenCounter)
	u64(uint64(seenB))
	u64(late)
	u64(early)
	u64(deferred)
	str(greeting)
	str(built)
	u64(uint64(*cell))
	u64(uint64(biggest))
	u64(uint64(zeta.Z))
	u64(zeta.Base)
	u64(uint64(zeta.Calls))
	u64(uint64(beta.B))
	for _, v := range beta.Mutable {
		u64(uint64(v))
	}
	u64(alpha.Counter)
	u64(alpha.S.N)
	for _, h := range alpha.S.Hist {
		u8(uint64(h))
	}
	u64(*alpha.S.Last)
	u64(uint64(len(alpha.S.Tags)))
	str(alpha.S.Tags[0])
	u8(uint64(alpha.A1))
	u8(uint64(alpha.A2))
	u64(flag(alpha.P == &alpha.S))
	u64(flag(alpha.S.Last == alpha.Q))
	u64(uint64(len(view)))
	u64(uint64(cap(view)))
	u64(view[0])
	u64(flag(wp == &writable))
	u64(flag(nilFn == nil))
	u64(flag(nilMap == nil))
	u64(uint64(len(registry)))
	u64(flag(ErrNotFound != nil))
	str(ErrNotFound.Error())
	str(ErrCode.Error())
	u64(uint64(anything.(uint32)))
	u64(uint64(BigEndian.Uint16([]byte{1, 2})))
	u64(uint64(LittleEndian.Uint16([]byte{1, 2})))
	u64(uint64(len(table)))
	u64(pows64[19])
	u64(uint64(len(names)))
	str(names[2])
	u64(uint64(zeroTable[3]))
	u64(flag(flags[0]))
	u64(uint64(negatives[1]))
	u64(config.Limit)
	u64(uint64(config.Shift))
	u64(flag(config.On))
	return 1
}
