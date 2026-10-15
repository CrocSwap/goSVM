package compiler

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	mapvalues "gosvm/examples/map-values"
)

// Maps against native Go: examples/map-values runs natively and as
// host-compiled C (32 KiB heap, as on chain by default, and 256 KiB for large
// maps); output bytes, results and panics must match for every vector. Every
// observation is independent of iteration order. See docs/GO_MAPS.md.

func mapSource(t *testing.T) []byte { return mustRead(t, "../../examples/map-values/program.go") }

func nativeMapCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := mapvalues.Run(out, append([]byte(nil), in...))
	return fmt.Sprintf("%d:%x\n", code, out)
}

// mapModes is the number of modes Run dispatches on.
const mapModes = 9

// mapVectors covers every mode. Large maps (mode 5) stay below limit keys and
// heap-use vectors (mode 6) below 300 insertions, so each fits the heap it
// runs with (native Go never runs out of memory).
func mapVectors(seed int64, perMode int, limit int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	for mode := byte(0); mode < mapModes; mode++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			switch mode {
			case 0:
				in[1] = byte(j % 20)
			case 1:
				in[1] = byte(j % 12)
			case 2, 3:
				in[1] = byte(j % 12)
			case 5:
				n := rng.Intn(limit)
				in[1], in[2] = byte(n), byte(n>>8)
			case 6:
				n := rng.Intn(300)
				in[1], in[2], in[3] = byte(n), byte(n>>8), 0
				in[4] = byte(rng.Intn(5))
				in[5], in[6] = byte(rng.Intn(64)), 0
			case 8:
				in[1] = byte(rng.Intn(6))
				size := 1 + rng.Intn(56)
				n := rng.Intn(200)
				in[2], in[3], in[4], in[5] = byte(size), 0, byte(n), byte(n>>8)
			}
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// MapVectorsForTest returns the vectors the SBF test runs.
func MapVectorsForTest() [][]byte { return mapVectors(20261019, 30, 380) }

func compileMaps(t *testing.T, heap int) []byte {
	t.Helper()
	c, err := CompileProgram(&Program{Entry: "program", HeapBytes: heap, Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: mapSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// runMapVectors compares vectors natively and on the host binary and returns
// the panics per mode.
func runMapVectors(t *testing.T, bin string, vectors [][]byte) (map[byte]int, map[byte]int) {
	t.Helper()
	var want []string
	var got, batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	panics, runs := map[byte]int{}, map[byte]int{}
	for _, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		result := nativeMapCall(in)
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
	return panics, runs
}

func TestMapDifferential(t *testing.T) {
	bin := hostCompile(t, compileMaps(t, 0), DefaultHeapBytes, allocDriver, false)
	vectors := append(mapVectors(20261014, 600, 380), MapVectorsForTest()...)
	panics, runs := runMapVectors(t, bin, vectors)
	// Modes 2 and 3 select panicking and completing scenarios; the others
	// must complete.
	total := 0
	for mode := byte(0); mode < mapModes; mode++ {
		switch mode {
		case 2, 3:
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
	// Large maps on a 256 KiB heap: thousands of keys and many growths.
	big := hostCompile(t, compileMaps(t, MaxHeapBytes), MaxHeapBytes, allocDriver, false)
	var large [][]byte
	for _, n := range []int{0, 1, 6, 7, 12, 13, 384, 385, 1000, 1536, 1537, 3000, 3072} {
		in := make([]byte, 128)
		in[0], in[1], in[2] = 5, byte(n), byte(n>>8)
		large = append(large, in)
	}
	// Long random scripts over large key spaces (mode 9).
	rng := rand.New(rand.NewSource(20261020))
	for j := 0; j < 24; j++ {
		in := make([]byte, 128)
		rng.Read(in)
		space, ops := 1+rng.Intn(2500), 1+rng.Intn(1200)
		in[0], in[1], in[2], in[3], in[4] = 9, byte(space), byte(space>>8), byte(ops), byte(ops>>8)
		large = append(large, in)
	}
	runMapVectors(t, big, large)
	t.Logf("%d large-map vectors match native Go with a 256 KiB heap", len(large))
	// The 3,073rd key needs 8,192 slots: the chain 8, 16, ..., 8192 of
	// 24-byte slots (393,024 bytes) exceeds 256 KiB, so it aborts.
	in := make([]byte, 128)
	in[0], in[1], in[2] = 5, 0x01, 0x0c
	if got := runHost(t, big, fmt.Sprintf("%x\n", in)); !strings.HasPrefix(got, "P:") {
		t.Fatalf("3,073 keys fit a 256 KiB heap: %.20q", got)
	}
}
