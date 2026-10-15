package compiler

import (
	"fmt"
	"go/types"
	"math/rand"
	"sort"
	"strings"
	"testing"

	mixedvalues "gosvm/examples/mixed-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// Combinations of signed integers, generics and allocation: generic code that
// allocates and whose values escape, and signed lengths, indexes and bounds
// on allocated slices. See docs/GO_ALLOCATION.md, GO_GENERICS.md and
// GO_INTEGERS.md.

const mixedPrefix = `package p
type Signed interface{ ~int8 | ~int16 | ~int32 | ~int64 | ~int }
type Node[T any] struct {
	Next *Node[T]
	Val  T
}
type Vec[T any] struct{ items []T }
func (v *Vec[T]) Add(x T) { v.items = append(v.items, x) }
func (v Vec[T]) Ref() *Vec[T] { return &v }
func Map[T Signed](s []T, k T) []T { out := make([]T, len(s)); for i, v := range s { out[i] = v * k }; return out }
func Box[T any](v T) *T { return &v }
func NewOf[T any]() *T { return new(T) }
func Ptrs[T any](s []T) []*T { var ps []*T; for _, v := range s { ps = append(ps, &v) }; return ps }
func Push[T any](head *Node[T], v T) *Node[T] { n := Node[T]{Next: head, Val: v}; return &n }
func Lit[T any](a, b T) []T { return []T{a, b} }
func Id[T any](x T) T { return x }
func Process(s, i []byte) uint64 {
`

func TestMixedAcceptedForms(t *testing.T) {
	for name, body := range map[string]string{
		"generic make":              `w := Map([]int8{-1, 2}, -3); s[0] = byte(w[0])`,
		"generic append field":      `var v Vec[int32]; v.Add(-1); v.Add(2); s[0] = byte(len(v.items))`,
		"generic receiver escape":   `v := Vec[int64]{}; r := v.Ref(); r.Add(5); s[0] = byte(len(r.items))`,
		"generic parameter escape":  `p, q := Box(int16(-2)), Box(uint8(3)); s[0] = byte(*p) + *q`,
		"generic new":               `p := NewOf[[4]int16](); p[1] = -1; s[0] = byte(p[1])`,
		"generic loop escape":       `ps := Ptrs([]int{-1, 2, 3}); *ps[0] = 4; s[0] = byte(*ps[0] + *ps[1])`,
		"generic node escape":       `h := Push(Push[int8](nil, -1), 2); s[0] = byte(h.Next.Val)`,
		"generic slice literal":     `w := Lit[int64](-1, 1); s[0] = byte(len(w))`,
		"generic slice of slices":   `w := Lit([]int8{-1}, nil); w[1] = append(w[1], 3); s[0] = byte(w[1][0])`,
		"generic pointer argument":  `x := int32(-5); p := Id(&x); *p = 1; s[0] = byte(x)`,
		"generic slice argument":    `w := Id([]int16{-1}); w = append(w, 2); s[0] = byte(len(w))`,
		"generic pointer slice":     `w := Lit[*int8](nil, nil); w[0] = new(int8); s[0] = byte(len(w))`,
		"signed make length":        `n := int8(i[0]); w := make([]int16, n); s[0] = byte(len(w))`,
		"signed make capacity":      `w := make([]int64, int32(i[0]), int64(i[1])); s[0] = byte(cap(w))`,
		"signed make int":           `w := make([]int, len(i)-1); s[0] = byte(len(w))`,
		"signed index":              `w := make([]int32, 4); k := int8(i[0]); w[k] = -1; s[0] = byte(w[k])`,
		"signed slice bounds":       `w := []int16{1, 2, 3}; lo, hi := int8(i[0]), int64(i[1]); v := w[lo:hi:3]; s[0] = byte(len(v))`,
		"signed copy result":        `w := make([]int8, 4); n := copy(w, []int8{-1, -2}); if n > 1 { s[0] = byte(w[1]) }`,
		"signed append":             `var w []int8; w = append(w, -128, 127); w = append(w, w...); s[0] = byte(w[3])`,
		"signed range":              `for k, v := range []int64{-1, 2} { s[k] = byte(v) }`,
		"signed division of slices": `w := []int32{-7, 7}; d := int32(i[0]); s[0] = byte(w[0] / d)`,
		"signed shift of slices":    `w := []int16{-7}; k := int8(i[0]); s[0] = byte(w[0] >> k)`,
	} {
		t.Run(name, func(t *testing.T) {
			source := mixedPrefix + body + "\nreturn 0 }\n"
			if got := allocMessage(t, source); got != "" {
				t.Fatalf("rejected: %s", got)
			}
			c, err := Compile("alloc.go", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			hostCompile(t, c, DefaultHeapBytes, "", true)
		})
	}
}

func TestMixedRejections(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"generic zero-size elements": {`_ = Lit[[0]byte]([0]byte{}, [0]byte{})`,
			"instantiation Lit[[0]uint8]: alloc.go:15:1: slice elements must not contain zero-length arrays or empty structs"},
		"generic box of slice": {`b := Box([]int8{1}); _ = b`,
			"instantiation Box[[]int8]: alloc.go:11:1: pointer elements must be integer/bool scalars, value structs or scalar arrays"},
		"generic compound append": {`var v Vec[int8]; v.Add(1); v.items[0] += 1`, "compound assignment unsupported"},
	} {
		t.Run(name, func(t *testing.T) {
			source := mixedPrefix + c.body + "\nreturn 0 }\n"
			if got := allocMessage(t, source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// Each instance of a generic declaration has its own escape analysis: the
// moved parameters and locals are the instance's own objects, and an
// instance whose values do not escape moves nothing.
func TestMixedInstanceHeapMoves(t *testing.T) {
	source := mixedPrefix + "p, q := Box(int16(-2)), Box(uint8(3)); h := Push[int64](nil, 1); _ = Id(int8(1)); ps := Ptrs([]int{1}); s[0] = byte(*p) + *q + byte(h.Val) + byte(*ps[0])\nreturn 0 }\n"
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	moves := map[string]string{}
	for _, fn := range checked.Functions {
		var names []string
		for _, v := range fn.HeapVars {
			names = append(names, v.Name()+":"+v.Type().String())
			if fn.Instance != nil && v.Parent() != nil && !scopeWithin(v.Parent(), checked.Info.Scopes[fn.Decl.Type]) {
				t.Errorf("%s: moved %s is not the instance's own object", fn.Object.Name(), v.Name())
			}
		}
		sort.Strings(names)
		if len(fn.HeapLiterals) > 0 {
			names = append(names, fmt.Sprintf("%d literals", len(fn.HeapLiterals)))
		}
		moves[fn.Object.Name()] = strings.Join(names, ",")
	}
	for name, want := range map[string]string{
		"Box[int16]":  "v:int16",
		"Box[uint8]":  "v:uint8",
		"Push[int64]": "n:program.Node[int64]",
		"Ptrs[int]":   "v:int",
		"Id[int8]":    "",
		"Process":     "",
	} {
		if got, ok := moves[name]; !ok || got != want {
			t.Errorf("%s moves %q (present %v), want %q", name, got, ok, want)
		}
	}
	// Signatures of the moved parameters are the instance's own.
	for _, fn := range checked.Functions {
		if fn.Instance == nil || !strings.HasPrefix(fn.Object.Name(), "Box[") {
			continue
		}
		sig := fn.Object.Type().(*types.Signature)
		if len(fn.HeapVars) != 1 || fn.HeapVars[0] != sig.Params().At(0) {
			t.Errorf("%s: moved %v, parameter %v", fn.Object.Name(), fn.HeapVars, sig.Params().At(0))
		}
	}
}

// The heap runtime is emitted only for instances that allocate: an
// uninstantiated generic function that would allocate adds nothing, and an
// instance over byte slices that only reads them needs no runtime.
func TestMixedHeapRuntimeOnlyForInstances(t *testing.T) {
	plain := "package p\nfunc Process(s, i []byte) uint64 { return uint64(len(s)) }\n"
	unused := "package p\nfunc Mk[T any](n int) []T { return make([]T, n) }\nfunc Sum[T ~[]E, E ~uint8](s T) uint64 { t := uint64(0); for _, v := range s { t = t + uint64(v) }; return t }\nfunc Process(s, i []byte) uint64 { return Sum(s) }\n"
	used := "package p\nfunc Mk[T any](n int) []T { return make([]T, n) }\nfunc Process(s, i []byte) uint64 { return uint64(len(Mk[int8](3))) }\n"
	for name, c := range map[string]struct {
		source string
		heap   bool
	}{"plain": {plain, false}, "unused generic": {unused, false}, "used generic": {used, true}} {
		out, err := Compile("a.go", []byte(c.source))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.Contains(string(out), "gosvm_alloc"); got != c.heap {
			t.Errorf("%s: heap runtime %v, want %v", name, got, c.heap)
		}
	}
}

func nativeMixedCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := mixedvalues.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// mixedVectors covers every mode of examples/mixed-values with random inputs
// and, for the signed make and bounds modes, every sign and boundary class.
func mixedVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	edges := []byte{0, 1, 2, 5, 6, 9, 10, 11, 12, 126, 127, 128, 129, 200, 254, 255}
	for mode := byte(0); mode < 8; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			switch j % 3 {
			case 0:
				for k := 1; k < 8; k++ {
					in[k] = edges[rng.Intn(len(edges))]
				}
			case 1:
				// Small non-negative operands: indexes and bounds in range.
				for k := 1; k < 8; k++ {
					in[k] = byte(rng.Intn(12))
				}
			}
			vectors = append(vectors, in)
		}
	}
	// Every make shape with lengths and capacities around zero and the
	// boundaries: exactly the negative, len > cap and maxAlloc cases panic.
	for shape := byte(0); shape < 6; shape++ {
		for _, l := range []byte{0, 1, 5, 6, 127, 128, 255} {
			for _, c := range []byte{0, 1, 5, 6, 130, 255} {
				in := make([]byte, 128)
				in[0], in[1], in[2], in[3], in[4] = 4, l, c, shape, l%4
				vectors = append(vectors, in)
			}
		}
	}
	return vectors
}

// MixedVectorsForTest returns the vectors the SBF test runs.
func MixedVectorsForTest() [][]byte { return mixedVectors(20261016, 24) }

func mixedSource(t *testing.T) []byte { return mustRead(t, "../../examples/mixed-values/program.go") }

// TestMixedDifferential runs examples/mixed-values natively and as generated
// C on the host (32 KiB heap, as on chain by default) and compares output
// bytes, results and panics for every vector.
func TestMixedDifferential(t *testing.T) {
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: mixedSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	vectors := append(mixedVectors(20261014, 2000), MixedVectorsForTest()...)
	var want []string
	var got strings.Builder
	var batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	panics, runs := map[byte]int{}, map[byte]int{}
	for _, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		result := nativeMixedCall(in)
		want = append(want, result)
		runs[in[0]]++
		if strings.HasPrefix(result, "P:") {
			panics[in[0]]++
			flush()
			got.WriteString(runHost(t, bin, line))
			continue
		}
		batch.WriteString(line)
	}
	flush()
	gotLines := strings.SplitAfter(got.String(), "\n")
	for j := range want {
		if j >= len(gotLines) || gotLines[j] != want[j] {
			g := ""
			if j < len(gotLines) {
				g = gotLines[j]
			}
			t.Fatalf("vector %d (%x):\nC  %q\nGo %q", j, vectors[j], g, want[j])
		}
	}
	// Modes 4-7 (makes, bounds, arith, copies) must exercise both outcomes.
	total := 0
	for mode := byte(4); mode < 8; mode++ {
		if panics[mode] == 0 || panics[mode] > runs[mode]*9/10 {
			t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
		}
		total += panics[mode]
	}
	t.Logf("%d vectors (%d panics: %v) match native Go", len(vectors), total, panics)
}

// mixedSBFSource is examples/mixed-values with a Context entrypoint: account
// 0 is the 1 KiB output and the instruction data is Run's input.
func mixedSBFSource(t *testing.T) []byte {
	source := string(mixedSource(t))
	source = strings.Replace(source, "package mixedvalues\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	entry := "func Process(s, i []byte) uint64 { return Run(s, i) }"
	if !strings.Contains(source, entry) {
		t.Fatal("mixed-values entrypoint changed")
	}
	// Run returns the bytes written; a nonzero result would fail the
	// transaction, so the on-chain entrypoint returns 0.
	return []byte(strings.Replace(source, entry, "func Process(c solana.Context) uint64 { Run(solana.Data(c, 0), solana.Instruction(c)); return 0 }", 1))
}

// TestMixedSBF builds examples/mixed-values for fogo-v0 and solana-v3 with
// the default 32 KiB heap and runs the vectors through the pinned runners:
// completions must leave native Go's output bytes; panics must abort and roll
// back. Skips without the v1.57 backend or a runner (make test).
func TestMixedSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	var cases []allocCase
	aborts := 0
	for j, in := range MixedVectorsForTest() {
		c := allocCase{name: fmt.Sprintf("mixed-%03d-mode%d", j, in[0]), in: in, err: abortError, log: abortLog}
		out := make([]byte, 1024)
		failed := func() (failed bool) {
			defer func() { failed = recover() != nil }()
			mixedvalues.Run(out, in)
			return false
		}()
		if failed {
			aborts++
		} else {
			c.output = out
		}
		cases = append(cases, c)
	}
	source := mixedSBFSource(t)
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "mixed", source, 0, cases, "")
			t.Logf("%s: %d cases (%d aborts with rollback) match native Go; ELF %s", profile, len(cases), aborts, result["elf_sha256"])
		})
	}
}
