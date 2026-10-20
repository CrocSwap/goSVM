package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	funcstrings "gosvm/examples/func-strings"
)

// Function values combined with strings and error values against native Go:
// examples/func-strings runs natively and as host-compiled C (32 KiB heap,
// as on chain by default); output bytes, results and panics must match for
// every vector. See docs/GO_FUNCTIONS.md and docs/GO_STRINGS.md.

func funcStringSource(t *testing.T) []byte {
	return mustRead(t, "../../examples/func-strings/program.go")
}

func nativeFuncStringCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := funcstrings.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// funcStringModes is the number of modes Run dispatches on; mode 5 (envs)
// counts closure environments and is limited to counts that fit the heap.
const funcStringModes = 6

// funcStringVectors covers every mode with random inputs, inputs over a
// small alphabet (ASCII, non-ASCII and empty strings) and every selector of
// the panic paths.
func funcStringVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	alphabet := []byte{'a', 'b', 'z', 'A', 0xc3, 0xa9, 0x80, 0xff, 0}
	for mode := byte(0); mode < funcStringModes; mode++ {
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
				for k := 1; k < 17; k++ {
					in[k] = byte(rng.Intn(6))
				}
			}
			if mode == 5 {
				in[1], in[2], in[3] = byte(rng.Intn(256)), 0, byte(rng.Intn(6))
			}
			vectors = append(vectors, in)
		}
	}
	for mode := byte(0); mode < funcStringModes-1; mode++ {
		for sel := byte(0); sel < 12; sel++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			for k := 1; k < 17; k++ {
				in[k] = sel + byte(k%2)
			}
			vectors = append(vectors, in)
		}
	}
	return vectors
}

func compileFuncStrings(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: funcStringSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFuncStringsDifferential(t *testing.T) {
	t.Parallel()
	bin := hostCompile(t, compileFuncStrings(t), DefaultHeapBytes, allocDriver, false)
	vectors := funcStringVectors(20261014, 1500)
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
		result := nativeFuncStringCall(in)
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
	// Modes 0-4 have panic paths (bounds, nil function calls, nil
	// pointers, uncomparable errors, failed assertions) and must exercise
	// both outcomes; mode 5 completes.
	total := 0
	for mode := byte(0); mode < funcStringModes; mode++ {
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

// funcStringUpfront is what is allocated before envs's loop: Run's 32-byte
// W (its address reaches parameters that closures capture in other modes,
// so it escapes and moves to the heap at Run's entry) and the captured
// string a (a 16-byte header; its bytes stay in read-only data).
const funcStringUpfront = 32 + 16

// funcStringBytes is the heap each mode-5 kind allocates per value: a
// per-iteration string (16) captured by a literal (8 + 8); a literal
// capturing the outer string (8 + 8); a per-iteration string (16) and a
// literal capturing it and the outer string (8 + 2*8); a deferred call with
// one string argument in a loop (16-byte header + 16); a value-receiver
// method value of a named string (8 + 16); a literal without captures
// (nothing).
var funcStringBytes = map[byte]int{0: 32, 1: 16, 2: 40, 3: 32, 4: 24, 5: 0}

// funcStringResult is len(keep()) at the end of envs (kind 3 keeps nothing).
var funcStringResult = map[byte]byte{0: 5, 1: 5, 2: 5, 3: 0, 4: 5, 5: 3}

func funcStringInput(kind byte, n int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = 5, byte(n), byte(n>>8), kind
	return in
}

// TestClosureStringHeap checks the out-of-memory point of closures,
// method values and loop defers over strings exactly: the value that does
// not fit aborts after exactly the predicted number; native Go never runs
// out, so the expectation is computed.
func TestClosureStringHeap(t *testing.T) {
	t.Parallel()
	bin := hostCompile(t, compileFuncStrings(t), DefaultHeapBytes, allocDriver, false)
	counted := func(n int, done bool, kind byte) string {
		out := make([]byte, 1024)
		out[0], out[1] = byte(n), byte(n>>8)
		if done {
			out[2], out[3] = 1, funcStringResult[kind]
			return fmt.Sprintf("3:%x\n", out)
		}
		return fmt.Sprintf("P:%x\n", out)
	}
	boundaries := 0
	for kind := byte(0); kind < 6; kind++ {
		per := funcStringBytes[kind]
		if per == 0 {
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", funcStringInput(kind, 65535))), counted(65535, true, kind); got != want {
				t.Errorf("kind %d: 65535 values: %.16s, want %.16s", kind, got, want)
			}
			continue
		}
		limit := (32760 - funcStringUpfront) / per
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", funcStringInput(kind, limit))), counted(limit, true, kind); got != want {
			t.Errorf("kind %d: %d values: %.16s, want %.16s", kind, limit, got, want)
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", funcStringInput(kind, limit+1))), counted(limit, false, kind); got != want {
			t.Errorf("kind %d: %d values: %.16s, want %.16s", kind, limit+1, got, want)
		}
		if got, want := nativeFuncStringCall(funcStringInput(kind, limit+1)), counted(limit+1, true, kind); got != want {
			t.Errorf("kind %d: native Go: %.16s, want %.16s", kind, got, want)
		}
		boundaries++
	}
	t.Logf("%d exact out-of-memory boundaries", boundaries)
}
