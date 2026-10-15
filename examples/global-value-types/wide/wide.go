// Package wide holds the value types the entry package aliases, and
// package-level variables of composite types that the entry package reads
// and mutates in place through pkg.V selectors.
package wide

// Int is a uint256-style word array; Pair a two-field struct; Cell has a
// blank field, which == ignores.
type Int [4]uint64

type Pair struct {
	K uint32
	V uint64
}

type Cell struct {
	P Pair
	_ uint8
	W [2]uint16
}

// Origin is read-only data; Table and Grid are written in place by the
// entry package; Counter is written by Bump.
var Origin = Pair{K: 1, V: 2}
var Table = [4]Pair{{K: 1, V: 2}, {K: 3, V: 4}, {K: 1, V: 2}, {K: 5}}
var Grid [3][3]uint16
var Counter Int

// Steps counts calls of Checked, including those of initializers.
var Steps uint64

// Checked panics when x is above 64; the entry package's initializers call
// it with small values, and Run with input-dependent ones.
func Checked(x uint64) uint64 {
	Steps++
	if x > 64 {
		panic(Limit(x))
	}
	return x
}

// Limit is the named type Checked panics with.
type Limit uint16

// Bump adds x to Counter as a 256-bit value.
func Bump(x uint64) {
	c := x
	for j := range Counter {
		s := Counter[j] + c
		if s < c {
			c = 1
		} else {
			c = 0
		}
		Counter[j] = s
	}
}

// Seed is computed by an initializer that calls Checked.
var Seed = Pair{K: uint32(Checked(9)), V: Checked(40)}
