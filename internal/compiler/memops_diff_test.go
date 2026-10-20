package compiler

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	memoryvalues "gosvm/examples/memory-values"
)

// Bulk memory moves (memops.go) against native Go: examples/memory-values
// runs natively and as host-compiled C for every vector here; the SBF test
// (memops_sbf_test.go) runs the same vectors on both profiles, where moves
// from the thresholds use sol_memmove_/sol_memset_. Host C always takes the
// loops, so it proves the routines' decisions and the small-size paths; only
// the SBF runs exercise the syscalls.

func memoryValuesSource(t *testing.T) []byte {
	return mustRead(t, "../../examples/memory-values/program.go")
}

// memoryDriver runs Process on an OutBytes output prefilled with Pattern and
// an InBytes input per line, resetting the host heap per call.
var memoryDriver = fmt.Sprintf(`
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static u8 driver_out[%[1]d];
static void print_out(void) {for (u64 j=0;j<%[1]d;j++) printf("%%02x",driver_out[j]); puts("");}
void abort(void) {printf("P:"); print_out(); fflush(stdout); _Exit(0);}
static u8 nibble(char c){return c<='9'?c-'0':c-'a'+10;}
int main(void){char ih[%[3]d];u8 in[%[2]d];
while(scanf("%%%[4]ds",ih)==1){
 for(u64 j=0;j<%[2]d;j++)in[j]=(nibble(ih[j*2])<<4)|nibble(ih[j*2+1]);
 for(u64 j=0;j<%[1]d;j++)driver_out[j]=(u8)(j*131+(j>>8)+7);
 memset(gosvm_host_heap,0,sizeof gosvm_host_heap);
 printf("%%llu:",go_Process(GOSVM_SLICE(driver_out,%[1]d),GOSVM_SLICE(in,%[2]d)));print_out();
}return 0;}`, memoryvalues.OutBytes, memoryvalues.InBytes, 2*memoryvalues.InBytes+1, 2*memoryvalues.InBytes)

func memoryPattern() []byte {
	out := make([]byte, memoryvalues.OutBytes)
	for j := range out {
		out[j] = memoryvalues.Pattern(uint64(j))
	}
	return out
}

// nativeMemory runs Run natively: the output, or nil for a panic.
func nativeMemory(in []byte) (out []byte) {
	out = memoryPattern()
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	if code := memoryvalues.Run(out, append([]byte(nil), in...)); code != 0 {
		panic("Run returned nonzero")
	}
	return out
}

func nativeMemoryLine(in []byte) string {
	if out := nativeMemory(in); out != nil {
		return fmt.Sprintf("0:%x\n", out)
	}
	return fmt.Sprintf("P:%x\n", memoryPattern())
}

func memoryInput(mode, variant byte, a, b, c, d uint64) []byte {
	in := make([]byte, memoryvalues.InBytes)
	in[0], in[1] = mode, variant
	for j, v := range []uint64{a, b, c, d} {
		in[2+2*j], in[3+2*j] = byte(v), byte(v>>8)
	}
	return in
}

// memoryEdges are the byte counts around every threshold, plus small and
// large counts.
func memoryEdges() []uint64 {
	set := map[uint64]bool{}
	for _, n := range []uint64{0, 1, 2, 3, 7, 8, 9, 15, 16, 17, 24, 31, 32, 33, 40, 47, 48, 63, 64, 65, 96, 127, 128, 129, 255, 256, 257, 1000, 1024, 1500} {
		set[n] = true
	}
	// The mixed SBF variant's threshold (memops_sbf_test.go) and the chosen one.
	for _, v := range []uint64{memorySyscallMin, 64} {
		for _, n := range []uint64{v - 1, v, v + 1, v + 8, v - 8} {
			if n < 1<<15 {
				set[n] = true
			}
		}
	}
	var edges []uint64
	for n := uint64(0); n < 1<<15; n++ {
		if set[n] {
			edges = append(edges, n)
		}
	}
	return edges
}

// memoryVectors covers every mode. The systematic part pairs each edge
// count with aligned and unaligned addresses and overlaps in both
// directions; the random part adds out-of-range bounds (panics).
func memoryVectors(seed int64, random int) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	add := func(in []byte) { vectors = append(vectors, in) }
	const work = memoryvalues.WorkBytes
	for _, n := range memoryEdges() {
		for _, pos := range [][2]uint64{{0, 1024}, {1024, 0}, {8, 16}, {1, 9}, {3, 1029}, {16, 8}, {9, 1}, {100, 100}, {200, 201}, {201, 200}, {200, 207}, {207, 200}, {512, 512 + n/2}, {512 + n/2, 512}} {
			dst, src := pos[0], pos[1]
			if dst+n > work || src+n > work {
				continue
			}
			add(memoryInput(0, 0, dst, src, n, n))
			add(memoryInput(1, 0, dst, src, n, n+5))
			if n%3 == 0 {
				add(memoryInput(0, 1, dst, src, n, n))
				add(memoryInput(0, 2, dst, src, n, n))
				add(memoryInput(0, 3, dst, src, n, n))
				add(memoryInput(0, 0, dst, src, n, n/2)) // shorter source
				add(memoryInput(1, 1, dst, src, n, n))
			}
		}
		if n <= 1500 {
			add(memoryInput(2, 0, n%8, n%13, n, 0))
			add(memoryInput(2, 1, n%8+1, n%64, n, 0))
		}
		add(memoryInput(4, 0, n%900+20, n%900+40, n%7, n%9+n%7))
		add(memoryInput(4, 1, n%900+20, n%900+40, n%5, n%11))
		add(memoryInput(4, 2, n%600, n%600+100, n%300, n%300))
		add(memoryInput(4, 3, n, n+3, n%7, n%13))
		add(memoryInput(7, 0, n%300, n%400, n%250+3, n%200))
	}
	for _, n := range memoryEdges() {
		if n > work {
			continue
		}
		for variant := byte(0); variant < 9; variant++ {
			for _, start := range []uint64{0, 1, 3, 7, 8, n / 2, n, n + 1, n + 9} {
				if variant == 0 || variant >= 6 {
					if start+n > work {
						continue
					}
				}
				if (variant == 2 || variant == 4 || variant == 8) && start > 9 {
					continue
				}
				add(memoryInput(8, variant, start, n, start+n/3, 0))
			}
		}
	}
	for variant := byte(0); variant < 6; variant++ {
		for _, l := range []uint64{0, 1, 2, 3, 4, 5, 8, 16, 21, 32, 33, 60} {
			for _, ds := range [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {0, 5}, {5, 0}, {2, 3}, {7, 1}, {1, 7}} {
				add(memoryInput(3, variant, l, ds[0], ds[1], 0))
			}
		}
	}
	for variant := byte(0); variant < 9; variant++ {
		for i := uint64(0); i < 3; i++ {
			for j := uint64(0); j < 3; j++ {
				for k := uint64(0); k < 3; k++ {
					add(memoryInput(5, variant, i, j, k, 0))
				}
			}
		}
	}
	for variant := byte(0); variant < 6; variant++ {
		for i := uint64(0); i < 3; i++ {
			for j := uint64(0); j < 3; j++ {
				add(memoryInput(6, variant, i, j, 0, 0))
			}
		}
	}
	for j := 0; j < random; j++ {
		mode := byte(rng.Intn(memoryvalues.Modes))
		in := make([]byte, memoryvalues.InBytes)
		rng.Read(in)
		in[0] = mode
		pick := func(limit int) uint64 { return uint64(rng.Intn(limit)) }
		var a, b, c, d uint64
		switch mode {
		case 0, 1:
			n := pick(1200)
			a, b, c, d = pick(work-int(n)), pick(work-int(n)), n, pick(1300)
			if j%5 == 0 {
				a = pick(work + 100) // sometimes out of range
			}
		case 2:
			a, b, c = pick(work-1500), pick(2100), pick(1500)
		case 3:
			a, b, c = pick(80), pick(80), pick(80)
		case 4:
			a, b, c, d = pick(1500), pick(1600), pick(1600), pick(1700)
		case 5, 6:
			a, b, c = pick(3), pick(3), pick(3)
		case 7:
			a, b, c, d = pick(1600), pick(700), pick(1600), pick(700)
		case 8:
			a, b, c = pick(1600), pick(1400), pick(800)
		}
		copy(in, memoryInput(mode, in[1], a, b, c, d)[:10])
		add(in)
	}
	return vectors
}

// MemoryVectorsForTest are the SBF vectors (both profiles).
func MemoryVectorsForTest() [][]byte { return memoryVectors(20261019, 300) }

// TestMemoryDifferential runs examples/memory-values natively and as host C
// (SDK 2's word routines and the legacy byte routines) on the SBF vectors
// and more random ones.
func TestMemoryDifferential(t *testing.T) {
	t.Parallel()
	for _, sdk := range []int{0, 2} {
		t.Run(fmt.Sprintf("sdk%d", sdk), func(t *testing.T) {
			c, err := CompileProgram(&Program{SDK: sdk, Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: memoryValuesSource(t)}}}}})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(c, []byte("GOSVM_MEMORY_SYSCALL_MOVE(dst, src, n)")) || !bytes.Contains(c, []byte("GOSVM_MEMORY_SYSCALL_SET(dst,")) {
				t.Fatal("emitted C lacks the syscall paths")
			}
			bin := hostCompile(t, c, DefaultHeapBytes, memoryDriver, false)
			vectors := append(MemoryVectorsForTest(), memoryVectors(7, 3000)...)
			var input strings.Builder
			var want []string
			panics := 0
			for _, in := range vectors {
				fmt.Fprintf(&input, "%x\n", in)
				line := nativeMemoryLine(in)
				if strings.HasPrefix(line, "P:") {
					panics++
				}
				want = append(want, line)
			}
			// A panic ends the host process: run each vector after one again.
			got := strings.SplitAfter(runHostAll(t, bin, vectors), "\n")
			for j := range want {
				if j >= len(got) || got[j] != want[j] {
					g := ""
					if j < len(got) {
						g = got[j][:min(len(got[j]), 80)]
					}
					t.Fatalf("vector %d (%x):\nC  %q\nGo %q", j, vectors[j], g, want[j][:80])
				}
			}
			if panics < 20 || panics > len(vectors)/4 {
				t.Fatalf("vectors need panics and completions: %d of %d panic", panics, len(vectors))
			}
			t.Logf("%d vectors (%d panics) match native Go", len(vectors), panics)
		})
	}
}

// runHostAll runs every vector, restarting the host binary after a panic.
func runHostAll(t *testing.T, bin string, vectors [][]byte) string {
	t.Helper()
	var all strings.Builder
	for start := 0; start < len(vectors); {
		var batch strings.Builder
		for _, in := range vectors[start:] {
			fmt.Fprintf(&batch, "%x\n", in)
		}
		out := runHost(t, bin, batch.String())
		lines := strings.SplitAfter(out, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) == 0 {
			t.Fatal("host run printed nothing")
		}
		for _, l := range lines {
			all.WriteString(l)
		}
		start += len(lines)
	}
	return all.String()
}

// The syscall paths must compile cleanly where they are used: each routine
// set compiled for SBF with the modern define, and the standalone memmove
// helper, under -Werror.
func TestMemorySyscallRoutinesCompileForSBF(t *testing.T) {
	t.Parallel()
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	prefix := "typedef unsigned char u8;\ntypedef unsigned long long u64;\n"
	sources := map[string]string{
		"memmove": memmoveHelper(),
		"words":   prefix + memorySyscallDecls() + arrayMemoryWords,
		"bytes":   prefix + memorySyscallDecls() + arrayMemory + libraryMemory(),
	}
	for name, source := range sources {
		for _, cpu := range []string{"generic", "v3"} {
			dir := t.TempDir()
			file := filepath.Join(dir, name+".c")
			if err := os.WriteFile(file, []byte(source+"\nvoid abort(void);\n"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-target", "sbf", "-mcpu=" + cpu, "-O2", "-fno-builtin", "-fPIC", "-std=c11", "-Werror", "-Wno-unused-function", "-DGOSVM_MEMORY_SYSCALLS=1", "-c", file, "-o", filepath.Join(dir, "out.o")}
			if cpu == "v3" {
				args = append(args, "-DGOSVM_SBF_V3=1")
			}
			if out, err := exec.Command(filepath.Join(llvm, "bin", "clang"), args...).CombinedOutput(); err != nil {
				t.Fatalf("%s %s: %v %s", name, cpu, err, out)
			}
		}
	}
}

// TestZeroLoopForms checks which loops become a memset on SBF
// (memclear.go): the two zeroing forms do; near misses compile as written.
func TestZeroLoopForms(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]bool{
		`for i := range s { s[i] = 0 }`:                            true,
		`for i := range w { w[i] = 0 }`:                            true,
		`for i := range f { f[i] = false }`:                        true,
		`for j := uint64(3); j < uint64(len(s)); j++ { s[j] = 0 }`: true,
		`for j := n; j < uint64(len(s)); j++ { s[j] = 0 }`:         true,
		`for j := uint(n); j < uint(len(w)); j++ { w[j] = 0 }`:     true,
		`for j := n; j < uint64((len(s))); j++ { s[j] = 0 }`:       true,
		`for i := range s { s[i] = 1 }`:                            false,
		`for i, v := range s { s[i] = v & 0 }`:                     false,
		`for i := range s { s[i] = 0; n++ }`:                       false,
		`for i := range s { t[i] = 0 }`:                            false,
		`for i := range s[1:] { s[i] = 0 }`:                        false,
		`var i int; for i = range s { s[i] = 0 }; n = uint64(i)`:   false,
		`for j := int(n); j < len(s); j++ { s[j] = 0 }`:            false,
		`for j := uint32(n); j < uint32(len(s)); j++ { s[j] = 0 }`: false,
		`for j := n; j <= uint64(len(s)); j++ { s[j] = 0 }`:        false,
		`for j := n; j < uint64(len(t)); j++ { s[j] = 0 }`:         false,
		`for j := n; j < uint64(len(s)); j += 2 { s[j] = 0 }`:      false,
		`for j := n; j < uint64(len(s)); j++ { s[n] = 0 }`:         false,
		`for i := range a { a[i] = 0 }`:                            false, // arrays: not a slice
	} {
		source := "package p\nfunc Process(s, t []byte) uint64 {\n w := make([]uint32, 4)\n f := make([]bool, 3)\n var a [40]byte\n n := uint64(len(t))\n" + body + "\n return n + uint64(w[0]) + uint64(a[1]) + uint64(len(f))\n}\n"
		c, err := CompileProgram(&Program{Entry: "p", Packages: []PackageSources{{"p", []Source{{Name: "p.go", Data: []byte(source)}}}}})
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if got := bytes.Contains(c, []byte("(void)memset(")); got != want {
			t.Errorf("%s: memset %v, want %v", body, got, want)
		}
	}
}
