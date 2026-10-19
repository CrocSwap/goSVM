package compiler

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"

	rangevalues "gosvm/examples/range-values"
	uint16values "gosvm/examples/uint16-values"
)

// checkMessage compiles one file and returns the diagnostic without its
// file:line:col prefix, or "" when the program is accepted.
func checkMessage(t *testing.T, source string) string {
	t.Helper()
	_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "range.go", Data: []byte("package p\n" + source)}}}}})
	if err == nil {
		return ""
	}
	message := err.Error()
	if !strings.HasPrefix(message, "range.go:") {
		t.Fatalf("diagnostic is not source-located: %q", message)
	}
	parts := strings.SplitN(message, ": ", 2)
	return parts[1]
}

func TestRangeAcceptedForms(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"slice key value":      `for i, v := range s { s[i] = v }`,
		"slice key":            `for i := range s { s[i] = 0 }`,
		"slice value":          `for _, v := range s { s[0] = v }`,
		"slice none":           `for range s {}`,
		"slice view":           `for i := range s[1:3] { s[i] = 1 }`,
		"array key value":      `var a [4]uint32; for i, v := range a { a[i] = v + 1 }`,
		"array blank blank":    `var a [4]bool; for _, _ = range a {}`,
		"array pointer":        `var a [4]uint64; p := &a; for i, v := range p { p[i] = v }`,
		"array pointer key":    `var p *[4]byte; for i := range p { s[i] = 0 }`,
		"array deref key":      `var p *[4]byte; for range *p {}`,
		"array call key":       `for i := range k() { s[i] = 0 }`,
		"array call value":     `for _, v := range k() { s[0] = v }`,
		"array conversion val": `var a [4]byte; for _, v := range K(a) { s[0] = v }`,
		"array literal value":  `for _, v := range [2]byte{1, 2} { s[0] = v }`,
		"field array":          `var b B; for i, v := range b.K { b.K[i] = v }`,
		"uint64 count":         `for i := range uint64(len(s)) { s[i] = byte(i) }`,
		"uint8 count":          `for i := range s[0] { s[i] = i }`,
		"uint16 count":         `var n uint16 = 3; for i := range n { s[0] = byte(i) }`,
		"uint32 count none":    `for range uint32(3) {}`,
		"untyped count":        `for i := range 3 { s[i] = 1 }`,
		"len count":            `for i := range len(s) { s[i] = 1 }`,
		"assign count":         `var x uint16; for x = range uint16(3) {}; s[0] = byte(x)`,
		"assign value":         `var v byte; for _, v = range s {}; s[0] = v`,
		"assign blank key":     `var v byte; for _, v = range [2]byte{} {}; s[0] = v`,
		"int key uses":         `var a [4]byte; for i := range a { if i < len(a) && i != 2 { a[i] = s[i] >> i }; s[uint64(i)+1] = byte(i); i = 0 }`,
		"break continue":       `for i := range s { if i == 1 { continue }; if i == 2 { break } }`,
		"local address":        `for _, v := range s { p := &v; *p = 1 }`,
		"word slice":           `for range []uint64{1} {}`,
		"value escapes":        `var p *byte; for _, v := range s { p = &v }; _ = p`,
		"key escapes":          `var p *uint8; for i := range uint8(3) { p = &i }; _ = p`,
		"nested":               `for i := range s { for j := range uint8(2) { s[i] = j } }`,
		// The int key is an ordinary int value.
		"signed count":       `for range int64(3) {}; for j := range int8(-3) { s[0] = byte(j) }`,
		"int key arithmetic": `for i := range s { if i+1 < len(s) { s[i+1] = 0 } }`,
		"int key variable":   `for i := range s { j := i; s[j] = 0 }`,
		"int key increment":  `for i := range s { i++; _ = i }`,
		"int key address":    `for i := range s { p := &i; *p = *p - 1 }`,
		"int key shift":      `for i := range s { s[0] = byte(i << 1 >> 1) }`,
		"int key negation":   `for i := range s { s[0] = byte(-i) }`,
		"int key complement": `for i := range s { s[0] = byte(^i) }`,
		"len complement":     `s[0] = byte(^len(s))`,
		"int assign key":     `var x int; for x = range 3 {}; _ = x`,
	} {
		t.Run(name, func(t *testing.T) {
			source := "type K [4]byte\ntype B struct{ K K }\nfunc k() K { return K{} }\nfunc Process(s, i []byte) uint64 {\n" + body + "\nreturn 0 }\n"
			if got := checkMessage(t, source); got != "" {
				t.Fatalf("rejected: %s", got)
			}
			if _, err := Compile("range.go", []byte("package p\n"+source)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRangeRejections(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ body, want string }{
		"map":              {`for range map[uint64]uint64{} {}`, rangeMapRule},
		"channel":          {`for range make(chan uint64) {}`, "range over channels unsupported"},
		"field target":     {`var b B; for b.N = range uint64(3) {}`, "range assignment targets must be local variables"},
		"index target":     {`for s[0] = range uint8(3) {}`, "range assignment targets must be local variables"},
		"conversion key":   {`var a [4]byte; for range K(a) {}`, "index-only range over an array conversion unsupported; range over the operand"},
		"literal key":      {`for i := range [2]byte{1, 2} { s[i] = 0 }`, "index-only range over an array requires a variable, field, dereference or call"},
		"labeled":          {`L: for range s { break L }`, "unsupported statement *ast.LabeledStmt"},
		"labeled continue": {`L: for range s { continue L }`, "unsupported statement *ast.LabeledStmt"},
	} {
		t.Run(name, func(t *testing.T) {
			source := "type K [4]byte\ntype B struct{ N uint64 }\nfunc Process(s, i []byte) uint64 {\n" + c.body + "\nreturn 0 }\n"
			if got := checkMessage(t, source); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if _, err := Compile("range.go", []byte("package p\n"+source)); err == nil || !strings.HasSuffix(err.Error(), c.want) {
				t.Fatalf("CompileProgram disagrees with Check: %v", err)
			}
		})
	}
	// Range-over-func is not part of Go 1.22 without GOEXPERIMENT; it never
	// reaches the subset rules.
	if got := checkMessage(t, "func Process(s, i []byte) uint64 {\nfor range func(func() bool) {} {}\nreturn 0 }\n"); got == "" {
		t.Fatal("range over a function was accepted")
	}
}

func TestUint16Checks(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ source, want string }{
		"scalars":         {`type U uint16;type S struct{ A uint16; B U; C [2]uint16 };func f(a uint16, p *uint16) (uint16, U) { *p = a; return a << 3, U(a) };func Process(s, i []byte) uint64 { var x S; x.C[1]++; a, b := f(x.A, &x.A); return uint64(a) + uint64(b) }`, ""},
		"constant":        {`const C uint16 = 65535;func Process(s, i []byte) uint64 { return uint64(C) }`, ""},
		"array boundary":  {`type K [512]uint16;func Process(s, i []byte) uint64 { var k K; return uint64(len(k)) }`, ""},
		"array limit":     {`type K [513]uint16;func Process(s, i []byte) uint64 { return 0 }`, "array storage exceeds experimental 1024-byte per-value limit"},
		"slice":           {`func Process(s, i []byte) uint64 { var w []uint16; _ = w; return 0 }`, ""},
		"view":            {`func Process(s, i []byte) uint64 { var k [4]uint16; w := k[:]; return uint64(w[0]) }`, ""},
		"element message": {`type K [2]float32;func Process(s, i []byte) uint64 { return 0 }`, "unsupported type float32"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkMessage(t, c.source); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The uint16 typedef is emitted only for programs that use uint16, so the
// C of every other program stays byte-identical.
func TestUint16TypedefOnlyWhenUsed(t *testing.T) {
	t.Parallel()
	without, err := Compile("a.go", []byte("package p\nfunc Process(s, i []byte) uint64 { return uint64(s[0]) }\n"))
	if err != nil {
		t.Fatal(err)
	}
	with, err := Compile("a.go", []byte("package p\nfunc Process(s, i []byte) uint64 { return uint64(uint16(s[0])) }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "u16") || !strings.Contains(string(with), uint16Prelude) {
		t.Fatal("u16 typedef must appear exactly when uint16 is used")
	}
}

type oracleVector struct{ state, instruction []byte }

// differential runs vectors natively (ordinary Go) and through the generated
// C on the host, comparing the status and final state, or the panic and the
// state at the panic.
func differential(t *testing.T, path string, process func([]byte, []byte) uint64, vectors []oracleVector) {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostProgram(t, string(source), arrayHostDriver)
	var want []string
	var got strings.Builder
	var batch strings.Builder
	flush := func() {
		if batch.Len() > 0 {
			got.WriteString(runHost(t, bin, batch.String()))
			batch.Reset()
		}
	}
	for _, v := range vectors {
		line := fmt.Sprintf("%x %x\n", v.instruction, v.state)
		result := nativeArrayCall(process, append([]byte(nil), v.state...), v.instruction)
		want = append(want, result)
		if strings.HasPrefix(result, "P:") {
			// An abort ends the host process: run it on its own.
			flush()
			got.WriteString(runHost(t, bin, line))
			continue
		}
		batch.WriteString(line)
	}
	flush()
	panics := 0
	for _, w := range want {
		if strings.HasPrefix(w, "P:") {
			panics++
		}
	}
	if panics == 0 || panics == len(want) {
		t.Fatalf("vectors must include both panics and completions (%d of %d panic)", panics, len(want))
	}
	gotLines := strings.SplitAfter(got.String(), "\n")
	for j := range want {
		if j >= len(gotLines) || gotLines[j] != want[j] {
			g := ""
			if j < len(gotLines) {
				g = gotLines[j]
			}
			t.Fatalf("vector %d (%x %x): C %q, Go %q", j, vectors[j].instruction, vectors[j].state, g, want[j])
		}
	}
}

func runHost(t *testing.T, bin, input string) string {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("host run: %v %s", err, out)
	}
	return string(out)
}

func rangeVectors() []oracleVector {
	rng := rand.New(rand.NewSource(20261014))
	var vectors []oracleVector
	for j := 0; j < 400; j++ {
		state, instruction := make([]byte, 24), make([]byte, 16)
		rng.Read(state)
		rng.Read(instruction)
		instruction[0] = byte(j % 20)
		if j%3 == 0 {
			instruction[1] = byte(rng.Intn(32))
		}
		vectors = append(vectors, oracleVector{state, instruction})
	}
	// Edge vectors: zero counts and empty views, full-length views, counts
	// at and past the state length, and both nil/non-nil pointer ranges.
	for mode := byte(0); mode < 20; mode++ {
		for _, n := range []byte{0, 1, 7, 8, 23, 24, 29, 30, 255} {
			for _, x := range []byte{0, 4, 255} {
				state, instruction := make([]byte, 24), make([]byte, 16)
				for k := range state {
					state[k] = byte(k*37 + int(x))
				}
				instruction[0], instruction[1], instruction[2], instruction[3], instruction[4] = mode, n, x, x, n
				vectors = append(vectors, oracleVector{state, instruction})
			}
		}
	}
	return vectors
}

func uint16Vectors() []oracleVector {
	rng := rand.New(rand.NewSource(65535))
	var vectors []oracleVector
	add := func(mode byte, a, b uint16, c byte) {
		state, instruction := make([]byte, 24), make([]byte, 16)
		rng.Read(state)
		instruction[0] = mode
		binary.LittleEndian.PutUint16(instruction[1:], a)
		binary.LittleEndian.PutUint16(instruction[3:], b)
		instruction[5] = c
		vectors = append(vectors, oracleVector{state, instruction})
	}
	for j := 0; j < 600; j++ {
		add(byte(j%6), uint16(rng.Uint32()), uint16(rng.Uint32()), byte(rng.Intn(40)))
	}
	edges := []uint16{0, 1, 2, 255, 256, 0x7fff, 0x8000, 0xfff0, 65000, 65534, 65535}
	for mode := byte(0); mode < 6; mode++ {
		for _, a := range edges {
			for _, b := range edges {
				for _, c := range []byte{0, 1, 15, 16, 17, 255} {
					add(mode, a, b, c)
				}
			}
		}
	}
	return vectors
}

func TestRangeDifferential(t *testing.T) {
	t.Parallel()
	differential(t, "../../examples/range-values/program.go", rangevalues.Process, rangeVectors())
}

func TestUint16Differential(t *testing.T) {
	t.Parallel()
	differential(t, "../../examples/uint16-values/program.go", uint16values.Process, uint16Vectors())
}

// Exports for the external SBF test (range_sbf_test.go).
var ModernLLVMForTest = modernLLVM

func RangeVectorsForTest() [][2][]byte  { return exportVectors(rangeVectors()) }
func Uint16VectorsForTest() [][2][]byte { return exportVectors(uint16Vectors()) }
func exportVectors(vectors []oracleVector) [][2][]byte {
	out := make([][2][]byte, len(vectors))
	for j, v := range vectors {
		out[j] = [2][]byte{v.state, v.instruction}
	}
	return out
}
