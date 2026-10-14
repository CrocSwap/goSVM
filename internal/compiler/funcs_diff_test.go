package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	funcvalues "gosvm/examples/func-values"
)

// Function values, closures and defer against native Go:
// examples/func-values runs natively and as host-compiled C (32 KiB heap,
// as on chain by default); output bytes, results and panics must match for
// every vector. See docs/GO_FUNCTIONS.md.

func funcSource(t *testing.T) []byte { return mustRead(t, "../../examples/func-values/program.go") }

func nativeFuncCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := funcvalues.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// funcModes is the number of modes Run dispatches on; mode 7 (envs) counts
// closure environments and is limited to counts that fit the heap.
const funcModes = 9

// funcVectors covers every mode with random inputs, edge bytes and every
// selector of the panic paths.
func funcVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	edges := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 12, 100, 127, 128, 200, 254, 255}
	for mode := byte(0); mode < funcModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			if j%2 == 0 {
				for k := 1; k < 32; k++ {
					in[k] = edges[rng.Intn(len(edges))]
				}
			}
			if mode == 7 {
				in[2] = 0 // at most 255 values: they fit the heap
				in[3] = in[3] % 7
			}
			vectors = append(vectors, in)
		}
	}
	// Every panic selector of every mode.
	for mode := byte(0); mode < funcModes; mode++ {
		for sel := byte(0); sel < 12; sel++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			for _, k := range []int{3, 4, 5, 6, 12} {
				in[k] = sel
			}
			if mode == 7 {
				in[1], in[2], in[3] = sel*20, 0, sel%7
			}
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// FuncVectorsForTest returns the vectors the SBF test runs.
func FuncVectorsForTest() [][]byte { return funcVectors(20261018, 24) }

func compileFuncValues(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: funcSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFuncValuesDifferential(t *testing.T) {
	bin := hostCompile(t, compileFuncValues(t), DefaultHeapBytes, allocDriver, false)
	vectors := append(funcVectors(20261014, 1500), FuncVectorsForTest()...)
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
		result := nativeFuncCall(in)
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
	// Modes with panic paths must exercise both outcomes; the others must
	// complete.
	total := 0
	for mode := byte(0); mode < funcModes; mode++ {
		switch mode {
		case 0, 2, 3, 4, 5:
			if panics[mode] == 0 || panics[mode] == runs[mode] {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
		case 8:
			// in[6]%8 == 0 divides by zero.
			if panics[mode] == 0 || panics[mode] == runs[mode] {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
		default:
			if panics[mode] != 0 {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
		}
		total += panics[mode]
	}
	t.Logf("%d vectors (%d panics: %v) match native Go", len(vectors), total, panics)
}

// envInput is a mode-7 input: n function values of kind.
func envInput(kind byte, n int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = 7, byte(n), byte(n>>8), kind
	in[4], in[5], in[6] = 1, 2, 3
	return in
}

// envUpfront is what envs allocates before its loop: the captured a, b and
// c (8 bytes each) and the &Big{} literal a method value binds (24 bytes).
const envUpfront = 3*8 + 24

// envBytes is the heap each mode-7 kind allocates per value: a literal
// capturing one variable (8 + 8), three (8 + 3*8), a declared function and
// a literal without captures (nothing), a value-receiver method value of a
// 24-byte struct (8 + 24), a pointer-receiver method value (8 + 8), and a
// deferred call with one uint64 argument in a loop (16 + 8).
var envBytes = map[byte]int{0: 16, 1: 32, 2: 0, 3: 0, 4: 32, 5: 16, 6: 24}

// envLimit is the number of values of kind that fit the default heap
// (32,760 usable bytes); -1 when they allocate nothing.
func envLimit(kind byte) int {
	if envBytes[kind] == 0 {
		return -1
	}
	return (32760 - envUpfront) / envBytes[kind]
}

// TestClosureEnvironmentHeap checks the heap cost of function values
// exactly: the value that does not fit aborts, after exactly the predicted
// number; native Go never runs out, so the expectation is computed.
func TestClosureEnvironmentHeap(t *testing.T) {
	bin := hostCompile(t, compileFuncValues(t), DefaultHeapBytes, allocDriver, false)
	counted := func(n int, done bool, kind byte) string {
		out := make([]byte, 1024)
		out[0], out[1] = byte(n), byte(n>>8)
		if done {
			out[2] = 1
			// keep's result; kind 6 keeps no value.
			out[3] = [7]byte{1, 6, 1, 3, 1, 2, 0}[kind]
			return fmt.Sprintf("3:%x\n", out)
		}
		return fmt.Sprintf("P:%x\n", out)
	}
	for kind := byte(0); kind < 7; kind++ {
		limit := envLimit(kind)
		if limit < 0 {
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", envInput(kind, 65535))), counted(65535, true, kind); got != want {
				t.Errorf("kind %d: 65535 values: %.16s, want %.16s", kind, got, want)
			}
			continue
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", envInput(kind, limit))), counted(limit, true, kind); got != want {
			t.Errorf("kind %d: %d values: %.16s, want %.16s", kind, limit, got, want)
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", envInput(kind, limit+1))), counted(limit, false, kind); got != want {
			t.Errorf("kind %d: %d values: %.16s, want %.16s", kind, limit+1, got, want)
		}
		if got, want := nativeFuncCall(envInput(kind, limit+1)), counted(limit+1, true, kind); got != want {
			t.Errorf("kind %d: native Go: %.16s, want %.16s", kind, got, want)
		}
	}
}
