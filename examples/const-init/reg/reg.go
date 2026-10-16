// Package reg builds registries at initialization: maps (of functions, of
// pointers aliasing other packages' variables, keyed by structs), closures
// over heap variables, method values, errors, interface values and tables
// that other variables alias.
package reg

import (
	"errors"

	"gosvm/examples/const-init/num"
)

// Op is a binary operation.
type Op func(a, b uint64) uint64

// Ops is a map of function values built by a literal and changed by an
// init function.
var Ops = map[string]Op{
	"add": func(a, b uint64) uint64 { return a + b },
	"sub": func(a, b uint64) uint64 { return a - b },
}

// Counter is mutable state shared through several variables.
type Counter struct {
	N   uint64
	Log []uint64
}

// Inc adds d to the counter and logs it.
func (c *Counter) Inc(d uint64) uint64 {
	c.N += d
	c.Log = append(c.Log, d)
	return c.N
}

// Value is a value-receiver method.
func (c Counter) Value() uint64 { return c.N }

var (
	Main  = &Counter{N: 1}
	Alias = Main     // the same object
	Bump  = Main.Inc // bound to Main (pointer receiver)
	// Snapshot binds a copy of *Main as it is now (value receiver).
	Snapshot = Main.Value
)

// next is a closure whose environment holds a heap-moved local.
var next = makeSeq(10)

func makeSeq(start uint64) func() uint64 {
	n := start
	return func() uint64 {
		n++
		return n
	}
}

// Next advances the sequence.
func Next() uint64 { return next() }

// First consumed one value of the sequence during initialization.
var First = Next()

// NotFound is an error type with a pointer receiver.
type NotFound struct{ Key string }

func (e *NotFound) Error() string { return "reg: " + e.Key + " not found" }

var (
	ErrMissing error = &NotFound{"missing"}
	ErrBoth          = errors.Join(ErrMissing, num.ErrRange)
	Errors           = []error{num.ErrEmpty, num.ErrSyntax, ErrMissing}
)

// Grid is a writable table; Row and Cell alias parts of it.
var (
	Grid [4][4]uint16
	Row  = Grid[1][:]
	Cell = &Grid[2][3]
)

func init() {
	for i := range Grid {
		for j := range Grid[i] {
			Grid[i][j] = uint16(i*10 + j)
		}
	}
	*Cell = 99
	Bump(4)
}

// Key is a struct map key.
type Key struct {
	A uint8
	B string
}

var ByKey = map[Key]uint32{{1, "x"}: 7, {2, "y"}: 8}

// Named maps names to values other variables also hold.
var Named = map[string]*num.U128{"one": num.One, "max": num.Max, "e30": num.E30}

// Squares grows a map during initialization (several table growths).
var Squares = map[uint64]bool{}

func init() {
	for i := uint64(0); i < 20; i++ {
		Squares[i*i] = true
	}
	delete(Squares, 4)
}

// Empty is a zero-size type with a method.
type Empty struct{}

// Name names it.
func (Empty) Name() string { return "empty" }

// Namer is implemented by Empty.
type Namer interface{ Name() string }

// Boxes holds interface values of several dynamic types.
var Boxes = []any{uint8(3), "text", Key{4, "z"}, Empty{}, Main, num.Half}

// It is an interface value holding a zero-size value.
var It Namer = Empty{}

// Words is a string table built with concatenation and conversions.
var Words = []string{"alpha", "be" + "ta", string([]byte{'g', 'a', 'm', 'm', 'a'}), num.Max.Hex()}
