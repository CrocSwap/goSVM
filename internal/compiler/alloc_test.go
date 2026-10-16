package compiler

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	allocvalues "gosvm/examples/alloc-values"
)

// allocMessage checks one file and returns the diagnostic without its
// file:line:col prefix, or "" when the program is accepted.
func allocMessage(t *testing.T, source string) string {
	t.Helper()
	_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(source)}}}}})
	if err == nil {
		return ""
	}
	message := err.Error()
	if !strings.HasPrefix(message, "alloc.go:") {
		t.Fatalf("diagnostic is not source-located: %q", message)
	}
	return strings.SplitN(message, ": ", 2)[1]
}

// hostHeapHeader replaces the SBF heap region with a host array in tests.
func hostHeapHeader(bytes int) string {
	return fmt.Sprintf("static unsigned long long gosvm_host_heap[%d];\n#define GOSVM_HEAP_BASE ((unsigned char *)gosvm_host_heap)\n", bytes/8)
}

// hostCompile compiles generated C with a host heap and a driver.
func hostCompile(t *testing.T, c []byte, heapBytes int, driver string, syntaxOnly bool) string {
	t.Helper()
	cc, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("host Clang is needed for allocation tests")
	}
	dir := t.TempDir()
	file, bin := filepath.Join(dir, "alloc.c"), filepath.Join(dir, "alloc")
	if err = os.WriteFile(file, []byte(hostHeapHeader(heapBytes)+string(c)+driver), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c11", "-O2", "-Werror", file, "-o", bin}
	if syntaxOnly {
		args = []string{"-std=c11", "-Werror", "-fsyntax-only", file}
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return bin
}

const allocPrefix = `package p
type V struct{ N uint64 }
type K [4]uint64
type E struct{}
type Node struct {
	Next *Node
	Kids []*Node
	Data []byte
	N    uint64
}
func local() *uint64 { var n uint64; return &n }
func link(h *Node, n *Node) { h.Next = n }
func (v V) Addr() *uint64 { return &v.N }
func Process(s, i []byte) uint64 {
`

func TestAllocationAcceptedForms(t *testing.T) {
	for name, body := range map[string]string{
		"new scalar":           `p := new(uint64); *p = 1; s[0] = byte(*p)`,
		"new struct":           `v := new(V); v.N = 2; s[0] = byte(v.N)`,
		"new array":            `k := new(K); k[3] = 1; s[0] = byte(k[3])`,
		"new uint16":           `p := new(uint16); *p = 7; s[0] = byte(*p)`,
		"make len":             `w := make([]uint64, len(i)); w[0] = 1`,
		"make cap":             `w := make([]V, 1, 4); w[0].N = 3; s[0] = byte(cap(w))`,
		"make byte count":      `b := make([]byte, s[0], 300); s[1] = byte(len(b))`,
		"make constant":        `b := make([]bool, 3); b[2] = true`,
		"append values":        `var w []uint32; w = append(w, 1, 2, 3); s[0] = byte(w[2])`,
		"append spread":        `w := []uint16{1}; w = append(w, w...); s[0] = byte(len(w))`,
		"append bytes":         `b := append([]byte(nil), i...); s[0] = byte(len(b))`,
		"append no values":     `w := []uint64{1}; w = append(w); s[0] = byte(len(w))`,
		"copy bytes":           `n := uint64(copy(s, i)); s[0] = byte(n)`,
		"copy typed":           `a, b := make([]K, 2), []K{{1}}; copy(a, b); s[0] = byte(a[0][0])`,
		"keyed slice literal":  `w := []uint64{3: 1, 1: 2}; s[0] = byte(w[3] + uint64(len(w)))`,
		"slices of arrays":     `ks := [][32]byte{{1}}; ks[0][31] = 2; s[0] = ks[0][31]`,
		"slices of slices":     `g := [][]byte{i, nil}; g[1] = append(g[1], 1); s[0] = g[1][0]`,
		"slices of pointers":   `ps := []*V{{N: 1}, new(V)}; s[0] = byte(ps[0].N)`,
		"slices of structs":    `w := []V{{N: 1}}; w = append(w, V{2}); s[0] = byte(w[1].N)`,
		"pointer fields":       `n := &Node{N: 1}; n.Next = &Node{N: 2}; s[0] = byte(n.Next.N)`,
		"slice fields":         `var n Node; n.Data = i; n.Kids = append(n.Kids, &n); s[0] = byte(len(n.Kids))`,
		"nil fields":           `var n Node; n.Data = nil; n.Kids = nil; n.Next = nil; s[0] = byte(len(n.Data))`,
		"element field target": `w := make([]V, 2); w[1].N = 5; w[1].N++; p := &w[0].N; *p = 1; s[0] = byte(w[1].N)`,
		"typed views":          `var k K; v := k[1:3:4]; v[0] = 1; w := v[:2]; s[0] = byte(cap(w))`,
		"range typed":          `for j, v := range []uint64{1, 2} { s[j] = byte(v) }`,
		"range pointers":       `for _, p := range []*V{new(V)} { p.N = 1 }`,
		"return local":         `s[0] = byte(*local())`,
		"outer scope":          `var p *uint64; { n := uint64(1); p = &n }; s[0] = byte(*p)`,
		"loop locals":          `var ps []*uint64; for j := uint64(0); j < 3; j++ { v := j; ps = append(ps, &v) }; s[0] = byte(*ps[2])`,
		"stack into heap":      `h := new(Node); var l Node; h.Next = &l; s[0] = byte(h.Next.N)`,
		"parameter into heap":  `var a, b Node; link(&a, &b); s[0] = byte(a.Next.N)`,
		"value receiver":       `v := V{N: 4}; s[0] = byte(*v.Addr())`,
		"slice conversion":     `w := []uint64(nil); w = []uint64(w); s[0] = byte(len(w))`,
		"slice comparison":     `var w []V; if w == nil { s[0] = 1 }`,
		"multiple results":     `a, b := make([]uint64, 1), []*V{}; s[0] = byte(uint64(len(a)) + uint64(len(b)))`,
	} {
		t.Run(name, func(t *testing.T) {
			source := allocPrefix + body + "\nreturn 0 }\n"
			if got := allocMessage(t, source); got != "" {
				t.Fatalf("rejected: %s", got)
			}
			c, err := Compile("alloc.go", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			hostCompile(t, c, DefaultHeapBytes, "", true)
		})
	}
}

func TestAllocationRejections(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"make channel":        {`ch := make(chan uint64); _ = ch`, "make supports only slices and maps"},
		"new int":             {`p := new(int); _ = p`, ""},
		"int slice":           {`var w []int; _ = w`, ""},
		"empty struct slice":  {`var w []E; _ = w`, "slice elements must not contain zero-length arrays or empty structs"},
		"zero array slice":    {`var w [][0]byte; _ = w`, "slice elements must not contain zero-length arrays or empty structs"},
		"clear":               {`clear(s)`, "clear supports only maps"},
		"min":                 {`x := min(s[0], 1); _ = x`, builtinRule},
		"panic":               {`panic(1)`, ""},
		"elided array":        {`ps := []*K{{1}}; _ = ps`, ""}, // lowered to &K{1} (elided.go)
		"elided map pointer":  {`ps := []*map[uint8]uint8{{1: 2}}; _ = ps`, "elided &T{...} literals require a struct type; write &T{...}"},
		"pointer to slice":    {`p := new([]byte); _ = p`, "pointer elements must be integer/bool scalars, value structs or scalar arrays"},
		"pointer to pointer":  {`p := new(*V); _ = p`, ""},
		"array of pointers":   {`var a [2]*V; _ = a`, ""},
		"array of slices":     {`var a [2][]byte; _ = a`, ""},
		"int copy result":     {`n := copy(s, i); _ = n`, ""},
		"slice compare":       {`a, b := []V{}, []V{}; _ = a == nil && b == nil; s[0] = 1; var w []uint64; _ = w`, ""},
		"compound append":     {`var w []uint64; w = append(w, 1); w[0] += 1; s[0] = byte(w[0])`, ""},
		"struct compare":      {`var a, b V; _ = a.N == b.N; s[0] = 1; _ = a == b`, ""},
		"signed make length":  {`w := make([]uint64, len(s)-1); _ = w`, ""},
		"zero struct pointer": {`p := new(E); _ = p`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			source := allocPrefix + c.body + "\nreturn 0 }\n"
			if got := allocMessage(t, source); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if _, err := Compile("alloc.go", []byte(source)); (err == nil) != (c.want == "") || err != nil && !strings.HasSuffix(err.Error(), c.want) {
				t.Fatalf("CompileProgram disagrees with Check: %v", err)
			}
		})
	}
	for name, c := range map[string]struct{ source, want string }{
		"named slice":    {"package p\ntype L []uint64\nfunc Process(s, i []byte) uint64 { return 0 }\n", "unsupported type program.L"},
		"context slices": {"package p\nimport \"gosvm/solana\"\nfunc Process(c solana.Context) uint64 { var cs []solana.Context; _ = cs; return 0 }\n", "slices of Context unsupported"},
		"context field":  {"package p\nimport \"gosvm/solana\"\ntype C struct{ c *solana.Context }\nfunc Process(c solana.Context) uint64 { return 0 }\n", "pointers to Context unsupported"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := allocMessage(t, c.source); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
	// heap_bytes is validated before any source is read.
	for _, n := range []int{1, 1023, 31 * 1024, 33*1024 + 1, 257 * 1024} {
		_, err := Check(&Program{Entry: "program", HeapBytes: n, Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(allocPrefix + "return 0 }\n")}}}}})
		want := fmt.Sprintf("heap_bytes %d must be a multiple of 1024 from 32768 to 262144", n)
		if err == nil || err.Error() != want {
			t.Fatalf("heap_bytes %d: %v", n, err)
		}
	}
}

// Heap moves and parameter escapes are part of the checked program, so a
// backend allocates exactly the same storage.
func TestHeapMovesExported(t *testing.T) {
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(`package p
type N struct{ Next *N; V uint64 }
func keep(h *N, n *N) { h.Next = n }
func local() *uint64 { var x uint64; return &x }
func (n N) Addr() *uint64 { return &n.V }
func pass(p *uint64) *uint64 { return p }
func Process(s, i []byte) uint64 {
	var a, b N
	keep(&a, &b)
	var p *uint64
	{
		y := uint64(1)
		p = &y
	}
	q := &N{}
	r := &N{}
	a.Next = q
	z := uint64(3)
	_ = pass(&z)
	return *p + *local() + *b.Addr() + r.V
}
`)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, fn := range checked.Functions {
		var vars []string
		for _, v := range fn.HeapVars {
			vars = append(vars, v.Name())
		}
		got[fn.Object.Name()] = fmt.Sprintf("vars=%v lits=%d escapes=%v results=%+v", vars, len(fn.HeapLiterals), fn.Borrows.Escapes, fn.Borrows.Results)
	}
	want := map[string]string{
		"keep":    "vars=[] lits=0 escapes=[1] results=[]",
		"local":   "vars=[x] lits=0 escapes=[] results=[{Parameters:[] SDK:false Heap:true}]",
		"Addr":    "vars=[n] lits=0 escapes=[] results=[{Parameters:[] SDK:false Heap:true}]",
		"pass":    "vars=[] lits=0 escapes=[] results=[{Parameters:[0] SDK:false Heap:false}]",
		"Process": "vars=[b y] lits=1 escapes=[] results=[{Parameters:[] SDK:false Heap:false}]",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: got %s, want %s", name, got[name], w)
		}
	}
}

// Programs that do not allocate keep their exact C; the runtime and the
// configured budget appear only when used.
func TestAllocationRuntimeOnlyWhenUsed(t *testing.T) {
	without, err := Compile("a.go", []byte("package p\ntype B struct{ K [4]byte }\nfunc Process(s, i []byte) uint64 { var b B; v := b.K[:]; return uint64(len(v)) }\n"))
	if err != nil {
		t.Fatal(err)
	}
	with, err := Compile("a.go", []byte("package p\nfunc Process(s, i []byte) uint64 { b := make([]byte, 2); return uint64(len(b)) }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "gosvm_alloc") || !strings.Contains(string(with), "#define GOSVM_HEAP_BYTES 32768ULL") {
		t.Fatal("the heap runtime must appear exactly when allocation is used")
	}
	big, err := CompileProgram(&Program{Entry: "program", HeapBytes: 65536, Packages: []PackageSources{{"program", []Source{{Name: "a.go", Data: []byte("package p\nfunc Process(s, i []byte) uint64 { b := make([]byte, 2); return uint64(len(b)) }\n")}}}}})
	if err != nil || !strings.Contains(string(big), "#define GOSVM_HEAP_BYTES 65536ULL") {
		t.Fatalf("heap_bytes not applied: %v", err)
	}
}

// allocDriver runs Process on a 1 KiB output and a 128-byte input per line,
// resetting the host heap (as the SVM does for every invocation).
const allocDriver = `
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static u8 driver_out[1024];
static void print_out(void) {for (u64 j=0;j<1024;j++) printf("%02x",driver_out[j]); puts("");}
void abort(void) {printf("P:"); print_out(); fflush(stdout); _Exit(0);}
static u8 nibble(char c){return c<='9'?c-'0':c-'a'+10;}
int main(void){char ih[257];u8 in[128];
while(scanf("%256s",ih)==1){
 for(u64 j=0;j<128;j++)in[j]=(nibble(ih[j*2])<<4)|nibble(ih[j*2+1]);
 memset(driver_out,0,sizeof driver_out);
 memset(gosvm_host_heap,0,sizeof gosvm_host_heap);
 printf("%llu:",go_Process(GOSVM_SLICE(driver_out,1024),GOSVM_SLICE(in,128)));print_out();
}return 0;}`

func nativeAllocCall(in []byte) (result string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			result = fmt.Sprintf("P:%x\n", out)
		}
	}()
	code := allocvalues.Run(out, in)
	return fmt.Sprintf("%d:%x\n", code, out)
}

// allocVectors covers every mode of examples/alloc-values. limit bounds the
// growth vectors' capacity in bytes, so each vector's allocations fit the
// heap it runs with (native Go never runs out of memory).
func allocVectors(seed int64, perMode int, limit, maxMake uint64) [][]byte {
	rng := rand.New(rand.NewSource(seed))
	var vectors [][]byte
	add := func(in []byte) { vectors = append(vectors, in) }
	for kind := 0; kind < allocvalues.GrowKinds+1; kind++ {
		for j := 0; j < perMode; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0], in[1] = 0, byte(kind)
			in[2] = byte(1 + rng.Intn(100))
			l := uint64(rng.Int63n(int64(limit))) + 1
			if kind >= allocvalues.GrowKinds-1 {
				// The last kind is BigRef or W128 by the parity of in[4].
				in[1] = allocvalues.GrowKinds - 1
				l = l&^0x100 | uint64(kind-allocvalues.GrowKinds+1)<<8
			}
			in[3], in[4] = byte(l), byte(l>>8)
			if j%4 == 0 {
				for k := 5; k < 128; k++ {
					in[k] = byte(rng.Intn(4)) // only 1-3 value appends and short spreads
				}
			}
			add(in)
		}
	}
	for mode := byte(1); mode <= 8; mode++ {
		for j := 0; j < perMode*2; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = mode
			switch mode {
			case 1:
				n, m := uint64(rng.Int63n(int64(maxMake))), uint64(rng.Int63n(int64(maxMake)))
				if j%3 != 0 && n > m {
					n, m = m, n
				}
				in[1], in[2], in[3], in[4] = byte(n), byte(n>>8), byte(m), byte(m>>8)
				in[5] = byte(j % 7 / 6) // occasionally a length beyond Go's maxAlloc
			case 3:
				for k := 1; k <= 7; k++ {
					in[k] = byte(rng.Intn(20))
				}
			case 8:
				n := uint64(rng.Int63n(int64(maxMake) * 8))
				in[1], in[2], in[3] = byte(n), byte(n>>8), byte(n>>16)
			}
			add(in)
		}
	}
	return vectors
}

// AllocVectorsForTest returns SBF-sized vectors (heap 32 KiB) for the
// external SBF test.
func AllocVectorsForTest() [][]byte { return allocVectors(20261015, 3, 3000, 1500) }

// TestAllocationDifferential runs examples/alloc-values natively and through
// the generated C on the host with a 256 KiB heap: append len/cap/aliasing
// for 18 element kinds, make/new zero values and make traps, copy overlap,
// slicing bounds, linked structures, escaping locals, literals and aliasing.
func TestAllocationDifferential(t *testing.T) {
	source, err := os.ReadFile("../../examples/alloc-values/program.go")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileProgram(&Program{Entry: "program", HeapBytes: MaxHeapBytes, Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: source}}}}})
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, MaxHeapBytes, allocDriver, false)
	vectors := allocVectors(20261014, 12, 30000, 4000)
	var want []string
	var got strings.Builder
	var batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	panics := 0
	for _, in := range vectors {
		line := fmt.Sprintf("%x\n", in)
		result := nativeAllocCall(in)
		want = append(want, result)
		if strings.HasPrefix(result, "P:") {
			panics++
			flush()
			got.WriteString(runHost(t, bin, line))
			continue
		}
		batch.WriteString(line)
	}
	flush()
	if panics < 20 || panics > len(vectors)/2 {
		t.Fatalf("vectors need both panics and completions: %d of %d panic", panics, len(vectors))
	}
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
	t.Logf("%d vectors (%d panics) match native Go", len(vectors), panics)
}

// oomDriver prints how far a program got before aborting.
const oomDriver = `
#include <stdio.h>
#include <stdlib.h>
static u8 driver_out[16];
void abort(void) {printf("abort %llu\n", *(u64 *)driver_out); fflush(stdout); _Exit(0);}
int main(int argc,char **argv){u8 in[8]={0};u64 n=strtoull(argv[1],0,10);for(int j=0;j<8;j++)in[j]=(u8)(n>>(8*j));
 printf("ok %llu\n",go_Process(GOSVM_SLICE(driver_out,16),GOSVM_SLICE(in,8)));return 0;}`

func runOOM(t *testing.T, bin string, n uint64) string {
	t.Helper()
	out, err := exec.Command(bin, fmt.Sprint(n)).CombinedOutput()
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestOutOfMemoryBoundaries checks the budget arithmetic exactly: the first
// heap word holds the bytes in use, allocations round up to 8 bytes, and the
// first allocation that does not fit aborts. Native Go never runs out of
// memory at these sizes, so the expectations are computed, not observed.
func TestOutOfMemoryBoundaries(t *testing.T) {
	build := func(heap int, body string) string {
		source := "package p\nfunc load(i []byte) uint64 { n := uint64(0); for j := uint64(0); j < 8; j++ { n = n | uint64(i[j])<<(8*j) }; return n }\nfunc save(s []byte, v uint64) { for j := uint64(0); j < 8; j++ { s[j] = byte(v >> (8 * j)) } }\nfunc Process(s, i []byte) uint64 {\nn := load(i)\n" + body + "\n}\n"
		c, err := CompileProgram(&Program{Entry: "program", HeapBytes: heap, Packages: []PackageSources{{"program", []Source{{Name: "oom.go", Data: []byte(source)}}}}})
		if err != nil {
			t.Fatal(err)
		}
		h := heap
		if h == 0 {
			h = DefaultHeapBytes
		}
		return hostCompile(t, c, h, oomDriver, false)
	}
	// One make: 8 bytes of allocator state, then the array rounded to 8.
	makeOne := build(0, "b := make([]byte, n); b[n-1] = 1; return uint64(len(b))")
	for n, want := range map[uint64]string{1: "ok 1", 32760: "ok 32760", 32761: "abort 0", 40000: "abort 0"} {
		if got := runOOM(t, makeOne, n); got != want {
			t.Errorf("make([]byte, %d): %s, want %s", n, got, want)
		}
	}
	// new(uint64) takes 8 more bytes; 3 * n bytes of uint16 round up to 8.
	mixed := build(0, "p := new(uint64); *p = 1; save(s, 1); w := make([]uint16, n); save(s, 2); return uint64(len(w)) + *p")
	for n, want := range map[uint64]string{16376: "ok 16377", 16377: "abort 1", 16373: "ok 16374"} {
		if got := runOOM(t, mixed, n); got != want {
			t.Errorf("new + make([]uint16, %d): %s, want %s", n, got, want)
		}
	}
	// A local that escapes in every iteration allocates 8 bytes each time.
	loop := build(0, "var keep *uint64; for j := uint64(0); j < n; j++ { v := j; keep = &v; save(s, j+1) }; return *keep")
	if got := runOOM(t, loop, 4095); got != "ok 4094" {
		t.Errorf("4095 escaping iterations: %s", got)
	}
	if got := runOOM(t, loop, 4096); got != "abort 4095" {
		t.Errorf("4096 escaping iterations: %s", got)
	}
	// append growth: count the appends that succeed before the first growth
	// whose array does not fit, using the growth model.
	appends := build(0, "var b []byte; for j := uint64(0); j < n; j++ { b = append(b, 1); save(s, j+1) }; return uint64(len(b))")
	used, capacity, expected := uint64(8), uint64(0), uint64(0)
	for {
		if expected == capacity {
			next := modelCap(expected, capacity, 1, 1, false)
			if (next+7)&^7 > 32768-used {
				break // this append aborts
			}
			used, capacity = used+(next+7)&^7, next
		}
		expected++
	}
	if got := runOOM(t, appends, 100000); got != fmt.Sprintf("abort %d", expected) {
		t.Errorf("append bytes until out of memory: %s, want abort %d", got, expected)
	}
	// A 64 KiB budget doubles the room.
	large := build(65536, "b := make([]byte, n); b[n-1] = 1; return uint64(len(b))")
	for n, want := range map[uint64]string{65528: "ok 65528", 65529: "abort 0"} {
		if got := runOOM(t, large, n); got != want {
			t.Errorf("64 KiB make([]byte, %d): %s, want %s", n, got, want)
		}
	}
	// make's range checks: len > cap, and a capacity beyond Go's maxAlloc.
	ranges := build(0, "if n == 0 { b := make([]byte, 4, n); return uint64(len(b)) }; w := make([]uint64, n<<45); return uint64(len(w))")
	for n, want := range map[uint64]string{0: "abort 0", 8: "abort 0"} {
		if got := runOOM(t, ranges, n); got != want {
			t.Errorf("make range %d: %s, want %s", n, got, want)
		}
	}
}
