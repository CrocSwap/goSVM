package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	ifacevalues "gosvm/examples/iface-values"
)

// Interface values against native Go: examples/iface-values runs natively
// and as host-compiled C (32 KiB heap, as on chain by default); output
// bytes, results and panics must match for every vector. See
// docs/GO_INTERFACES.md.

func ifaceSource(t *testing.T) []byte { return mustRead(t, "../../examples/iface-values/program.go") }

func nativeIfaceCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := ifacevalues.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// ifaceModes is the number of modes Run dispatches on; mode 8 (boxes)
// counts conversions and is limited to counts that fit the heap.
const ifaceModes = 9

// ifaceVectors covers every mode with random inputs, inputs whose pick
// selectors cover every dynamic type pair, and every nil-call combination.
func ifaceVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	edges := []byte{0, 1, 2, 3, 4, 5, 7, 8, 11, 12, 100, 127, 128, 200, 254, 255}
	for mode := byte(0); mode < ifaceModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			if j%2 == 0 {
				for k := 1; k < 32; k++ {
					in[k] = edges[rng.Intn(len(edges))]
				}
			}
			if mode == 8 {
				in[2] = in[2] % 4 // at most 1,023 conversions: 24,552 bytes
			}
			vectors = append(vectors, in)
		}
	}
	// Assertions, switches and comparisons for every pair of dynamic types.
	for _, mode := range []byte{2, 4} {
		for a := byte(0); a < 12; a++ {
			for b := byte(0); b < 12; b++ {
				in := make([]byte, 128)
				rng.Read(in)
				in[0], in[1], in[3] = mode, a, b
				in[2], in[4] = byte(rng.Intn(3)), byte(rng.Intn(3))
				if mode == 2 {
					in[3] = b % 6
				}
				vectors = append(vectors, in)
			}
		}
	}
	for sel := byte(0); sel < 16; sel++ {
		in := make([]byte, 128)
		rng.Read(in)
		in[0], in[1] = 5, sel
		vectors = append(vectors, in)
	}
	return vectors
}

// IfaceVectorsForTest returns the vectors the SBF test runs.
func IfaceVectorsForTest() [][]byte { return ifaceVectors(20261017, 24) }

func TestInterfaceDifferential(t *testing.T) {
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: ifaceSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	vectors := append(ifaceVectors(20261014, 1500), IfaceVectorsForTest()...)
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
		result := nativeIfaceCall(in)
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
	// The assertion, comparison and nil-call modes must exercise both
	// outcomes; the others must complete.
	total := 0
	for mode := byte(0); mode < ifaceModes; mode++ {
		switch mode {
		case 2, 4, 5:
			if panics[mode] == 0 || panics[mode] == runs[mode] {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
		case 6:
		default:
			if panics[mode] != 0 {
				t.Errorf("mode %d: %d of %d vectors panic", mode, panics[mode], runs[mode])
			}
		}
		total += panics[mode]
	}
	t.Logf("%d vectors (%d panics: %v) match native Go", len(vectors), total, panics)
}

// boxInput is a mode-8 input: n conversions of kind.
func boxInput(kind byte, n int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3] = 8, byte(n), byte(n>>8), kind
	return in
}

// boxLimits is the number of conversions of each mode-8 kind that fit the
// default 32 KiB heap (32,760 usable bytes): 8 bytes for uint64, int8 and
// Sq (uint32), 24 for [3]uint64; pointers and zero-size values allocate
// nothing (-1: no limit).
var boxLimits = map[byte]int{0: 4095, 1: 1365, 2: -1, 3: -1, 4: 4095, 5: 4095}

// TestInterfaceBoxingHeap checks the heap cost of boxing exactly: the
// conversion that does not fit aborts, after exactly the predicted number of
// conversions; native Go never runs out, so the expectation is computed.
func TestInterfaceBoxingHeap(t *testing.T) {
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: ifaceSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	counted := func(n int, done bool) string {
		out := make([]byte, 1024)
		out[0], out[1] = byte(n), byte(n>>8)
		if done {
			out[2] = 1
			return fmt.Sprintf("3:%x\n", out)
		}
		return fmt.Sprintf("P:%x\n", out)
	}
	for kind := byte(0); kind < 6; kind++ {
		limit := boxLimits[kind]
		if limit < 0 {
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", boxInput(kind, 65535))), counted(65535, true); got != want {
				t.Errorf("kind %d: 65535 conversions: %.12s, want %.12s", kind, got, want)
			}
			continue
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", boxInput(kind, limit))), counted(limit, true); got != want {
			t.Errorf("kind %d: %d conversions: %.12s, want %.12s", kind, limit, got, want)
		}
		if got, want := runHost(t, bin, fmt.Sprintf("%x\n", boxInput(kind, limit+1))), counted(limit, false); got != want {
			t.Errorf("kind %d: %d conversions: %.12s, want %.12s", kind, limit+1, got, want)
		}
		if got, want := nativeIfaceCall(boxInput(kind, limit+1)), counted(limit+1, true); got != want {
			t.Errorf("kind %d: native Go: %.12s", kind, got)
		}
	}
}
