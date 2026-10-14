// Package intvalues is an ordinary-Go oracle for the signed integer types
// and uint in the on-chain subset. ops.go (generated) applies every operator,
// comparison, conversion and shift-count type of every integer type, and
// named versions, in locals, parameters, results, fields, arrays and
// pointers; native tests run it with real Go, and the compiler tests compare
// the generated C and SBF programs with it.
//
// Process is the legacy entry: the state holds a, b and the result word; the
// instruction holds the operation code and a status that is returned after
// the result is stored (a nonzero status is a custom error).
package intvalues

func load(b []byte, o uint64) uint64 {
	v := uint64(0)
	for j := uint64(0); j < 8; j++ {
		v = v | uint64(b[o+j])<<(8*j)
	}
	return v
}

func store(b []byte, o uint64, v uint64) {
	for j := uint64(0); j < 8; j++ {
		b[o+j] = byte(v >> (8 * j))
	}
}

func Process(s, i []byte) uint64 {
	store(s, 16, Eval(load(i, 0), load(s, 0), load(s, 8)))
	return load(i, 8)
}
