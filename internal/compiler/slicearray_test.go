package compiler

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	target "gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// Slice-to-array conversions (docs/GO_ARRAYS.md#slice-to-array-conversions):
// (*[N]T)(s) aliases the slice's backing array, [N]T(s) copies it, and both
// trap when len(s) < N. Each program runs natively (go build of the same
// source), as host C and on SBF for both targets, with the formPrograms
// harness (reallibs_forms_test.go): run(o, in []byte) uint64 writes to o,
// which is account data on SBF.
var sliceArrayPrograms = map[string]string{
	"slice-array-pointers": `package main

type Key [8]byte
type Pair struct {
	A uint32
	B uint16
}

// firstWord is the helper from CrocSwap/goSVM#7.
func firstWord(b []byte) uint64 {
	k := (*[8]byte)(b)
	return uint64(k[0]) | uint64(k[7])<<56
}

func First[T any](s []T) *[2]T { return (*[2]T)(s) }

// Second converts inside a generic function called from a generic function.
func Second[T any](s []T) T { return First(s)[1] }

// view returns a pointer into a local array: the array moves to the heap.
func view(v byte) *[4]byte {
	var a [8]byte
	a[3] = v
	return (*[4]byte)(a[2:])
}

var gs = make([]byte, 4)

func grow(v byte) byte {
	gs = make([]byte, 8)
	gs[0] = v
	return 1
}

func boom(n int) byte {
	if n < 12 {
		panic("boom")
	}
	return 0
}

func pick(p *[8]byte, x byte) byte { return p[0] + x }

// Package-level initializers convert too (at run time, not at compile time).
var gbuf = make([]byte, 8)
var gview = (*[4]byte)(gbuf[2:])

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	buf := make([]byte, 16)
	copy(buf, in[:16])
	// len == N and len > N.
	j = put(o, j, firstWord(buf[:8]))
	j = put(o, j, firstWord(buf[4:]))
	// Writes through either view are visible through the other.
	p := (*[4]byte)(buf[2:6])
	p[0] = in[16]
	buf[3] = in[17]
	j = put(o, j, uint64(buf[2])|uint64(p[1])<<8)
	// The pointer shares the backing array, not the slice header.
	s := buf[8:]
	q := (*[8]byte)(s)
	s = append(s[:0], 0xaa)
	j = put(o, j, uint64(q[0])|uint64(len(s))<<8)
	// Account data, through a named array type.
	k := (*Key)(o[512:])
	k[1] = in[18]
	o[514] = in[19]
	j = put(o, j, uint64(o[513])|uint64(k[2])<<8)
	// Other element types: integers, structs and arrays.
	words := []uint64{uint64(in[20]), 2, 3}
	w := First(words)
	w[1] += 10
	j = put(o, j, words[1]+w[0]<<8+Second(words[1:])<<16)
	pairs := []Pair{{A: uint32(in[21])}, {B: 7}, {B: uint16(in[22])}}
	pp := First(pairs[1:])
	pp[0].A = 99
	j = put(o, j, uint64(pairs[1].A)+uint64(pp[1].B)<<16+uint64(Second(pairs).A)<<32)
	grid := [][4]byte{{in[23]}, {1, 2, 3, 4}}
	g := (*[2][4]byte)(grid)
	g[1][2] = 0xee
	j = put(o, j, uint64(grid[1][2])|uint64(g[0][0])<<8|uint64(First(buf)[1])<<16)
	// A nil slice gives a nil pointer; a non-nil empty slice does not.
	var ns []byte
	if (*[0]byte)(ns) == nil {
		j = put(o, j, 1)
	}
	if (*[0]byte)(buf[16:]) != nil {
		j = put(o, j, 2)
	}
	if (*[0]byte)(make([]byte, 0)) != nil {
		j = put(o, j, 3)
	}
	// len of a converted pointer is a constant: nothing is evaluated.
	j = put(o, j, uint64(len((*[8]byte)(ns))))
	e1, e2 := view(in[24]), view(in[25])
	e1[1]++
	j = put(o, j, uint64(e1[1])|uint64(e2[1])<<8)
	gview[0] = in[26]
	j = put(o, j, uint64(gbuf[2]))
	// gc's order: the slice is read after the call in the same expression.
	gs = make([]byte, 4)
	j = put(o, j, uint64((*[8]byte)(gs)[0]+grow(in[27])))
	// Short slices trap, after the writes above.
	n := int(in[28] % 12)
	switch in[29] % 8 {
	case 0:
		j = put(o, j, firstWord(buf[:n]))
	case 1:
		j = put(o, j, Second(words[:n%3]))
	case 2:
		_ = (*[1]byte)(ns)
	case 3:
		// The call panics before the conversion traps, as in gc.
		j = put(o, j, uint64((*[8]byte)(buf[:n])[0]+boom(n)))
	case 4:
		j = put(o, j, uint64(pick((*[8]byte)(buf[:n]), boom(n))))
	}
	return uint64(j)
}
`,
	"slice-array-values": `package main

type Key [4]byte
type Pair struct {
	A uint32
	B uint16
}

func Head[T any](s []T) [2]T { return [2]T(s) }

func Tail[T any](s []T) T {
	h := Head(s[1:])
	return h[1]
}

var gs = make([]byte, 4)

func grow(v byte) byte {
	gs = make([]byte, 8)
	gs[0] = v
	return 1
}

func poke(v byte) byte {
	gs[0] = v
	return 0
}

func boom(n int) byte {
	if n < 12 {
		panic("boom")
	}
	return 0
}

var gbuf = []byte{1, 2, 3, 4, 5}
var garr = [4]byte(gbuf[1:])

func put(o []byte, j int, v uint64) int {
	for k := 0; k < 8; k++ {
		o[j+k] = byte(v >> (8 * k))
	}
	return j + 8
}

func run(o, in []byte) uint64 {
	j := 0
	buf := make([]byte, 16)
	copy(buf, in[:16])
	// len == N and len > N; the results are copies.
	a := [8]byte(buf[:8])
	b := [4]byte(buf[2:])
	a[0] = 0xff
	buf[2] = in[16]
	j = put(o, j, uint64(buf[0])|uint64(a[0])<<8|uint64(b[0])<<16|uint64(buf[2])<<24)
	// From account data, through a named array type.
	o[600] = in[17]
	k := Key(o[600:])
	o[600] = 7
	j = put(o, j, uint64(k[0])|uint64(o[600])<<8)
	h := Head([]uint64{uint64(in[18]), 5, 6})
	hp := Head([]Pair{{A: uint32(in[19])}, {B: uint16(in[20])}})
	grid := [2][2]byte([][2]byte{{in[21], 1}, {2, 3}})
	j = put(o, j, h[0]+h[1]<<8+uint64(hp[0].A)<<16+uint64(hp[1].B)<<24+uint64(grid[0][0])<<40)
	j = put(o, j, Tail([]uint64{1, 2, uint64(in[22])}))
	var ns []byte
	z := [0]byte(ns)
	j = put(o, j, uint64(len(z)))
	j = put(o, j, uint64([4]byte(buf)[3]))
	if [4]byte(buf) == [4]byte(buf[:4]) {
		j = put(o, j, 1)
	}
	garr[0]++
	j = put(o, j, uint64(garr[0])|uint64(gbuf[1])<<8)
	// gc's order: the elements are read after the call in the same
	// expression.
	gs = make([]byte, 4)
	j = put(o, j, uint64([4]byte(gs)[0]+poke(in[23])))
	gs = make([]byte, 4)
	j = put(o, j, uint64([8]byte(gs)[0]+grow(in[24])))
	// Short slices trap, after the writes above.
	n := int(in[25] % 12)
	switch in[26] % 8 {
	case 0:
		a = [8]byte(buf[:n])
		j = put(o, j, uint64(a[1]))
	case 1:
		j = put(o, j, Tail([]uint64{1, 2, 3}[:n%4]))
	case 2:
		_ = [1]byte(ns)
	case 3:
		// The call panics before the conversion traps, as in gc.
		j = put(o, j, uint64([8]byte(buf[:n])[0]+boom(n)))
	}
	return uint64(j)
}
`,
}

// sliceArrayMain is formMain, printing a panic call's log line first as
// valueTypesDriver does ("L:hex " before "P:"); a runtime error is a trap,
// which logs nothing.
const sliceArrayMain = `package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
)

func call(in []byte) (line string) {
	out := make([]byte, 1024)
	defer func() {
		if r := recover(); r != nil {
			line = fmt.Sprintf("P:%x\n", out)
			if s, ok := r.(string); ok {
				line = fmt.Sprintf("L:%x %s", "panic: "+s, line)
			}
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

// sliceArrayNative runs src natively: the expected host lines, and each
// failure's panic line ("" for a trap or a completion).
func sliceArrayNative(t *testing.T, src string, inputs [][]byte) (lines, panics []string) {
	t.Helper()
	lines = formNativeMain(t, src, sliceArrayMain, inputs)
	for _, line := range lines {
		msg := ""
		if logged, ok := strings.CutPrefix(line, "L:"); ok {
			encoded, _, _ := strings.Cut(logged, " ")
			b, err := hex.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			msg = string(b)
		}
		panics = append(panics, msg)
	}
	return lines, panics
}

func sliceArrayInputs(count int) [][]byte {
	rng := rand.New(rand.NewSource(20261019))
	var inputs [][]byte
	for j := 0; j < count; j++ {
		in := make([]byte, 128)
		rng.Read(in)
		if j < 16 {
			// Every trap selector, each with a short and a long slice.
			in[25], in[26], in[28], in[29] = byte(j*5), byte(j/2), byte(j*5), byte(j/2)
		}
		inputs = append(inputs, in)
	}
	return inputs
}

func TestSliceArrayDifferential(t *testing.T) {
	inputs := sliceArrayInputs(256)
	for name, src := range sliceArrayPrograms {
		t.Run(name, func(t *testing.T) {
			want, panicLines := sliceArrayNative(t, src, inputs)
			c, err := CompileProgram(globalsProgram(unitFiles("program", formHostSource(src))))
			if err != nil {
				t.Fatal(err)
			}
			bin := hostCompile(t, c, DefaultHeapBytes, valueTypesDriver, false)
			panics, logged := 0, 0
			for j, in := range inputs {
				got := runHost(t, bin, fmt.Sprintf("%x\n", in))
				if got != want[j] {
					t.Fatalf("input %d:\nC  %.300q\nGo %.300q", j, got, want[j])
				}
				if strings.Contains(got, "P:") {
					panics++
				}
				if panicLines[j] != "" {
					logged++
				}
			}
			if panics == 0 || panics == len(inputs) || logged == 0 {
				t.Fatalf("%d of %d inputs fail, %d with a panic call; want every outcome", panics, len(inputs), logged)
			}
			t.Logf("%d inputs (%d failures, %d of them panic calls) match native Go", len(inputs), panics, logged)
		})
	}
}

// The checked program's view: a converted pointer into a returned local
// moves the local to the heap; the slice of a package-level variable makes
// it writable; and the initializers converting slices run at run time.
func TestSliceArrayCheck(t *testing.T) {
	checked, err := Check(globalsProgram(unitFiles("program", formHostSource(sliceArrayPrograms["slice-array-pointers"]))))
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range checked.Functions {
		if fn.Object.Name() == "view" && len(fn.HeapVars) != 1 {
			t.Fatalf("view: %d heap moves, want 1", len(fn.HeapVars))
		}
		if fn.Object.Name() == "run" && len(fn.HeapVars) != 0 {
			t.Fatalf("run: %d heap moves, want 0", len(fn.HeapVars))
		}
	}
	for _, g := range checked.Globals {
		if g.Var.Name() == "gbuf" && g.ReadOnly {
			t.Fatal("gbuf is sliced and written through gview; it must not be read-only")
		}
	}
	if stop := checked.InitImage.Stop; stop == nil || stop.Reason != "converts a slice to an array" {
		t.Fatalf("initialization stop %+v; want gview's conversion", stop)
	}
}

func TestSliceArrayRejections(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		// Go evaluates nothing for an index-only range over a converted
		// pointer without calls; the existing rule keeps rejecting it.
		{"for i := range (*[4]byte)(s) { s[i] = 0 }", "index-only range over an array conversion unsupported; range over the operand"},
	} {
		_, err := Check(globalsProgram(unitFiles("program", "package main\n\nfunc Process(s, in []byte) uint64 {\n\t"+tc.body+"\n\treturn 0\n}\n")))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.body, err, tc.want)
		}
	}
}

func TestSliceArraySBF(t *testing.T) {
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	inputs := sliceArrayInputs(96)
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			for name, src := range sliceArrayPrograms {
				want, panicLines := sliceArrayNative(t, src, inputs)
				var cases []allocCase
				aborts, logged := 0, 0
				for j, in := range inputs {
					c := allocCase{name: fmt.Sprintf("%s-%02d", name, j), in: in, err: abortError, log: abortLog}
					if strings.Contains(want[j], "P:") {
						// A trap logs no panic line; a panic call logs its
						// own (the order of evaluation decides which).
						aborts++
						if panicLines[j] != "" {
							logged++
							c.panicLog = "Program log: " + panicLines[j]
						} else {
							c.noPanicLog = true
						}
					} else {
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
				t.Logf("%s: %d cases (%d aborts with rollback, %d logging a panic line) match native Go", name, len(cases), aborts, logged)
			}
		})
	}
}
