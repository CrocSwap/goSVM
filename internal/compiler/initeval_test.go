package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// Compile-time evaluation of package initialization
// (docs/GO_GLOBALS.md#compile-time-evaluation): which steps the interpreter
// evaluates, why it stops, and that a program behaves identically with its
// image and with every initializer at run time.

// initEvalCase is a program, the steps the interpreter must evaluate of
// the total, and a substring of the stop reason ("" when every step is
// evaluated).
type initEvalCase struct {
	src         string
	steps, of   int
	stop        string
	in          []byte
	wantPanic   bool
	wantResult  string
	extraSource string // a second package, imported as "lib"
}

var initEvalCases = map[string]initEvalCase{
	"every step": {src: `var a = triple(5)
var b = a * 2
func triple(x uint64) uint64 { return 3 * x }
func init() { b++ }
func Process(s, i []byte) uint64 { return a*100 + b }`, steps: 3, of: 3, wantResult: "1531"},
	"panic in an initializer": {src: `var a = uint64(7)
var b = check(a)
var c = uint64(3)
func check(x uint64) uint64 { if x > 5 { panic("too big") }; return x }
func Process(s, i []byte) uint64 { return a + b + c }`, steps: 0, of: 1, stop: "panic", wantPanic: true},
	"division by zero after an evaluated step": {src: `var zero = make([]uint64, 1)[0]
var bad = 10 / zero
func Process(s, i []byte) uint64 { return bad }`, steps: 1, of: 2, stop: "integer divide by zero", wantPanic: true},
	"index out of range in an init function": {src: `var t = []uint8{1}
func init() { t[1] = 2 }
func Process(s, i []byte) uint64 { return uint64(t[0]) }`, steps: 1, of: 2, stop: "index out of range", wantPanic: true},
	"nil map write": {src: `var m map[uint8]uint8
var n = uint64(len("abc"))
func init() { m[1] = 1 }
func Process(s, i []byte) uint64 { return n }`, steps: 0, of: 1, stop: "assignment to entry in nil map", wantPanic: true},
	"nil pointer dereference": {src: `type T struct{ v uint64 }
var p *T
var q = new(T)
var v = p.v
func Process(s, i []byte) uint64 { return v + q.v }`, steps: 1, of: 2, stop: "nil pointer dereference", wantPanic: true},
	"failed type assertion": {src: `var x any = uint8(1)
var y = x.(uint16)
func Process(s, i []byte) uint64 { return uint64(y) }`, steps: 1, of: 2, stop: "failed type assertion", wantPanic: true},
	"heap budget": {src: `var small = make([]byte, 16)
var big = make([]byte, 40000)
func Process(s, i []byte) uint64 { return uint64(len(small) + len(big)) }`, steps: 1, of: 2, stop: "allocation exceeds the heap budget", wantPanic: true},
	"step budget": {src: `var n = spin()
var m = n + 1
func spin() uint64 { r := uint64(0); for i := 0; i < 1100000; i++ { r += uint64(i) & 3 }; return r }
func Process(s, i []byte) uint64 { return m }`, steps: 0, of: 2, stop: "step compile-time evaluation budget", wantResult: "1650001"},
	// Every step after the stop runs at run time, even one that does not
	// depend on it: the evaluated part is always a prefix.
	"pointer-keyed map stops the prefix": {src: `type T struct{ n uint64 }
var first = &T{1}
var byPtr = map[*T]uint64{first: 2}
var later = triple(4)
func triple(x uint64) uint64 { return 3 * x }
func init() { first.n = 10 }
func Process(s, i []byte) uint64 { return first.n*1000 + byPtr[first]*100 + later }`, steps: 1, of: 4, stop: "maps keyed by *program.T", wantResult: "10212"},
	"interface-keyed map": {src: `var m = map[any]uint64{uint8(1): 2}
func Process(s, i []byte) uint64 { return m[uint8(1)] }`, steps: 0, of: 1, stop: "maps keyed by any", wantResult: "2"},
	// The image composes with run-time steps of later packages that change
	// evaluated variables.
	"later package at run time": {src: `import "lib"
var seen = map[*uint64]bool{lib.P: true}
func init() { *lib.P += 5; lib.Log = append(lib.Log, 9) }
func Process(s, i []byte) uint64 { return *lib.P*100 + uint64(len(lib.Log))*10 + uint64(lib.Log[2]) }`,
		extraSource: `package lib
var P = new(uint64)
var Log = []uint64{1}
func init() { *P = 3; Log = append(Log, 2) }`, steps: 3, of: 5, stop: "maps keyed by *uint64", wantResult: "839"},
}

func initEvalProgram(c initEvalCase) *Program {
	units := []PackageSources{unitFiles("program", "package p\n"+c.src)}
	if c.extraSource != "" {
		units = append(units, unitFiles("lib", c.extraSource))
	}
	return globalsProgram(units...)
}

func TestInitEvalStops(t *testing.T) {
	t.Parallel()
	for name, c := range initEvalCases {
		t.Run(name, func(t *testing.T) {
			checked, err := Check(initEvalProgram(c))
			if err != nil {
				t.Fatal(err)
			}
			img := checked.InitImage
			steps := 0
			if img != nil {
				steps = img.Steps
			}
			if steps != c.steps || len(checked.InitSteps()) != c.of {
				t.Fatalf("evaluated %d of %d steps, want %d of %d (%+v)", steps, len(checked.InitSteps()), c.steps, c.of, img)
			}
			if c.stop == "" && img.Stop != nil {
				t.Fatalf("stopped: %s", img.Stop.Reason)
			}
			if c.stop != "" && img != nil && (img.Stop == nil || !strings.Contains(img.Stop.Reason, c.stop)) {
				t.Fatalf("stop %+v, want %q", img.Stop, c.stop)
			}
			if img != nil {
				for _, pi := range checked.Inits {
					if pi.EvaluatedVars < 0 || pi.Vars != nil && pi.EvaluatedVars > len(pi.Vars.Decl.Body.List) || pi.EvaluatedFuncs > len(pi.Funcs) {
						t.Fatalf("package %s: %d vars, %d funcs evaluated", pi.Package.Path(), pi.EvaluatedVars, pi.EvaluatedFuncs)
					}
				}
			}
			// The same output with the image and with every initializer at
			// run time.
			var outputs []string
			for _, runtimeInit := range []bool{false, true} {
				p := initEvalProgram(c)
				p.RuntimeInit = runtimeInit
				outputs = append(outputs, runInitHost(t, p, c.in))
			}
			if outputs[0] != outputs[1] {
				t.Fatalf("image %.40q, run time %.40q", outputs[0], outputs[1])
			}
			if c.wantPanic != strings.HasPrefix(outputs[0], "P:") {
				t.Fatalf("output %.40q, panic %v", outputs[0], c.wantPanic)
			}
			if c.wantResult != "" && resultOf(t, outputs[0]) != c.wantResult {
				t.Fatalf("result %s, want %s", resultOf(t, outputs[0]), c.wantResult)
			}
		})
	}
}

// The image describes the state backend-neutrally: two variables holding
// one object refer to one InitObject, a slice of a global array refers to
// the variable, strings built at initialization are heap bytes, and each
// PackageInit records the steps the image covers.
func TestInitImageCheckedProgram(t *testing.T) {
	t.Parallel()
	src := `package p
type T struct{ a, b uint64 }
var arr [4]uint16
var view = arr[1:3]
var x = &T{1, 2}
var y = x
var s = "ab" + itoa(x.b)
var m = map[string]uint64{"k": 7}
var e error = &Err{"e"}
var f = func() uint64 { return 1 }
type Err struct{ msg string }
func (e *Err) Error() string { return e.msg }
func itoa(n uint64) string { return string([]byte{byte('0' + n)}) }
func init() { arr[2] = 9 }
func Process(o, in []byte) uint64 { return x.a + y.b + uint64(view[1]) + uint64(len(s)) + m["k"] + f() }`
	checked, err := Check(globalsProgram(unitFiles("program", src)))
	if err != nil {
		t.Fatal(err)
	}
	img := checked.InitImage
	if img == nil || img.Steps != img.Total || img.Total != 8 {
		t.Fatalf("image %+v", img)
	}
	pi := checked.Inits[len(checked.Inits)-1]
	if pi.EvaluatedVars != len(pi.Vars.Decl.Body.List) || pi.EvaluatedFuncs != 1 {
		t.Fatalf("evaluated %d vars, %d funcs", pi.EvaluatedVars, pi.EvaluatedFuncs)
	}
	values := map[string]*InitValue{}
	for i, g := range checked.Globals {
		values[g.Var.Name()] = img.Values[i]
	}
	if values["x"].Ref == nil || values["x"].Ref.Object != values["y"].Ref.Object {
		t.Fatal("x and y must refer to one object")
	}
	if o := values["x"].Ref.Object; o.Kind != InitObjectValue || o.Value.Elems[1].Bits != 2 || o.Size != 16 {
		t.Fatalf("x's object %+v", o)
	}
	if v := values["view"]; v.Kind != InitSlice || v.Ref.Global == nil || v.Ref.Global.Var.Name() != "arr" || v.Low != 1 || v.Len != 2 || v.Cap != 3 {
		t.Fatalf("view %+v", v)
	}
	if v := values["arr"]; v.Elems[2].Bits != 9 {
		t.Fatalf("arr %+v", v)
	}
	if v := values["s"]; v.Kind != InitString || string(v.Bytes) != "ab2" || v.Ref == nil || v.Ref.Object.Kind != InitObjectBytes {
		t.Fatalf("s %+v", v)
	}
	if v := values["m"]; v.Ref == nil || v.Ref.Object.Kind != InitObjectMap || len(v.Ref.Object.Map.Entries) != 1 || v.Ref.Object.Map.Cap != 8 {
		t.Fatalf("m %+v", v)
	}
	if v := values["e"]; v.Kind != InitInterface || v.Dynamic == nil || !v.Dynamic.Direct || v.Elem.Ref.Object.Kind != InitObjectValue {
		t.Fatalf("e %+v", v)
	}
	if v := values["f"]; v.Kind != InitFuncValue || v.Target == nil || v.Target.Kind != TargetLiteral || v.Ref != nil {
		t.Fatalf("f %+v", v)
	}
	// Objects are listed once, by offset, after the block.
	last := int64(8 + checked.GlobalBytes)
	for _, o := range img.Objects {
		if o.Offset < last {
			t.Fatalf("object at %d before %d", o.Offset, last)
		}
		last = o.Offset + o.Size
	}
	if last > img.HeapUsed {
		t.Fatalf("objects end at %d, HeapUsed %d", last, img.HeapUsed)
	}
	// Nothing of the initialization runs at entry: the image is installed
	// and Process called.
	c, err := emitC(checked)
	if err != nil {
		t.Fatal(err)
	}
	init := string(c)[strings.Index(string(c), "static void gosvm_init(void) {"):]
	init = init[:strings.Index(init, "}")]
	if strings.Contains(init, "();") || !strings.Contains(init, "gosvm_image_copy") {
		t.Fatalf("gosvm_init:\n%s", init)
	}
}

// RuntimeInit disables compile-time evaluation: no image, and the C runs
// every initializer as before it existed.
func TestInitEvalRuntimeInit(t *testing.T) {
	t.Parallel()
	p := globalsProgram(unitFiles("program", `package p
var a = f()
func f() uint64 { return 4 }
func Process(s, i []byte) uint64 { return a }`))
	p.RuntimeInit = true
	checked, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	if checked.InitImage != nil || checked.Inits[0].EvaluatedVars != 0 {
		t.Fatalf("image %+v", checked.InitImage)
	}
	c, err := emitC(checked)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(c), "gosvm_image") || !strings.Contains(string(c), "(void)gosvm_alloc(8ULL)") {
		t.Fatal("RuntimeInit program emits an image")
	}
}

// A package whose initializer stops part-way runs the remaining statements
// of its synthetic initializer at run time, from the first one not
// evaluated.
func TestInitEvalPartialPackage(t *testing.T) {
	t.Parallel()
	src := `package p
type T struct{ n uint64 }
var a = &T{1}
var b = a.n + 1
var c = map[*T]uint64{a: 5}
var d = b * 10
func init() { a.n = 100 }
func Process(s, i []byte) uint64 { return a.n + b*1000 + c[a]*100000 + d*10000000 }`
	checked, err := Check(globalsProgram(unitFiles("program", src)))
	if err != nil {
		t.Fatal(err)
	}
	pi := checked.Inits[0]
	if pi.EvaluatedVars != 2 || pi.EvaluatedFuncs != 0 || len(pi.Vars.Decl.Body.List) != 4 {
		t.Fatalf("evaluated %d of %d vars, %d funcs", pi.EvaluatedVars, len(pi.Vars.Decl.Body.List), pi.EvaluatedFuncs)
	}
	got := runGlobalHost(t, globalsProgram(unitFiles("program", src)), nil)
	if resultOf(t, got) != "200502100" {
		t.Fatalf("result %s", resultOf(t, got))
	}
}

// runInitHost is runGlobalHost with a silent panic log.
func runInitHost(t *testing.T, p *Program, in []byte) string {
	t.Helper()
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, quietLogDriver, false)
	padded := make([]byte, 128)
	copy(padded, in)
	return runHost(t, bin, fmt.Sprintf("%x\n", padded))
}
