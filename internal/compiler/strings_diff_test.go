package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	stringvalues "gosvm/examples/string-values"
)

// Strings against native Go: examples/string-values runs natively and as
// host-compiled C (32 KiB heap, as on chain by default); output bytes,
// results and panics must match for every vector. See docs/GO_STRINGS.md.

func stringSource(t *testing.T) []byte { return mustRead(t, "../../examples/string-values/program.go") }

func nativeStringCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := stringvalues.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// stringModes is the number of modes Run dispatches on; mode 8 (heap use)
// is limited to counts that fit the heap.
const stringModes = 9

// utf8Edges are byte sequences at the edges of UTF-8 decoding: ASCII,
// boundaries of each length, overlong forms, surrogates, the maximum rune
// and beyond, truncated sequences, stray continuation bytes and bytes that
// never start a sequence.
var utf8Edges = [][]byte{
	{0x00}, {0x7f}, {0x80}, {0xbf}, {0xc0, 0x80}, {0xc1, 0xbf}, {0xc2, 0x80}, {0xdf, 0xbf},
	{0xe0, 0x80, 0x80}, {0xe0, 0x9f, 0xbf}, {0xe0, 0xa0, 0x80}, {0xed, 0x9f, 0xbf}, {0xed, 0xa0, 0x80},
	{0xed, 0xbf, 0xbf}, {0xee, 0x80, 0x80}, {0xef, 0xbf, 0xbf}, {0xef, 0xbf, 0xbd},
	{0xf0, 0x80, 0x80, 0x80}, {0xf0, 0x8f, 0xbf, 0xbf}, {0xf0, 0x90, 0x80, 0x80}, {0xf4, 0x8f, 0xbf, 0xbf},
	{0xf4, 0x90, 0x80, 0x80}, {0xf5, 0x80, 0x80, 0x80}, {0xf8, 0x88, 0x80, 0x80, 0x80}, {0xff},
	{0xe2, 0x82}, {0xf0, 0x9f, 0x98}, {0xc3}, {0xe2, 0x28, 0xa1}, {0xc3, 0x28}, {'a'}, {0xc3, 0xa9},
}

// stringVectors covers every mode with random inputs, inputs over a small
// alphabet (shared prefixes for comparisons and switches) and inputs built
// from the UTF-8 edge sequences.
func stringVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	alphabet := []byte{'a', 'b', 'c', 0x80, 0xff, 0}
	for mode := byte(0); mode < stringModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			switch j % 3 {
			case 0:
				for k := 1; k < 128; k++ {
					in[k] = alphabet[rng.Intn(len(alphabet))]
				}
			case 1:
				o := 2
				for o < 128 {
					o += copy(in[o:], utf8Edges[rng.Intn(len(utf8Edges))])
				}
			}
			if mode == 8 {
				in[1], in[2], in[3] = byte(rng.Intn(64)), 0, byte(rng.Intn(9))
			}
			if j%5 == 0 {
				for k := 1; k < 10; k++ {
					in[k] = byte(rng.Intn(4))
				}
			}
			vectors = append(vectors, in)
		}
	}
	// Every single edge sequence, alone and twice, through range and
	// []rune conversions.
	for _, edge := range utf8Edges {
		for reps := 1; reps <= 2; reps++ {
			for _, mode := range []byte{3, 4} {
				in := make([]byte, 128)
				in[0] = mode
				o := 2
				if mode == 3 {
					o = 26
				}
				var s []byte
				for r := 0; r < reps; r++ {
					s = append(s, edge...)
				}
				copy(in[o:], s)
				if mode == 3 {
					in[25] = byte(len(s))
				} else {
					in[1] = byte(len(s))
				}
				vectors = append(vectors, in)
			}
		}
	}
	return vectors
}

// StringVectorsForTest returns the vectors the SBF test runs.
func StringVectorsForTest() [][]byte { return stringVectors(20261018, 40) }

func compileStrings(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: stringSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStringDifferential(t *testing.T) {
	bin := hostCompile(t, compileStrings(t), DefaultHeapBytes, allocDriver, false)
	vectors := append(stringVectors(20261014, 1500), StringVectorsForTest()...)
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
		result := nativeStringCall(in)
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
	// Modes with bounds or assertion panics must exercise both outcomes; the
	// others must complete.
	total := 0
	for mode := byte(0); mode < stringModes; mode++ {
		switch mode {
		case 0, 1, 5, 6, 7:
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
