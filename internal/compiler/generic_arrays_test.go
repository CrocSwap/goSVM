package compiler

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	target "gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// Arrays nested in generic type arguments and in fields of instantiated
// generic structs (CrocSwap/goSVM#8): Pair[uint32, [][32]byte] and friends,
// where the array type is reached through a slice, pointer or map. Each
// program runs natively (go build of the same source), as host C and on
// SBF for both targets, with the formPrograms harness
// (reallibs_forms_test.go).
var genericArrayPrograms = map[string]string{
	"generic-array-type-args": `package main

type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

type Key [32]byte

// countKeys is the helper from CrocSwap/goSVM#8.
func countKeys(p Pair[uint32, [][32]byte]) uint64 { return uint64(len(p.Value)) }

// Find looks a key up in a sorted []Pair, the encoding of a Borsh
// BTreeMap<u32, BTreeSet<[u8; 32]>>.
func Find[K comparable, V any](ps []Pair[K, V], k K) (V, bool) {
	for _, p := range ps {
		if p.Key == k {
			return p.Value, true
		}
	}
	var zero V
	return zero, false
}

func Id[T any](x T) T { return x }

func First[T any](xs []T) T { return xs[0] }

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	keys := make([][32]byte, 1)
	keys[0][0] = in[0]
	j = put(o, j, countKeys(Pair[uint32, [][32]byte]{Key: 1, Value: keys}))
	sets := []Pair[uint32, [][32]byte]{
		{Key: uint32(in[1] % 4), Value: [][32]byte{{in[2]}, {31: in[3]}}},
		{Key: 9, Value: make([][32]byte, 3)},
	}
	sets[1].Value[2][5] = in[4]
	if v, ok := Find(sets, uint32(in[5]%4)); ok {
		j = put(o, j, uint64(len(v))|uint64(v[0][0])<<8|uint64(v[1][31])<<16)
	}
	if v, ok := Find(sets, 9); ok {
		v[0] = v[2] // the slice aliases sets[1].Value
		j = put(o, j, uint64(sets[1].Value[0][5]))
	}
	if sets[0].Value[0] == sets[0].Value[1] {
		j = put(o, j, 1)
	}
	// Pointers, maps, named arrays and nested slices of arrays.
	ptrs := Pair[uint8, []*[4]byte]{Value: []*[4]byte{{in[6]}, nil}}
	ptrs.Value[1] = ptrs.Value[0]
	ptrs.Value[1][3] = in[7]
	j = put(o, j, uint64(ptrs.Value[0][0])|uint64(ptrs.Value[0][3])<<8)
	m := Pair[uint8, map[uint8][][2]uint16]{Value: map[uint8][][2]uint16{}}
	m.Value[in[8]%3] = append(m.Value[in[8]%3], [2]uint16{uint16(in[9]) << 8, 7})
	j = put(o, j, uint64(len(m.Value))|uint64(m.Value[in[8]%3][0][0])<<8)
	named := Pair[uint32, []Key]{Value: []Key{{in[10]}}}
	j = put(o, j, uint64(First(named.Value)[0]))
	deep := Pair[uint32, [][][2]uint8]{Value: [][][2]uint8{{{in[11], 1}}, {}}}
	deep.Value[1] = append(deep.Value[1], [2]uint8{2, in[12]})
	j = put(o, j, uint64(deep.Value[0][0][0])|uint64(deep.Value[1][0][1])<<8)
	// Nested generics: a slice of instances whose own argument holds arrays.
	nested := Pair[uint32, []Pair[uint8, [][32]byte]]{Value: []Pair[uint8, [][32]byte]{{Key: in[13], Value: keys}}}
	nested.Value[0].Value[0][1] = in[14] // keys is shared
	j = put(o, j, uint64(nested.Value[0].Key)|uint64(keys[0][1])<<8)
	// Inferred type arguments and function types holding arrays.
	ids := Id([][4]byte{{in[15]}, {1, 2, 3, in[16]}})
	j = put(o, j, uint64(ids[1][3])|uint64(First(ids)[0])<<8)
	fs := Pair[uint8, func([][2]byte) [2]byte]{Value: func(xs [][2]byte) [2]byte { return xs[len(xs)-1] }}
	j = put(o, j, uint64(fs.Value([][2]byte{{in[17], in[18]}})[1]))
	// Bounds traps inside the nested arrays, after the writes above.
	i := int(in[19] % 4)
	switch in[20] % 4 {
	case 0:
		j = put(o, j, uint64(sets[1].Value[i][0]))
	case 1:
		j = put(o, j, uint64(ptrs.Value[i%2*2][0]))
	}
	return uint64(j)
}
`,
	"generic-struct-array-fields": `package main

type Number interface{ ~uint8 | ~uint16 | ~uint32 | ~uint64 }

// Box's fields hold arrays only through T: Box[uint16] writes [3]uint16,
// [4]uint16 and [5]uint16 nowhere else.
type Box[T Number] struct {
	V [][3]T
	P *[4]T
	M map[uint8][][5]T
	A [2][][2]T
}

type Row[T any] [4]T

// Grid uses Row[T] before Row[T] is instantiated on its own.
type Grid[T any] struct {
	Rows []Row[T]
}

func (b *Box[T]) Push(v T) {
	b.V = append(b.V, [3]T{v, v})
	b.V[len(b.V)-1][2] = v + v
}

func Last[T any](xs []T) T { return xs[len(xs)-1] }

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	var b Box[uint16]
	b.Push(uint16(in[0]))
	b.Push(uint16(in[1]) << 8)
	j = put(o, j, uint64(len(b.V))|uint64(b.V[1][0])<<8|uint64(b.V[0][2])<<24)
	j = put(o, j, uint64(Last(b.V)[2]))
	b.P = &[4]uint16{3: uint16(in[2])}
	j = put(o, j, uint64(b.P[3]))
	b.M = map[uint8][][5]uint16{in[3] % 2: {{4: uint16(in[4])}}}
	j = put(o, j, uint64(b.M[in[3]%2][0][4])|uint64(len(b.M[1-in[3]%2]))<<16)
	b.A[1] = append(b.A[1], [2]uint16{uint16(in[5]), 1})
	j = put(o, j, uint64(b.A[1][0][0])|uint64(len(b.A[0]))<<16)
	c := Box[uint64]{V: [][3]uint64{{uint64(in[6]) << 40}}}
	j = put(o, j, c.V[0][0])
	g := Grid[uint32]{Rows: make([]Row[uint32], 2)}
	g.Rows[1][3] = uint32(in[7]) << 24
	g.Rows[0] = g.Rows[1]
	j = put(o, j, uint64(g.Rows[0][3]))
	// Bounds and nil traps, after the writes above.
	i := int(in[8] % 4)
	switch in[9] % 4 {
	case 0:
		j = put(o, j, uint64(b.V[i][1]))
	case 1:
		var nb Box[uint8]
		if in[10]%2 == 0 {
			nb.P = &[4]uint8{1}
		}
		j = put(o, j, uint64(nb.P[0]))
	}
	return uint64(j)
}
`,
}

func genericArrayInputs(count int) [][]byte {
	rng := rand.New(rand.NewSource(20261019))
	var inputs [][]byte
	for j := 0; j < count; j++ {
		in := make([]byte, 128)
		rng.Read(in)
		if j < 16 {
			// Every trap selector, each with an in-range and an out-of-range index.
			in[8], in[9], in[19], in[20] = byte(j), byte(j/2), byte(j), byte(j/2)
		}
		inputs = append(inputs, in)
	}
	return inputs
}

func TestGenericArrayTypeArgsDifferential(t *testing.T) {
	inputs := genericArrayInputs(256)
	for name, src := range genericArrayPrograms {
		t.Run(name, func(t *testing.T) {
			want := formNative(t, src, inputs)
			c, err := CompileProgram(globalsProgram(unitFiles("program", formHostSource(src))))
			if err != nil {
				t.Fatal(err)
			}
			bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
			panics := 0
			for j, in := range inputs {
				got := runHost(t, bin, fmt.Sprintf("%x\n", in))
				if got != want[j] {
					t.Fatalf("input %d:\nC  %.300q\nGo %.300q", j, got, want[j])
				}
				if strings.HasPrefix(got, "P:") {
					panics++
				}
			}
			if panics == 0 || panics == len(inputs) {
				t.Fatalf("%d of %d inputs trap; want both outcomes", panics, len(inputs))
			}
			t.Logf("%d inputs (%d traps) match native Go", len(inputs), panics)
		})
	}
}

// Type arguments and generic struct fields holding arrays through slices,
// pointers, maps, arrays, function types and other instances are accepted
// (each was "array type was not declared before use"), and the storage
// rules inside them still apply.
func TestGenericArrayTypeArgsForms(t *testing.T) {
	const decls = `type Pair[K any, V any] struct { Key K; Value V }
type Box[T any] struct { V [][3]T }
type PB[T any] struct { P *[4]T }
type Row[T any] [4]T
type Grid[T any] struct { Rows []Row[T] }
type Key [8]byte
type S struct{ K [][32]byte }
func Id[T any](x T) T { return x }
`
	for _, use := range []string{
		"var p Pair[uint32, [][32]byte]; return uint64(len(p.Value))",
		"var p Pair[uint32, [][][4]byte]; return uint64(len(p.Value))",
		"var p Pair[uint32, []*[4]byte]; return uint64(len(p.Value))",
		"var p Pair[uint32, **[4]byte]; return uint64(p.Key)",
		"var p Pair[uint32, map[uint8][][4]byte]; return uint64(len(p.Value))",
		"var p Pair[uint32, [2][]*[4]byte]; return uint64(len(p.Value))",
		"var p Pair[uint32, []Key]; return uint64(len(p.Value))",
		"var p Pair[uint32, []S]; return uint64(len(p.Value))",
		"var p Pair[uint32, func([]*[4]byte) [][2]byte]; return uint64(p.Key)",
		"var p Pair[uint32, []Pair[uint8, [][32]byte]]; return uint64(len(p.Value))",
		"return uint64(len(Id([][4]byte{{1}})))",
		"var b Box[uint8]; return uint64(len(b.V))",
		"var b Box[[2]uint8]; return uint64(len(b.V))",
		"var b PB[uint64]; return uint64(len(b.P))",
		"var g Grid[uint16]; return uint64(len(g.Rows))",
	} {
		src := "package main\n" + decls + "func Process(s, in []byte) uint64 { " + use + " }\n"
		c, err := CompileProgram(globalsProgram(unitFiles("program", src)))
		if err != nil {
			t.Errorf("%s: %v", use, err)
			continue
		}
		hostCompile(t, c, DefaultHeapBytes, allocDriver, true)
	}
	for _, tc := range []struct{ use, want string }{
		{"var p Pair[uint32, [][2000]byte]; return uint64(len(p.Value))", "type argument [][2000]uint8 for V: array storage exceeds experimental 1024-byte per-value limit"},
		{"var p Pair[uint32, []*[0]byte]; return uint64(len(p.Value))", ""},
		{"var p Pair[uint32, [][0]byte]; return uint64(len(p.Value))", "type argument [][0]uint8 for V: slice elements must not contain zero-length arrays or empty structs"},
		{"var p Pair[uint32, *[]*[4]byte]; return uint64(p.Key)", "type argument *[]*[4]uint8 for V: pointer elements must be integer/bool scalars, value structs or scalar arrays"},
	} {
		src := "package main\n" + decls + "func Process(s, in []byte) uint64 { " + tc.use + " }\n"
		_, err := Check(globalsProgram(unitFiles("program", src)))
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.use, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.use, err, tc.want)
		}
	}
}

func TestGenericArrayTypeArgsSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	inputs := genericArrayInputs(64)
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			for name, src := range genericArrayPrograms {
				want := formNative(t, src, inputs)
				var cases []allocCase
				aborts := 0
				for j, in := range inputs {
					c := allocCase{name: fmt.Sprintf("%s-%02d", name, j), in: in, err: abortError, log: abortLog, noPanicLog: true}
					if strings.HasPrefix(want[j], "P:") {
						aborts++
					} else {
						_, hexOut, _ := strings.Cut(strings.TrimSpace(want[j]), ":")
						out, err := hex.DecodeString(hexOut)
						if err != nil {
							t.Fatal(err)
						}
						c.output = out
					}
					cases = append(cases, c)
				}
				runGlobalProgram(t, llvm, runner, profile, name, globalsProgram(unitFiles("program", formSBFSource(src))), cases, "")
				t.Logf("%s: %d cases (%d aborts with rollback) match native Go", name, len(cases), aborts)
			}
		})
	}
}
