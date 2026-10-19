package compiler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	byvalvalues "gosvm/examples/byval-values"
	"gosvm/internal/targets"
	"gosvm/internal/testvm"
)

// Aggregate parameters that callees write (byval.go): examples/byval-values
// keeps strings, slices, structs and arrays in memory (package variables,
// heap fields, address-taken locals, boxed interface values, deferred
// arguments, slice, array and map elements) and passes them to callees that
// write their parameter and pass it on. Native Go is the oracle; the host C
// was always right, SBF differed before every written aggregate parameter
// was copied at entry.

func byvalSource(t *testing.T) []byte { return mustRead(t, "../../examples/byval-values/program.go") }

// byvalVectors gives every case perCase random vectors.
func byvalVectors(perCase int) [][]byte {
	rng := rand.New(rand.NewSource(20261015))
	var vectors [][]byte
	for k := 0; k < byvalvalues.Cases; k++ {
		for j := 0; j < perCase; j++ {
			in := make([]byte, 128)
			rng.Read(in)
			in[0] = byte(k)
			vectors = append(vectors, in)
		}
	}
	return vectors
}

func nativeByval(in []byte) []byte {
	out := make([]byte, 1024)
	byvalvalues.Run(out, append([]byte(nil), in...))
	return out
}

func TestByvalDifferential(t *testing.T) {
	t.Parallel()
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: byvalSource(t)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, valueTypesDriver, false)
	vectors := byvalVectors(16)
	var input strings.Builder
	var want []string
	for _, in := range vectors {
		fmt.Fprintf(&input, "%x\n", in)
		want = append(want, fmt.Sprintf("%x\n", nativeByval(in)))
	}
	got := strings.SplitAfter(runHost(t, bin, input.String()), "\n")
	for j := range want {
		line := ""
		if j < len(got) {
			line = got[j]
			if i := strings.IndexByte(line, ':'); i >= 0 {
				line = line[i+1:]
			}
		}
		if line != want[j] {
			t.Fatalf("vector %d (case %d):\nC  %.200q\nGo %.200q", j, vectors[j][0], line, want[j])
		}
	}
	t.Logf("%d vectors over %d cases match native Go", len(vectors), byvalvalues.Cases)
}

// TestByvalSBF runs examples/byval-values on fogo-v0 and solana-v3: every
// case must leave native Go's output. GOSVM_INT_RESULTS=<dir> keeps the
// fixtures, ELFs and reports under byval/.
func TestByvalSBF(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("SBF runs are skipped in -short mode")
	}
	llvm := modernLLVM()
	if llvm == "" {
		t.Skip("pinned platform-tools v1.57 backend required")
	}
	var cases []allocCase
	for j, in := range byvalVectors(4) {
		cases = append(cases, allocCase{name: fmt.Sprintf("byval-%03d-case%02d", j, in[0]), in: in, units: comboUnits, output: nativeByval(in)})
	}
	source := comboSBFSource(t, byvalSource(t), "byvalvalues")
	keep := os.Getenv("GOSVM_INT_RESULTS")
	if keep != "" {
		keep = filepath.Join(keep, "byval")
	}
	for _, profile := range []string{target.Fogo, target.Solana} {
		t.Run(profile, func(t *testing.T) {
			runner, err := testvm.RunnerFor("", profile)
			if err != nil {
				t.Skipf("runner unavailable: %v", err)
			}
			result := runAllocProgram(t, llvm, runner, profile, "byval-values", source, 0, cases, keep)
			summary := map[string]any{"profile": profile, "elf_sha256": result["elf_sha256"], "cases": len(cases)}
			encoded, _ := json.MarshalIndent(summary, "", "  ")
			t.Logf("%s: %d cases match native Go\n%s", profile, len(cases), encoded)
		})
	}
}

// Which parameters are copied at entry: written ones plainly, ones read
// after a write that may change the caller's variable opaquely, and no
// others, so programs without such parameters keep their C.
func TestByvalCopies(t *testing.T) {
	t.Parallel()
	const prefix = `package p
type P struct{ a, b uint64 }
type K struct{ key [4]byte; n uint64 }
type S struct{ buf [8]byte; n uint64 }
func (s *S) add(b []byte) { for j := range b { s.buf[s.n%8] = b[j]; s.n++ } }
var gp P
var gk K
func use(p P) uint64 { return p.a + p.b }
func useK(k K) uint64 { return uint64(k.key[0]) + k.n }
`
	for _, tc := range []struct {
		name, src string
		plain     int // parameters copied plainly
		opaque    int // parameters copied opaquely
	}{
		{"read only", `func f(p P, s string) uint64 { return use(p) + uint64(len(s)) }`, 0, 0},
		{"written field", `func f(p P) uint64 { p.a++; return use(p) }`, 1, 0},
		{"written string", `func f(s string) uint64 { n := uint64(0); for len(s) > 0 { s = s[1:]; n++ }; return n }`, 1, 0},
		{"written array", `func f(a [4]uint64) uint64 { a[1] = 3; return a[0] + a[1] }`, 1, 0},
		{"value receiver", `func (p P) f() uint64 { p.b = 2; return use(p) }`, 1, 0},
		{"read before write", `func f(p P) uint64 { r := use(p); gp.a = 1; return r }`, 0, 0},
		{"read after global write", `func f(p P) uint64 { gp.a = 1; return use(p) }`, 0, 1},
		{"read after pointer write", `func f(p P, q *P) uint64 { q.b = 1; return use(p) }`, 0, 1},
		{"write in loop", `func f(p P, q *P) uint64 { r := uint64(0); for j := 0; j < 2; j++ { r += use(p); q.a = r }; return r }`, 0, 1},
		{"other type written", `func f(s []byte, q *P) uint64 { q.a = 1; return uint64(len(s)) }`, 0, 0},
		{"bytes into a byte array holder", `func f(k K, b []byte) uint64 { b[0] = 1; return useK(k) }`, 0, 1},
		{"bytes into a slice header", `func f(s []byte, b []byte) uint64 { b[0] = 1; return uint64(len(s)) }`, 0, 0},
		{"local receiver", `func f(k K, b []byte) uint64 { var s S; s.add(b); return useK(k) + s.n }`, 0, 0},
		{"param receiver", `func f(k K, s *S, b []byte) uint64 { s.add(b); return useK(k) }`, 0, 1},
		{"written and aliased", `func f(p P) uint64 { gp.a = 1; p.b = 2; return use(p) }`, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := prefix + tc.src + "\nfunc Process(s, i []byte) uint64 { return 0 }\n"
			c, err := CompileProgram(&Program{Entry: "p", Packages: []PackageSources{{"p", []Source{{Name: "p.go", Data: []byte(src)}}}}})
			if err != nil {
				t.Fatal(err)
			}
			text := string(c)
			opaque := strings.Count(text, "*gosvm_p = &")
			plain := strings.Count(text, "_in;") - opaque
			if plain != tc.plain || opaque != tc.opaque {
				t.Fatalf("%d plain and %d opaque copies, want %d and %d", plain, opaque, tc.plain, tc.opaque)
			}
		})
	}
}
