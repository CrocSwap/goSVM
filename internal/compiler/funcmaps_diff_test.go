package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	funcmaps "gosvm/examples/func-maps"
)

// Maps combined with function values, closures, defer and named results
// against native Go: examples/func-maps runs natively and as host-compiled C
// (32 KiB heap, as on chain by default); output bytes, results and panics
// must match for every vector. See docs/GO_FUNCTIONS.md and docs/GO_MAPS.md.

func funcMapSource(t *testing.T) []byte {
	return mustRead(t, "../../examples/func-maps/program.go")
}

func nativeFuncMapCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := funcmaps.Run(out, append([]byte(nil), in...))
	return fmt.Sprintf("%d:%x\n", code, out)
}

// funcMapModes is the number of modes Run dispatches on; mode 5 (envs)
// counts closure environments and loop defers and is limited to counts that
// fit the heap.
const funcMapModes = 6

// funcMapVectors covers every mode with random inputs, inputs over a small
// alphabet (so string keys repeat) and every selector of the panic paths.
func funcMapVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	alphabet := []byte{'a', 'b', 'z', 'A', 0xc3, 0xa9, 0, 1, 2}
	for mode := byte(0); mode < funcMapModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			if j%2 == 0 {
				for k := 1; k < 32; k++ {
					in[k] = alphabet[rng.Intn(len(alphabet))]
				}
			}
			if j%3 == 0 {
				for k := 1; k < 30; k++ {
					in[k] = byte(rng.Intn(8))
				}
			}
			if mode == 5 {
				in[1], in[2], in[3] = byte(rng.Intn(256)), 0, byte(rng.Intn(5))
			}
			vectors = append(vectors, in)
		}
	}
	for mode := byte(0); mode < funcMapModes-1; mode++ {
		for sel := byte(0); sel < 12; sel++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			for k := 1; k < 30; k++ {
				in[k] = sel + byte(k%2)
			}
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// FuncMapVectorsForTest is the vector set the SBF test runs.
func FuncMapVectorsForTest() [][]byte { return funcMapVectors(20261015, 50) }

func compileFuncMaps(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: funcMapSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFuncMapsDifferential(t *testing.T) {
	t.Parallel()
	bin := hostCompile(t, compileFuncMaps(t), DefaultHeapBytes, allocDriver, false)
	vectors := funcMapVectors(20261014, 1500)
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
		result := nativeFuncMapCall(in)
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
	// Modes 0-4 have panic paths (nil-map writes, unhashable keys, nil
	// function calls, nil pointers, bounds) and must exercise both
	// outcomes; mode 5 completes.
	total := 0
	for mode := byte(0); mode < funcMapModes; mode++ {
		if mode == 5 {
			if panics[mode] != 0 {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
		} else if panics[mode] == 0 || panics[mode] == runs[mode] {
			t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
		}
		total += panics[mode]
	}
	t.Logf("%d vectors (%d panics: %v) match native Go", len(vectors), total, panics)
}

// funcMapUpfront is what is allocated before envs's loop: Run's 32-byte W
// (its address reaches parameters that closures capture in other modes, so
// it moves to the heap at Run's entry); shared, captured by kind 1, moves
// to the heap (8) and its one-entry literal allocates a 24-byte header and
// 8 slots of 24 bytes (216); and c's empty literal allocates a 24-byte
// header.
const funcMapUpfront = 32 + 8 + 216 + 24

// funcMapBytes is the heap each mode-5 kind allocates per value: a
// per-iteration map variable captured by a literal (8 moved + 24 header +
// 16 environment); a literal capturing the outer map (8 + 8); a deferred
// call saving a map and a uint64 in a loop (16-byte header + 8 + 8; the
// deferred calls write an existing key at return, so they never grow the
// table); a value-receiver method value of a map type (8 + 8); a literal
// without captures (nothing).
var funcMapBytes = map[byte]int{0: 48, 1: 16, 2: 32, 3: 16, 4: 0}

// funcMapResult is out[3] at the end of envs: keep's result (len(m) = 0,
// len(shared) = 1, no keep, c.Len() = 0, 0).
var funcMapResult = map[byte]byte{0: 0, 1: 1, 2: 0, 3: 0, 4: 0}

func funcMapInput(kind byte, n int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = 5, byte(n), byte(n>>8), kind
	return in
}

// funcMapLimit is the number of kind values that fit the default heap, or
// -1 when the kind allocates nothing.
func funcMapLimit(kind byte) int {
	if funcMapBytes[kind] == 0 {
		return -1
	}
	return (DefaultHeapBytes - 8 - funcMapUpfront) / funcMapBytes[kind]
}

func funcMapCounted(n int, done bool, kind byte) string {
	out := make([]byte, 1024)
	out[0], out[1] = byte(n), byte(n>>8)
	if done {
		out[2], out[3] = 1, funcMapResult[kind]
		return fmt.Sprintf("3:%x\n", out)
	}
	return fmt.Sprintf("P:%x\n", out)
}

// TestClosureMapHeap checks the out-of-memory point of closures capturing
// maps, loop defers saving maps and method values of map types exactly: the
// value that does not fit aborts after exactly the predicted number; native
// Go never runs out, so the expectation is computed.
func TestClosureMapHeap(t *testing.T) {
	t.Parallel()
	if DefaultHeapBytes-8 != 32760 {
		t.Fatalf("default heap %d", DefaultHeapBytes)
	}
	bin := hostCompile(t, compileFuncMaps(t), DefaultHeapBytes, allocDriver, false)
	boundaries := 0
	for kind := byte(0); kind < 5; kind++ {
		limit := funcMapLimit(kind)
		if limit < 0 {
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", funcMapInput(kind, 65535))), funcMapCounted(65535, true, kind); got != want {
				t.Errorf("kind %d: 65535 values: %.16s, want %.16s", kind, got, want)
			}
			continue
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", funcMapInput(kind, limit))), funcMapCounted(limit, true, kind); got != want {
			t.Errorf("kind %d: %d values: %.16s, want %.16s", kind, limit, got, want)
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", funcMapInput(kind, limit+1))), funcMapCounted(limit, false, kind); got != want {
			t.Errorf("kind %d: %d values: %.16s, want %.16s", kind, limit+1, got, want)
		}
		if got, want := nativeFuncMapCall(funcMapInput(kind, limit+1)), funcMapCounted(limit+1, true, kind); got != want {
			t.Errorf("kind %d: native Go: %.16s, want %.16s", kind, got, want)
		}
		boundaries++
	}
	t.Logf("%d exact out-of-memory boundaries", boundaries)
}
