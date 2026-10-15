// Package beta holds tables and function values; it imports only log.
package beta

import "gosvm/examples/global-values/log"

// Op is a binary operation.
type Op func(a, b uint64) uint64

func add(a, b uint64) uint64 { return a + b }
func sub(a, b uint64) uint64 { return a - b }
func mul(a, b uint64) uint64 { return a * b }

// Ops and Names are runtime-initialized (function values and strings are
// not plain data); Fib is never written (read-only data); Mutable is written
// by the entry package.
var Ops = []Op{add, sub, mul}
var Names = []string{"add", "sub", "mul"}
var Fib = [8]uint16{1, 1, 2, 3, 5, 8, 13, 21}
var Mutable = [4]uint32{9, 8, 7, 6}
var B = log.Mark('b')

// Default is a function-valued variable called as beta.Default(a, b).
var Default Op = add

// Swap replaces Default and returns the old one.
func Swap(op Op) Op {
	old := Default
	Default = op
	return old
}

// Two init functions in one file run in source order.
func init() { log.Mark('B') }
func init() { log.Mark('C'); Mutable[3] = Mutable[3] + uint32(B) }
