package compiler

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	byteloopvalues "gosvm/examples/byte-loop-values"
)

// Hand-written byte loops against native Go (docs/GO_ARRAYS.md#hand-written-byte-loops):
// examples/byte-loop-values runs natively and as host-compiled C with and
// without the word paths; output bytes, results and panics must match for
// every vector.

const byteLoopModes = 10

func byteLoopSource(t *testing.T) []byte {
	return mustRead(t, "../../examples/byte-loop-values/program.go")
}

func nativeByteLoopCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := byteloopvalues.Run(out, append([]byte(nil), in...))
	return fmt.Sprintf("%d:%x\n", code, out)
}

// byteLoopVectors covers every mode with equal operands, one byte flipped at
// each position (bit 0 and bit 7), random operands, every operand length from
// 0 to 32 and nil pointers.
func byteLoopVectors(seed int64) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	base := func(mode byte) []byte {
		in := make([]byte, 72)
		rng.Read(in[8:40])
		copy(in[40:72], in[8:40])
		in[0], in[1], in[2] = mode, 32, 32
		return in
	}
	for mode := byte(0); mode < byteLoopModes; mode++ {
		vectors = append(vectors, base(mode))
		for pos := 0; pos < 32; pos++ {
			for _, bit := range []byte{1, 0x80} {
				in := base(mode)
				in[40+pos] ^= bit
				vectors = append(vectors, in)
			}
		}
		for n := byte(0); n <= 32; n++ {
			in := base(mode)
			in[1] = n
			vectors = append(vectors, in)
			in = base(mode)
			in[2] = n
			vectors = append(vectors, in)
			// Short operand, but an earlier byte differs: the loop returns
			// before reaching the missing byte.
			in = base(mode)
			in[1] = n
			in[40+int(n)/2] ^= 1
			vectors = append(vectors, in)
		}
		for j := 0; j < 40; j++ {
			in := base(mode)
			rng.Read(in[40:72])
			in[1], in[2], in[3] = byte(rng.Intn(33)), byte(rng.Intn(33)), byte(rng.Intn(2))
			vectors = append(vectors, in)
		}
		in := base(mode)
		in[3] = 1
		vectors = append(vectors, in)
	}
	return vectors
}

// ByteLoopVectorsForTest is the vector set the SBF test runs.
func ByteLoopVectorsForTest() [][]byte { return byteLoopVectors(20261019) }

func compileByteLoops(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: byteLoopSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestByteLoopDifferential runs the vectors natively and through host C
// built twice: with the word paths (GOSVM_BYTE_LOOP_WORDS, as on SBF) and
// with only the loops.
func TestByteLoopDifferential(t *testing.T) {
	c := compileByteLoops(t)
	vectors := byteLoopVectors(20261019)
	for _, words := range []bool{true, false} {
		source := c
		if words {
			source = append([]byte("#define GOSVM_BYTE_LOOP_WORDS 1\n"), c...)
		}
		bin := hostCompile(t, source, DefaultHeapBytes, allocDriver, false)
		panics := map[byte]int{}
		var batch strings.Builder
		var want []string
		var got strings.Builder
		flush := func() {
			if batch.Len() > 0 {
				got.WriteString(runHost(t, bin, batch.String()))
				batch.Reset()
			}
		}
		for _, in := range vectors {
			line := fmt.Sprintf("%x\n", in)
			result := nativeByteLoopCall(in)
			want = append(want, result)
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
				t.Fatalf("words=%v vector %d (%x):\nC  %q\nGo %q", words, j, vectors[j], g, want[j])
			}
		}
		// Modes 0, 4 and 6 read fixed arrays and never trap; every other mode
		// must trap on some vectors and complete on others.
		for mode := byte(0); mode < byteLoopModes; mode++ {
			never := mode == 0 || mode == 4 || mode == 6
			if never != (panics[mode] == 0) {
				t.Errorf("mode %d: %d vectors panic", mode, panics[mode])
			}
		}
		t.Logf("words=%v: %d vectors match native Go (%v panics by mode)", words, len(vectors), panics)
	}
}

// byteGuardC matches one emitted word path. withoutByteGuards removes them,
// so the corpus test (check_corpus_test.go) proves they are the only change
// to the emitted C.
var byteGuardC = regexp.MustCompile(`(?s)#if defined\(__bpf__\) \|\| defined\(GOSVM_BYTE_LOOP_WORDS\)\nif \(!\(\{\n.*?\n\}\)\)\n#endif\n`)

func withoutByteGuards(c []byte) []byte { return byteGuardC.ReplaceAll(c, nil) }

// TestByteLoopsC checks which loops get a word path: the five compare
// functions of the example, not its loads (clang already combines them) or
// stores, and not compare loops outside the shape.
func TestByteLoopsC(t *testing.T) {
	if n := len(byteGuardC.FindAll(compileByteLoops(t), -1)); n != 5 {
		t.Fatalf("%d word paths, want 5 (less32, prefix8, equal32, firstDifference, equalSigned)", n)
	}
	for _, src := range []string{
		// The index is not the loop variable alone, the bound is not
		// constant, the variable outlives the loop, the body does more than
		// compare, the operand is not a variable, the elements are wider
		// than a byte, the array is shorter than the bound, or the loop
		// compares something else.
		`func f(a, b []byte) bool { for i := 0; i < 8; i++ { if a[i+1] != b[i] { return false } }; return true }`,
		`func f(a, b []byte) bool { for i := 0; i < len(a); i++ { if a[i] != b[i] { return false } }; return true }`,
		`func f(a, b []byte) bool { i := 0; for i = 0; i < 8; i++ { if a[i] != b[i] { return false } }; return i == 8 }`,
		`func f(a, b []byte) bool { n := 0; for i := 0; i < 8; i++ { n++; if a[i] != b[i] { return false } }; return n == 8 }`,
		`func f(a [][]byte, b []byte) bool { for i := 0; i < 8; i++ { if a[0][i] != b[i] { return false } }; return true }`,
		`func f(a, b []uint16) bool { for i := 0; i < 8; i++ { if a[i] != b[i] { return false } }; return true }`,
		`func f(a [4]byte, b []byte) bool { for i := 0; i < 8; i++ { if a[i] != b[i] { return false } }; return true }`,
		`func f(a, b []byte) bool { for i := 0; i < 8; i++ { if a[i] == b[i] { return false } }; return true }`,
		`func f(a, b []byte) bool { for i := 0; i < 8; i++ { if a[i] != b[i] { return false } else { a[i] = 0 } }; return true }`,
	} {
		program := "package main\nimport \"gosvm/solana\"\n" + src + "\nfunc Process(c solana.Context) uint64 { return 0 }\n"
		out, err := Compile("program.go", []byte(program))
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if byteGuardC.Match(out) {
			t.Errorf("word path for %s", src)
		}
	}
}
