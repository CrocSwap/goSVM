// Package log records the order in which package-level variables and init
// functions run, for examples/global-values. It has no imports, so Go
// initializes it first.
package log

// Trace is the initialization record: one byte per mark, in order.
var Trace []byte

// Steps counts marks; Last is the last mark.
var Steps uint64
var Last byte

// Mark appends b to Trace and returns it.
func Mark(b byte) byte {
	Trace = append(Trace, b)
	Steps++
	Last = b
	return b
}

func init() { Mark('L') }
