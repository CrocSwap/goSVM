package compiler

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Package-level variables against native Go: examples/global-values (an
// entry package of two files and four imported packages) runs natively in a
// fresh process per vector, as every SBF invocation starts from freshly
// initialized variables, and as host-compiled C with the default 32 KiB
// heap; output bytes, results and panics must match. See docs/GO_GLOBALS.md.

const globalEntry = "gosvm/examples/global-values"

// globalPackages are the example's on-chain packages, by import path.
var globalPackages = []string{globalEntry, globalEntry + "/alpha", globalEntry + "/beta", globalEntry + "/log", globalEntry + "/zeta"}

// globalProgram reads the example's packages from disk; replace, when set,
// rewrites the entry package's program.go.
func globalProgramSources(t *testing.T, replace func(string) string) *Program {
	t.Helper()
	p := &Program{Entry: globalEntry}
	for _, path := range globalPackages {
		dir := filepath.Join("../../examples/global-values", strings.TrimPrefix(strings.TrimPrefix(path, globalEntry), "/"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		unit := PackageSources{Path: path}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			data := mustRead(t, filepath.Join(dir, e.Name()))
			if replace != nil && path == globalEntry && e.Name() == "program.go" {
				data = []byte(replace(string(data)))
			}
			unit.Sources = append(unit.Sources, Source{Name: filepath.Join(dir, e.Name()), Data: data})
		}
		p.Packages = append(p.Packages, unit)
	}
	return p
}

// globalOracle builds the native oracle command (one process per vector).
func globalOracle(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "oracle")
	args := []string{"build", "-o", bin}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	cmd := exec.Command("go", append(args, "./examples/global-values/oracle")...)
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build oracle: %v\n%s", err, out)
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("codesign", "--force", "--sign", "-", bin).CombinedOutput(); err != nil {
			t.Fatalf("codesign oracle: %v\n%s", err, out)
		}
	}
	return bin
}

// nativeGlobals runs each vector in its own oracle process, in parallel.
func nativeGlobals(t *testing.T, oracle string, vectors [][]byte) []string {
	t.Helper()
	results := make([]string, len(vectors))
	var wg sync.WaitGroup
	jobs := make(chan int)
	var failed sync.Once
	var failure error
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				out, err := exec.Command(oracle, fmt.Sprintf("%x", vectors[j])).Output()
				if err != nil {
					failed.Do(func() { failure = fmt.Errorf("vector %d: %v", j, err) })
				}
				results[j] = string(out)
			}
		}()
	}
	for j := range vectors {
		jobs <- j
	}
	close(jobs)
	wg.Wait()
	if failure != nil {
		t.Fatal(failure)
	}
	return results
}

// globalModes is the number of modes Run dispatches on.
const globalModes = 7

// globalVectors covers every mode with random inputs; some select Go panics
// (out-of-range table indexes, nil function values and maps, failed
// assertions).
func globalVectors(seed int64, perMode int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	for mode := byte(0); mode < globalModes; mode++ {
		n := perMode
		if mode == 0 {
			n = 1 // the initial state does not depend on the input
		}
		for j := 0; j < n; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			switch mode {
			case 3:
				in[10] = 0
				if rng.Intn(8) == 0 {
					in[10] = 255 // calls the nil function value
				}
			case 4:
				if rng.Intn(6) != 0 {
					in[4] = 0
				} else {
					in[4] = 255
				}
				if rng.Intn(6) != 0 {
					in[5] = 0
				} else {
					in[5] = 255
				}
			case 6:
				in[2] = byte(rng.Intn(3)) // up to 768 appends
				in[3] = byte(rng.Intn(200))
			}
			vectors = append(vectors, in)
		}
	}
	return vectors
}

// GlobalVectorsForTest returns the vectors the SBF test runs.
func GlobalVectorsForTest() [][]byte { return globalVectors(20261021, 40) }

func compileGlobals(t *testing.T, replace func(string) string) []byte {
	t.Helper()
	c, err := CompileProgram(globalProgramSources(t, replace))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// compareGlobalVectors runs the vectors on the host binary and compares them
// with native results; it returns the panics per mode.
func compareGlobalVectors(t *testing.T, bin string, vectors [][]byte, want []string) map[byte]int {
	t.Helper()
	var got, batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	panics := map[byte]int{}
	for j, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		if strings.HasPrefix(want[j], "P:") {
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
			t.Fatalf("vector %d (%x):\nC  %.300q\nGo %.300q", j, vectors[j], g, want[j])
		}
	}
	return panics
}

func TestGlobalsDifferential(t *testing.T) {
	t.Parallel()
	oracle := globalOracle(t)
	bin := hostCompile(t, compileGlobals(t, nil), DefaultHeapBytes, allocDriver, false)
	vectors := append(globalVectors(20261014, 300), GlobalVectorsForTest()...)
	want := nativeGlobals(t, oracle, vectors)
	panics := compareGlobalVectors(t, bin, vectors, want)
	total := 0
	var modes []int
	for mode := range panics {
		modes = append(modes, int(mode))
		total += panics[mode]
	}
	sort.Ints(modes)
	// Modes 2 to 5 select panicking and completing vectors; the others must
	// complete.
	for mode := byte(0); mode < globalModes; mode++ {
		switch mode {
		case 2, 3, 4, 5:
			if panics[mode] == 0 {
				t.Errorf("mode %d: no vector panics", mode)
			}
		default:
			if panics[mode] != 0 {
				t.Errorf("mode %d: %d vectors panic", mode, panics[mode])
			}
		}
	}
	t.Logf("%d vectors (%d panics: %v) match native Go, each from fresh initialization", len(vectors), total, panics)
}
