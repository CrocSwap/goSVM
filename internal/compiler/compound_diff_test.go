package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	compoundvalues "gosvm/examples/compound-values"
)

// Compound assignment against native Go: examples/compound-values runs
// natively and as host-compiled C; output bytes, results and panics must
// match for every vector. See docs/GO_INTEGERS.md#compound-assignment.

func compoundSource(t *testing.T) []byte {
	return mustRead(t, "../../examples/compound-values/program.go")
}

func nativeCompoundCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := compoundvalues.Run(out, append([]byte(nil), in...))
	return fmt.Sprintf("%d:%x\n", code, out)
}

// compoundModes is the number of modes Run dispatches on.
const compoundModes = 5

// compoundVectors covers every mode and, for modes 0 and 1, every type with
// random operands, operands built from boundary bytes (0, 1, 0x7f, 0x80,
// 0xff: zero, one, -1, MinInt and MaxInt at each width) and small shift
// counts, and every selector of the panic paths.
func compoundVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	edges := []byte{0, 1, 0x7f, 0x80, 0xff}
	var vectors [][]byte
	for mode := byte(0); mode < compoundModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			if mode < 2 {
				in[1] = byte(j % 6)
			}
			switch j % 4 {
			case 1:
				for k := 4; k < 30; k++ {
					in[k] = edges[rng.Intn(len(edges))]
				}
			case 2:
				for k := 4; k < 30; k++ {
					in[k] = byte(rng.Intn(4))
				}
				in[20] = byte(rng.Intn(70))
			case 3:
				in[20] = byte(rng.Intn(70))
			}
			vectors = append(vectors, in)
		}
	}
	for mode := byte(0); mode < compoundModes; mode++ {
		for sel := byte(0); sel < 16; sel++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			in[1], in[2], in[3], in[8] = sel%6, sel, sel, sel
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// CompoundVectorsForTest is the vector set the SBF test runs.
func CompoundVectorsForTest() [][]byte { return compoundVectors(20261017, 60) }

func compileCompound(t *testing.T) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: compoundSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCompoundDifferential(t *testing.T) {
	t.Parallel()
	bin := hostCompile(t, compileCompound(t), DefaultHeapBytes, allocDriver, false)
	vectors := compoundVectors(20261014, 2400)
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
		result := nativeCompoundCall(in)
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
	// Modes 0-3 have panic paths (division and modulo by zero, negative
	// shift counts, bounds, nil pointers and nil maps after the right
	// operand) and must exercise both outcomes; mode 4 (loops) completes.
	total := 0
	for mode := byte(0); mode < compoundModes; mode++ {
		if mode == 4 {
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
