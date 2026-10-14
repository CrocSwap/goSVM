package compiler

import (
	"bytes"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"math/bits"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The curated math/bits declarations must be exactly the standard library's
// exported API: the same names, the same signatures (including parameter and
// result names) and the same constant values.
func TestBitsDeclarationsMatchStandardLibrary(t *testing.T) {
	std, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(bitsPath)
	if err != nil {
		t.Skipf("standard library source unavailable: %v", err)
	}
	curated, err := importBits()
	if err != nil {
		t.Fatal(err)
	}
	describe := func(pkg *types.Package) map[string]string {
		out := map[string]string{}
		for _, name := range pkg.Scope().Names() {
			o := pkg.Scope().Lookup(name)
			if !o.Exported() {
				continue
			}
			s := types.ObjectString(o, types.RelativeTo(pkg))
			if c, ok := o.(*types.Const); ok {
				s += " = " + c.Val().ExactString()
			}
			out[name] = s
		}
		return out
	}
	want, got := describe(std), describe(curated)
	var names []string
	for name := range want {
		names = append(names, name)
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if want[name] != got[name] {
			t.Errorf("%s: standard library %q, curated %q", name, want[name], got[name])
		}
	}
	if len(want) < 50 {
		t.Fatalf("standard library math/bits has only %d exported names", len(want))
	}
	for name := range bitsFunctions {
		if _, ok := curated.Scope().Lookup(name).(*types.Func); !ok {
			t.Errorf("supported function %s is not declared", name)
		}
		if !strings.Contains(bitsRuntime+bits16Runtime, "gosvm_bits_"+name+"(") {
			t.Errorf("supported function %s has no runtime helper", name)
		}
	}
}

const bitsAllSource = `package main

import (
	"math/bits"
	mb "math/bits"
)

type Pair struct{ Hi, Lo uint64 }

func three(a, b, c uint64) (uint64, uint64, uint64) { return a, b, c }

func product(x, y uint64) (uint64, uint64) { return bits.Mul64(x, y) }

func Process(s, i []byte) uint64 {
	x, y := uint64(i[0])<<56|uint64(i[1]), uint64(i[2])+1
	var p Pair
	p.Hi, p.Lo = bits.Mul64(x, y)
	q, r := bits.Div64(p.Hi%y, p.Lo, y)
	sum, carry := bits.Add64(three(x, y, 1))
	diff, borrow := bits.Sub64(x, y, carry)
	h, l := product(q, r)
	a32, c32 := bits.Add32(uint32(x), uint32(y), 1)
	d32, b32 := bits.Sub32(uint32(x), uint32(y), 0)
	m32, n32 := bits.Mul32(uint32(x), uint32(y))
	q32, r32 := bits.Div32(0, uint32(x), uint32(y))
	total := sum + diff + borrow + h + l + uint64(a32+c32+d32+b32+m32+n32+q32+r32)
	total = total + bits.Rem64(x, y, y) + uint64(bits.Rem32(uint32(x), 1, uint32(y)))
	total = total + uint64(bits.LeadingZeros64(x)) + uint64(bits.LeadingZeros32(uint32(x))) + uint64(bits.LeadingZeros8(i[0]))
	total = total + uint64(bits.TrailingZeros64(x)) + uint64(bits.TrailingZeros32(uint32(x))) + uint64(bits.TrailingZeros8(i[0]))
	total = total + uint64(bits.OnesCount64(x)) + uint64(bits.OnesCount32(uint32(x))) + uint64(bits.OnesCount8(i[0]))
	total = total + uint64(mb.Len64(x)) + uint64(mb.Len32(uint32(x))) + uint64(mb.Len8(i[0]))
	total = total + bits.Reverse64(x) + uint64(bits.Reverse32(uint32(x))) + uint64(bits.Reverse8(i[0]))
	total = total + bits.ReverseBytes64(x) + uint64(bits.ReverseBytes32(uint32(x)))
	const k = -3
	total = total + bits.RotateLeft64(x, k) + uint64(bits.RotateLeft32(uint32(x), 1<<40+5)) + uint64(bits.RotateLeft8(i[0], -(k*k)))
	if bits.Len64(total) > 32 && bits.OnesCount8(i[1]) != 3 {
		total = total << bits.TrailingZeros8(i[1])
	}
	switch bits.LeadingZeros64(total) {
	case 0, 64:
		total = total + bits.UintSize
	}
	return total + uint64(len(s))
}
`

func TestBitsCheckAcceptsSupportedFunctions(t *testing.T) {
	checked := checkSource(t, bitsAllSource)
	if len(checked.Functions) != 3 || len(checked.Functions[2].Callees) != 2 {
		t.Fatalf("math/bits calls must not be program callees: %+v", checked.Functions)
	}
	c, err := CompileSources([]Source{{"program.go", []byte(bitsAllSource)}})
	if err != nil {
		t.Fatal(err)
	}
	// One shared result aggregate per distinct tuple type, program or math/bits.
	if n := bytes.Count(c, []byte("typedef struct {\nu64 r0;\nu64 r1;\n}")); n != 1 {
		t.Fatalf("(uint64, uint64) results declared %d times", n)
	}
	if !bytes.Contains(c, []byte("gosvm_bits_RotateLeft64(")) || !bytes.Contains(c, []byte(",61ULL)")) {
		t.Fatal("RotateLeft64(x, -3) did not fold to a left rotation by 61")
	}
	hostGenerated(t, c, "int main(void){u8 s[1]={0},i[2]={1,2};return go_Process((slice){s,1},(slice){i,2})==0;}")
}

func TestBitsCheckRejections(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"uint Add":          {`sum, carry := bits.Add(1, 2, 0); return uint64(sum + carry)`, "math/bits.Add is unsupported: uint is not in the on-chain subset"},
		"uint Mul":          {`h, l := bits.Mul(1, 2); return uint64(h ^ l)`, "math/bits.Mul is unsupported: uint is not in the on-chain subset"},
		"uint LeadingZeros": {`return uint64(bits.LeadingZeros(1))`, "math/bits.LeadingZeros is unsupported: uint"},
		"uint Rem":          {`return uint64(bits.Rem(1, 2, 3))`, "math/bits.Rem is unsupported: uint"},
		"variable k16":      {`return uint64(bits.RotateLeft16(uint16(i[0]), len(i)))`, "math/bits.RotateLeft16 requires a constant rotation count"},
		"variable k":        {`return bits.RotateLeft64(1, len(i))`, "math/bits.RotateLeft64 requires a constant rotation count"},
		"count result k":    {`return uint64(bits.RotateLeft8(i[0], bits.Len64(7)))`, "math/bits.RotateLeft8 requires a constant rotation count"},
		"int variable":      {`n := bits.Len64(7); return uint64(n)`, "int variables are unsupported"},
		"int var decl":      {`var n = bits.OnesCount8(i[0]); return uint64(n)`, "int variables are unsupported"},
		"signed arithmetic": {`return uint64(bits.Len64(7) - 1)`, "signed arithmetic unsupported"},
		"function value":    {`f := bits.Len64; return uint64(f(1))`, "only value struct fields supported"},
		"uint UintSize":     {`var u uint = bits.UintSize; return uint64(u)`, "unsupported type uint"},
	} {
		t.Run(name, func(t *testing.T) {
			source := "package main\nimport \"math/bits\"\nfunc Process(s, i []byte) uint64 {\n" + tc.body + "\n}\n"
			if _, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(source)}}}}}); err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "p.go:") {
				t.Fatalf("want %q at source, got %v", tc.want, err)
			}
		})
	}
	for name, tc := range map[string]struct{ source, want string }{
		"int result": {"package main\nimport \"math/bits\"\nfunc n(x uint64) int { return bits.Len64(x) }\nfunc Process(s, i []byte) uint64 { return uint64(n(1)) }\n", "int variables are unsupported"},
		"dot import": {"package main\nimport . \"math/bits\"\nfunc Process(s, i []byte) uint64 { return Reverse64(1) }\n", "dot and blank imports unsupported"},
		"blank":      {"package main\nimport _ \"math/bits\"\nfunc Process(s, i []byte) uint64 { return 1 }\n", "dot and blank imports unsupported"},
		"other std":  {"package main\nimport \"math\"\nfunc Process(s, i []byte) uint64 { return math.MaxUint32 }\n", `unsupported import "math"`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(tc.source)}}}}}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

// A single file needs no module for math/bits, and module programs may use it
// from shared user packages.
func TestBitsImportForms(t *testing.T) {
	dir := t.TempDir()
	writePackageFile(t, dir, "single/program.go", "package main\nimport \"math/bits\"\nfunc Process(s, i []byte) uint64 { h, _ := bits.Mul64(uint64(len(i)), 1<<63); return h }\n")
	p, err := ReadProgram(filepath.Join(dir, "single", "program.go"))
	if err != nil || p.Entry != "program" || len(p.Metadata) != 0 {
		t.Fatalf("single-file program: %v %+v", err, p)
	}
	if _, err = CompileProgram(p); err != nil {
		t.Fatal(err)
	}
	writePackageFile(t, dir, "module/go.mod", "module example.com/app\n\ngo 1.22\n")
	writePackageFile(t, dir, "module/mathx/mathx.go", "package mathx\nimport wide \"math/bits\"\nfunc MulDiv(a, b, d uint64) uint64 {\n hi, lo := wide.Mul64(a, b)\n if d == 0 || hi >= d { return 0 }\n q, _ := wide.Div64(hi, lo, d)\n return q\n}\n")
	writePackageFile(t, dir, "module/program/program.go", "package main\nimport (\n\"math/bits\"\n\"example.com/app/mathx\"\n)\nfunc Process(s, i []byte) uint64 { return mathx.MulDiv(1<<40, 1<<40, 1<<20+1) + uint64(bits.Len8(i[0])) }\n")
	p, err = ReadProgram(filepath.Join(dir, "module", "program"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Entry != "example.com/app/program" || len(p.Packages) != 2 {
		t.Fatalf("module graph: %s %d", p.Entry, len(p.Packages))
	}
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	hi, lo := bits.Mul64(1<<40, 1<<40)
	q, _ := bits.Div64(hi, lo, 1<<20+1)
	bin := hostGenerated(t, c, fmt.Sprintf("int main(void){u8 i[1]={5};return go_Process((slice){0,0},(slice){i,1})!=%dULL;}", q+3))
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("module math/bits program: %v %s", err, out)
	}
}

// Programs that do not call math/bits emit exactly the C they always have
// (the frozen corpus checks hashes); the runtime appears only when used.
func TestBitsRuntimeOnlyWhenUsed(t *testing.T) {
	plain := "package main\nfunc Process(s, i []byte) uint64 { return uint64(len(s)) }\n"
	c, err := CompileSources([]Source{{"p.go", []byte(plain)}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(c, []byte("gosvm_bits")) {
		t.Fatal("math/bits runtime emitted for a program that does not use it")
	}
	constantOnly := "package main\nimport \"math/bits\"\nfunc Process(s, i []byte) uint64 { return bits.UintSize }\n"
	c, err = CompileSources([]Source{{"p.go", []byte(constantOnly)}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(c, []byte("gosvm_bits")) {
		t.Fatal("math/bits runtime emitted for a constant-only use")
	}
}
