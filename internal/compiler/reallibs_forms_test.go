package compiler

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// The language forms the real libraries needed (results/compiler/
// 2026-10-16-real-libraries): elided &T{...} elements of array type
// (holiman/uint256's and int256's `var multipliers = [5]*Int{nil, {...}}`),
// read-only pointer parameters keeping a package-level table read-only
// (uint256's `z.Lt(&pows[t-20])` in Log10) and pointer conversions
// (uniswapv3-sdk-uint256's `(*int256.Int)(&tmp)`). Each program runs natively
// (go build of the same source), as host C and on SBF for both targets.

// formPrograms define run(o, in []byte) uint64, writing to o.
var formPrograms = map[string]string{}

func init() {
	formPrograms["elided-array-pointers"] = `package main

type Int [4]uint64
type Pair [2]uint32
type Grid [2][2]uint8

var mult = [5]*Int{nil, {10}, {100, 1}, {1000, 2, 3}, 4: {0, 0, 0, 7}}
var byKey = map[uint8]*Pair{1: {5, 6}, 2: {7}}
var shared = mult[2]

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	local := []*Int{{uint64(in[0])}, nil, {1, 2, 3, 4}, 5: {uint64(in[2]), uint64(in[3])}}
	for _, p := range local {
		if p == nil {
			j = put(o, j, 0xdd)
			continue
		}
		j = put(o, j, p[0]+p[1]<<8+p[3]<<16)
	}
	// Each element is its own allocation.
	same := []*Pair{{1}, {1}}
	same[0][0] = uint32(in[4])
	j = put(o, j, uint64(same[0][0])<<32|uint64(same[1][0]))
	// Nested: pointers to arrays of pointers to arrays, and arrays of
	// arrays behind one pointer.
	nested := []*[2]*Int{{{1}, nil}, {nil, {2, uint64(in[5])}}}
	j = put(o, j, nested[0][0][0]+nested[1][1][1]<<8)
	grid := []*Grid{{{in[6], 2}, {3, 4}}}
	j = put(o, j, uint64(grid[0][0][0])+uint64(grid[0][1][1])<<8)
	// Package-level tables: written through their pointers, with an alias.
	mult[2][0] += uint64(in[7])
	j = put(o, j, mult[1][0]+mult[2][0]<<16+mult[3][2]<<32+mult[4][3]<<40)
	j = put(o, j, shared[0])
	if mult[0] == nil {
		j = put(o, j, 1)
	}
	byKey[uint8(in[8]%3)] = &Pair{uint32(in[9])}
	for k := uint8(0); k < 3; k++ {
		if p := byKey[k]; p != nil {
			j = put(o, j, uint64(p[0])<<32|uint64(p[1]))
		} else {
			j = put(o, j, 0xee)
		}
	}
	// A map literal whose values are elided array pointers.
	m := map[string]*[3]uint8{"a": {in[10], 1}, "b": {2: in[11]}}
	j = put(o, j, uint64(m["a"][0])+uint64(m["b"][2])<<8+uint64(len(m))<<16)
	return uint64(j)
}
`
	formPrograms["read-only-pointer-params"] = `package main

type Int [4]uint64

func (z *Int) Lt(x *Int) bool {
	if z[3] != x[3] {
		return z[3] < x[3]
	}
	if z[2] != x[2] {
		return z[2] < x[2]
	}
	if z[1] != x[1] {
		return z[1] < x[1]
	}
	return z[0] < x[0]
}

func (z *Int) Words() uint64 { return z[0] ^ z[1] ^ z[2] ^ z[3] }

// low reads through its parameter only, and passes it on to another
// read-only parameter.
func low(x *Int) uint64      { return x[0] + high(x) }
func high(x *Int) uint64     { return x[3] }
func field(p *uint64) uint64 { return *p }

// setFirst writes through its parameter: tables it reaches are writable.
func setFirst(x *Int, v uint64) { x[0] = v }

// A 1,920-byte table read in place and through read-only pointers, like
// holiman/uint256's pows in Log10.
var pows = [60]Int{{7766279631452241920, 5}, {3875820019684212736, 54}, 30: {1, 2, 3, 4}, 59: {0, 0, 0, 1 << 63}}

// A small table passed to read-only parameters.
var small = [3]Int{{1}, {2, 2}, {3, 3, 3}}

// A table written through a pointer parameter.
var written = [2]Int{{5}, {6}}

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	var z Int
	z[0], z[1], z[3] = uint64(in[0])<<56, uint64(in[1]), uint64(in[2])>>4
	t := int(in[3] % 60)
	if z.Lt(&pows[t]) {
		j = put(o, j, 1)
	}
	j = put(o, j, low(&pows[t])+pows[t].Words()+field(&pows[t][1]))
	j = put(o, j, low(&small[in[4]%3])+small[in[4]%3].Words())
	setFirst(&written[in[5]%2], uint64(in[6]))
	j = put(o, j, written[0][0]<<8|written[1][0])
	return uint64(j)
}
`
	formPrograms["pointer-conversions"] = `package main

type A [4]uint64
type B [4]uint64
type S struct{ X, Y uint32 }
type T struct{ X, Y uint32 }

func (b *B) Sum() uint64 { return b[0] + b[1] + b[2] + b[3] }

func sum(b *B) uint64 { return b.Sum() }

// escape returns a converted pointer to a local: the local moves to the
// heap, as in Go.
func escape(v uint64) *B {
	var l A
	l[2] = v
	return (*B)(&l)
}

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	var a A
	a[0] = uint64(in[0])
	b := (*B)(&a)
	b[1] = uint64(in[1]) // visible through a
	j = put(o, j, a[1]+b[0]<<8)
	var s S
	tp := (*T)(&s)
	tp.Y = uint32(in[2])
	j = put(o, j, uint64(s.Y))
	var np *A
	if (*B)(np) == nil && (*B)(nil) == nil {
		j = put(o, j, 1)
	}
	p := (*B)(&a)
	q := (*A)(p)
	q[3] = 9
	j = put(o, j, a[3]+sum((*B)(&a))<<8+(*B)(q).Sum()<<24)
	e1, e2 := escape(uint64(in[3])), escape(uint64(in[4]))
	j = put(o, j, e1[2]+e2[2]<<8)
	if in[5]%2 == 0 {
		np = &a
	}
	// A nil conversion result traps when dereferenced, as in Go.
	j = put(o, j, (*B)(np)[0])
	return uint64(j)
}
`
}

// formHostSource and formSBFSource add the entrypoints.
func formHostSource(src string) string {
	return src + "\nfunc Process(o, in []byte) uint64 { return run(o, in) }\n"
}

func formSBFSource(src string) string {
	src = strings.Replace(src, "package main\n", "package main\n\nimport \"gosvm/solana\"\n", 1)
	return src + "\nfunc Process(c solana.Context) uint64 { run(solana.Data(c, 0), solana.Instruction(c)); return 0 }\n"
}

// formMain is formNative's driver.
const formMain = `package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
)

func call(in []byte) (line string) {
	out := make([]byte, 1024)
	defer func() {
		if recover() != nil {
			line = fmt.Sprintf("P:%x\n", out)
		}
	}()
	r := run(out, in)
	return fmt.Sprintf("%d:%x\n", r, out)
}

func main() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		in, _ := hex.DecodeString(s.Text())
		fmt.Print(call(in))
	}
}
`

// formNative builds src natively and runs it over the inputs, one fresh
// process per input (every SBF invocation initializes the package
// variables anew): one line per input, "result:hex" over the 1 KiB output,
// or "P:hex" when it panics.
func formNative(t *testing.T, src string, inputs [][]byte) []string {
	t.Helper()
	return formNativeMain(t, src, formMain, inputs)
}

// formNativeMain is formNative with its own main package source.
func formNativeMain(t *testing.T, src, main string, inputs [][]byte) []string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string]string{"go.mod": "module forms\n\ngo 1.22\n", "prog.go": src, "main.go": main} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "native")
	args := []string{"build", "-o", bin}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native build: %v\n%s", err, out)
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("codesign", "--force", "--sign", "-", bin).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v\n%s", err, out)
		}
	}
	lines := make([]string, len(inputs))
	for j, in := range inputs {
		run := exec.Command(bin)
		run.Stdin = strings.NewReader(fmt.Sprintf("%x\n", in))
		out, err := run.Output()
		if err != nil {
			t.Fatalf("native run: %v", err)
		}
		lines[j] = string(out)
	}
	return lines
}

func formInputs() [][]byte {
	rng := rand.New(rand.NewSource(20261016))
	var inputs [][]byte
	for j := 0; j < 64; j++ {
		in := make([]byte, 128)
		rng.Read(in)
		if j < 8 {
			for k := range in[:16] {
				in[k] = byte(j * k)
			}
		}
		inputs = append(inputs, in)
	}
	return inputs
}

func TestRealLibraryFormsDifferential(t *testing.T) {
	inputs := formInputs()
	for name, src := range formPrograms {
		t.Run(name, func(t *testing.T) {
			want := formNative(t, src, inputs)
			c, err := CompileProgram(globalsProgram(unitFiles("program", formHostSource(src))))
			if err != nil {
				t.Fatal(err)
			}
			bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
			panics := 0
			for j, in := range inputs {
				got := runHost(t, bin, fmt.Sprintf("%x\n", in))
				if got != want[j] {
					t.Fatalf("input %d:\nC  %.300q\nGo %.300q", j, got, want[j])
				}
				if strings.HasPrefix(got, "P:") {
					panics++
				}
			}
			t.Logf("%d inputs (%d panics) match native Go", len(inputs), panics)
		})
	}
}

// The checked program's view: the large table stays read-only, the written
// table does not, and the converted local escapes to the heap.
func TestRealLibraryFormsCheck(t *testing.T) {
	checked, err := Check(globalsProgram(unitFiles("program", formHostSource(formPrograms["read-only-pointer-params"]))))
	if err != nil {
		t.Fatal(err)
	}
	readOnly := map[string]bool{}
	for _, g := range checked.Globals {
		readOnly[g.Var.Name()] = g.ReadOnly
	}
	if !readOnly["pows"] || !readOnly["small"] || readOnly["written"] {
		t.Fatalf("read-only placement: %v", readOnly)
	}
	checked, err = Check(globalsProgram(unitFiles("program", formHostSource(formPrograms["pointer-conversions"]))))
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range checked.Functions {
		if fn.Object.Name() == "escape" && len(fn.HeapVars) != 1 {
			t.Fatalf("escape: %d heap moves, want 1", len(fn.HeapVars))
		}
		if fn.Object.Name() == "run" && len(fn.HeapVars) != 0 {
			t.Fatalf("run: %d heap moves, want 0", len(fn.HeapVars))
		}
	}
}

func TestRealLibraryFormsSBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	inputs := formInputs()[:24]
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			for name, src := range formPrograms {
				want := formNative(t, src, inputs)
				var cases []allocCase
				for j, in := range inputs {
					c := allocCase{name: fmt.Sprintf("%s-%02d", name, j), in: in, err: abortError, log: abortLog, noPanicLog: true}
					if !strings.HasPrefix(want[j], "P:") {
						_, hexOut, _ := strings.Cut(strings.TrimSpace(want[j]), ":")
						out, err := hex.DecodeString(hexOut)
						if err != nil {
							t.Fatal(err)
						}
						c.output = out
					}
					cases = append(cases, c)
				}
				runGlobalProgram(t, llvm, runner, profile, name, globalsProgram(unitFiles("program", formSBFSource(src))), cases, "")
			}
		})
	}
}
