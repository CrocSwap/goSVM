package compiler

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Compile-time evaluation of package initialization against native Go
// (docs/GO_GLOBALS.md#compile-time-evaluation): examples/const-init runs
// natively in a fresh process per vector, and as host C both with its
// initialization image (39 of its 42 steps evaluated at compile time; the
// rest, which change evaluated variables, at run time) and with every
// initializer at run time (Program.RuntimeInit). Output bytes, results and
// panics must match.

const constInitEntry = "gosvm/examples/const-init"

var constInitPackages = []string{constInitEntry, constInitEntry + "/num", constInitEntry + "/reg"}

// exampleProgram reads an example's packages (root module gosvm) from disk.
func exampleProgram(t *testing.T, dir, entry string, packages []string) *Program {
	t.Helper()
	p := &Program{Entry: entry}
	for _, path := range packages {
		d := filepath.Join(dir, strings.TrimPrefix(strings.TrimPrefix(path, entry), "/"))
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatal(err)
		}
		unit := PackageSources{Path: path}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			unit.Sources = append(unit.Sources, Source{Name: filepath.Join(d, e.Name()), Data: mustRead(t, filepath.Join(d, e.Name()))})
		}
		p.Packages = append(p.Packages, unit)
	}
	return p
}

func constInitProgram(t *testing.T) *Program {
	return exampleProgram(t, "../../examples/const-init", constInitEntry, constInitPackages)
}

// buildOracle builds an example's native oracle command.
func buildOracle(t *testing.T, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "oracle")
	args := []string{"build", "-o", bin}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	cmd := exec.Command("go", append(args, pkg)...)
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

// constInitVectors covers the dump (0), mutation scripts (1) and faults
// (2); some fault vectors panic.
func constInitVectors(seed int64, n int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	vectors := [][]byte{make([]byte, 128)}
	for j := 0; j < n; j++ {
		in := make([]byte, 128)
		rng.Read(in)
		in[0] = byte(j % 3)
		vectors = append(vectors, in)
	}
	return vectors
}

// ConstInitVectorsForTest returns the vectors the SBF test runs.
func ConstInitVectorsForTest() [][]byte { return constInitVectors(20261016, 120) }

func TestConstInitDifferential(t *testing.T) {
	oracle := buildOracle(t, "./examples/const-init/oracle")
	vectors := append(constInitVectors(20261017, 900), ConstInitVectorsForTest()...)
	want := nativeGlobals(t, oracle, vectors)
	checked, err := Check(constInitProgram(t))
	if err != nil {
		t.Fatal(err)
	}
	if img := checked.InitImage; img == nil || img.Steps != 39 || img.Total != 42 || img.Stop == nil || !strings.Contains(img.Stop.Reason, "maps keyed by *") {
		t.Fatalf("image: %+v", checked.InitImage)
	}
	panics := 0
	for _, runtimeInit := range []bool{false, true} {
		program := constInitProgram(t)
		program.RuntimeInit = runtimeInit
		c, err := CompileProgram(program)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(c), "gosvm_image"); got == runtimeInit {
			t.Fatalf("RuntimeInit %v: image emitted %v", runtimeInit, got)
		}
		bin := hostCompile(t, c, DefaultHeapBytes, quietLogDriver, false)
		p := compareGlobalVectors(t, bin, vectors, want)
		panics = 0
		for _, n := range p {
			panics += n
		}
	}
	if panics == 0 {
		t.Fatal("no vector panics")
	}
	t.Logf("%d vectors (%d panics) match native Go with and without compile-time evaluation", len(vectors), panics)
}

// quietLogDriver is allocDriver with a silent sol_log_: a panic's log line
// is not part of the native oracle's output.
var quietLogDriver = strings.Replace(allocDriver, "void abort(void)", "void sol_log_(const u8 *p,u64 n) {(void)p;(void)n;}\nvoid abort(void)", 1)

// heapDriver initializes once and prints the allocator's in-use word and
// the heap bytes it covers.
const heapDriver = `
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
void abort(void) {printf("P\n"); fflush(stdout); _Exit(0);}
void sol_log_(const u8 *p,u64 n) {}
int main(void){
 memset(gosvm_host_heap,0,sizeof gosvm_host_heap);
 gosvm_init();
 u64 used = gosvm_host_heap[0], base = (u64)GOSVM_HEAP_BASE;
 printf("%llu\n", used);
 for (u64 j = 1; j < used / 8; j++) {
  u64 x = gosvm_host_heap[j];
  if (x >= base && x < base + GOSVM_HEAP_BYTES) printf("h%llx,", x - base);
  else if (x >= 0x100000000ULL && x < 0x1000000000000ULL) printf("p,"); /* rodata; 48-bit user space (arm64 Linux loads near 0xaaaa...) */
  else printf("%llx,", x);
 }
 printf("\n");
 return 0;}`

// initHeap runs a program's initialization on the host and returns the
// in-use word and the heap bytes, with heap pointers made relative.
func initHeap(t *testing.T, program *Program) (string, string) {
	t.Helper()
	c, err := CompileProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, heapDriver, false)
	out := strings.Split(runHost(t, bin, ""), "\n")
	return out[0], out[1]
}

// The image reproduces the heap accounting of running every initializer:
// the allocator's in-use word after initialization is the same, so every
// out-of-memory boundary is; and the heap's live contents are the same.
func TestInitImageHeapAccounting(t *testing.T) {
	programs := map[string]func() *Program{
		"const-init":    func() *Program { return constInitProgram(t) },
		"global-values": func() *Program { return globalProgramSources(t, nil) },
		"global-value-types": func() *Program {
			p, err := ReadProgram("../../examples/global-value-types")
			if err != nil {
				t.Fatal(err)
			}
			return p
		},
		"stdlib-values": func() *Program {
			p, err := ReadProgram("../../examples/stdlib-values")
			if err != nil {
				t.Fatal(err)
			}
			return p
		},
	}
	if _, err := os.Stat(filepath.Join(realLibsExample, "go.mod")); err == nil {
		programs["real-libraries"] = func() *Program {
			env := realLibs(t)
			p := *env.program
			return &p
		}
	}
	for name, make := range programs {
		t.Run(name, func(t *testing.T) {
			image := make()
			checked, err := Check(image)
			if err != nil {
				t.Fatal(err)
			}
			if checked.InitImage == nil {
				t.Fatal("no image")
			}
			runtimeInit := make()
			runtimeInit.RuntimeInit = true
			usedImage, heapImage := initHeap(t, image)
			usedRuntime, heapRuntime := initHeap(t, runtimeInit)
			if usedImage != usedRuntime {
				t.Fatalf("in-use word %s with the image, %s without", usedImage, usedRuntime)
			}
			if fmt.Sprint(checked.InitImage.HeapUsed) != usedImage && checked.InitImage.Steps == checked.InitImage.Total {
				t.Fatalf("InitImage.HeapUsed %d, heap %s", checked.InitImage.HeapUsed, usedImage)
			}
			// The interpreter allocates in the generated code's order, so
			// every word of the globals block and of the image's objects is
			// the word running every initializer leaves there (heap pointers
			// compared as offsets, pointers to read-only data as such),
			// except in map slot arrays, whose layout depends on the
			// insertion history (any valid table answers alike).
			live := map[int64]bool{}
			for off := int64(8); off < 8+checked.GlobalBytes; off += 8 {
				live[off] = true
			}
			for _, o := range checked.InitImage.Objects {
				if o.Kind != InitObjectMapSlots {
					for off := o.Offset; off < o.Offset+o.Size; off += 8 {
						live[off] = true
					}
				}
			}
			a, b := strings.Split(heapImage, ","), strings.Split(heapRuntime, ",")
			compared := 0
			for i := range a {
				off := int64(8 * (i + 1))
				if !live[off] || i >= len(b) {
					continue
				}
				compared++
				if a[i] != b[i] {
					t.Errorf("heap word at %d: %s with the image, %s without", off, a[i], b[i])
				}
			}
			t.Logf("%s bytes in use after initialization (%d of %d steps at compile time); %d live words identical", usedImage, checked.InitImage.Steps, checked.InitImage.Total, compared)
		})
	}
}
