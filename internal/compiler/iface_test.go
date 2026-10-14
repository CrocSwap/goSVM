package compiler

import (
	"bytes"
	"fmt"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// Check-level tables for interface values (docs/GO_INTERFACES.md).

const ifacePrefix = `package p
type Shape interface{ Area() uint64 }
type Solid interface {
	Shape
	Scale(k uint64)
}
type R struct{ W, H uint64 }
func (r R) Area() uint64 { return r.W * r.H }
func (r *R) Scale(k uint64) { r.W = r.W * k }
type Two interface{ Two() (int8, bool) }
func (r R) Two() (int8, bool) { return -1, r.W > 0 }
type Box[T any] struct{ V T }
func (b Box[T]) Get() T { return b.V }
type Getter[T any] interface{ Get() T }
type E struct{}
type H struct {
	A any
	S Shape
}
func id(x any) any { return x }
func two() (R, bool) { return R{1, 2}, true }
func ret() (any, bool) { return two() }
func takes(a any, ok bool) bool { return ok && a != nil }
type Node interface{ Next() Node }
type Keyed interface{ Key() [4]uint16 }
type Wide interface{ Key() *[5]int8; Pick(k [6]bool) uint8 }
func Process(s, i []byte) uint64 {
`

func TestInterfaceAcceptedForms(t *testing.T) {
	for name, body := range map[string]string{
		"any variable":            `var a any; _ = a`,
		"nil interface":           `var a any = nil; if a == nil { s[0] = 1 }`,
		"value receiver":          `var x Shape = R{1, 2}; s[0] = byte(x.Area())`,
		"pointer receiver":        `r := R{1, 2}; var x Solid = &r; x.Scale(2); s[0] = byte(x.Area())`,
		"embedded interface":      `var x Solid = &R{1, 1}; var y Shape = x; s[0] = byte(y.Area())`,
		"multiple results":        `var x Two = R{1, 1}; a, b := x.Two(); if b { s[0] = byte(a) }`,
		"explicit conversion":     `a := any(R{}); b := Shape(R{}); c := any(b); _, _ = a, c`,
		"argument and result":     `a := id(uint8(3)); s[0] = a.(uint8)`,
		"assertion comma ok":      `var a any = int16(-1); v, ok := a.(int16); if ok { s[0] = byte(v) }`,
		"assertion to interface":  `var a any = R{1, 1}; if x, ok := a.(Shape); ok { s[0] = byte(x.Area()) }`,
		"single assertion":        `var a any = &R{}; a.(*R).W = 3`,
		"type switch":             `var a any = i[0]; switch v := a.(type) { case nil: case uint8, int8: _ = v; case Shape: s[0] = byte(v.Area()); default: }`,
		"empty type switch":       `var a any; switch a.(type) {}`,
		"switch without binding":  `var a any = E{}; switch a.(type) { case E: s[0] = 1 }`,
		"comparisons":             `var a, b any = uint64(1), R{}; if a == b || a != uint64(2) || (R{} == b) || a == nil { s[0] = 1 }`,
		"expression switch":       `var a any = int8(1); switch a { case int8(1), nil: s[0] = 1 }`,
		"struct fields":           `h := H{A: uint32(1), S: R{1, 1}}; s[0] = byte(h.S.Area()); h.A = nil`,
		"slices":                  `xs := []Shape{R{1, 1}, &R{2, 2}}; xs = append(xs, R{}); s[0] = byte(xs[1].Area())`,
		"arrays":                  `var xs [2]any; xs[0] = uint8(1); if xs == [2]any{uint8(1), nil} { s[0] = 1 }`,
		"pointer to interface":    `var a any; p := &a; *p = int64(-1); s[0] = byte(a.(int64))`,
		"generic dynamic type":    `var g Getter[int8] = Box[int8]{-1}; s[0] = byte(g.Get())`,
		"generic interface type":  `var g Getter[any] = Box[any]{V: nil}; if g.Get() == nil { s[0] = 1 }`,
		"interface type argument": `b := Box[Shape]{R{1, 1}}; s[0] = byte(b.V.Area())`,
		"tuple conversion":        `var a any; var ok bool; a, ok = two(); _, _ = a, ok`,
		"tuple return":            `a, ok := ret(); if ok && takes(two()) { _ = a }`,
		"recursive interface":     `var n Node; if n != nil { n = n.Next() }`,
		"unimplemented array":     `var k Keyed; if k != nil { s[0] = byte(k.Key()[1]) }`,
		"uncalled array methods":  `var w Wide; _ = w`,
		"array pointer method":    `var w Wide; if w != nil { s[0] = byte(w.Key()[0]) }`,
		"range assignment":        `var a any; for _, a = range []int8{1, -1} { _ = a }; for a = range 2 { _ = a }`,
		"zero-size value":         `var a any = E{}; var b any = [0]uint8{}; _, _ = a, b`,
		"pointer escapes":         `v := uint64(1); var a any = &v; _ = a`,
		"slice view escapes":      `var k [4]byte; var a any = k[:]; _ = a`,
		"signed values":           `var a any = int8(-3); if a.(int8) < 0 { s[0] = 1 }`,
		"untyped constants":       `var a, b any = 1, true; _, _ = a.(int), b.(bool)`,
	} {
		t.Run(name, func(t *testing.T) {
			source := ifacePrefix + body + "\nreturn 0 }\n"
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

func TestInterfaceRejections(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"float boxing":         {`var a any = 1.5; _ = a`, "unsupported type float64"},
		"zero-size part":       {`type Z struct{ a [0]byte; b uint64 }; _ = Z{}`, "unsupported local declaration"},
		"method expression":    {`_ = Shape.Area(R{})`, "method expressions of interface types unsupported"},
		"method value":         {`var x Shape = R{}; f := x.Area; _ = f`, "method values unsupported; call the method directly"},
		"map in interface":     {`var m map[uint8]uint8; var a any = m; _ = a`, "unsupported type map[uint8]uint8"},
		"generic float getter": {`var g Getter[float32]; _ = g`, "instantiation Getter[float32]: type argument float32 for T: unsupported type float32"},
	} {
		t.Run(name, func(t *testing.T) {
			source := ifacePrefix + c.body + "\nreturn 0 }\n"
			if got := allocMessage(t, source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
			if _, err := Compile("alloc.go", []byte(source)); err == nil || !strings.HasSuffix(err.Error(), c.want) {
				t.Fatalf("CompileProgram disagrees with Check: %v", err)
			}
		})
	}
	for name, c := range map[string]struct{ source, want string }{
		"float method": {"package p\ntype S interface{ Value() float64 }\nfunc f(x S) {}\nfunc Process(s, i []byte) uint64 { return 0 }\n",
			"alloc.go:3:1: interface method Value: unsupported type float64"},
		"variadic method": {"package p\ntype V interface{ M(xs ...uint64) }\nfunc f(x V) {}\nfunc Process(s, i []byte) uint64 { return 0 }\n",
			"alloc.go:3:1: interface method M: variadics are unsupported"},
		"function parameter": {"package p\ntype F interface{ M(f func()) }\nfunc f(x F) {}\nfunc Process(s, i []byte) uint64 { return 0 }\n",
			"alloc.go:3:1: interface method M: unsupported type func()"},
		"zero-size part": {"package p\ntype Z struct{ a [0]byte; b uint64 }\nfunc Process(s, i []byte) uint64 { var a any = Z{}; _ = a; return 0 }\n",
			"alloc.go:3:48: interface values must not hold program.Z: it contains zero-length arrays or empty structs"},
		"context": {"package p\nimport \"gosvm/solana\"\nfunc Process(c solana.Context) uint64 { var a any = c; _ = a; return 0 }\n",
			"alloc.go:3:53: Context values cannot be converted to interfaces"},
		// A dynamic call may reach any implementation: a cycle through one
		// is recursion.
		"recursion through dispatch": {"package p\ntype I interface{ M() uint64 }\ntype R struct{}\nfunc (R) M() uint64 { var i I = R{}; return i.M() }\nfunc Process(s, i []byte) uint64 { return R{}.M() }\n",
			"alloc.go:4:1: recursion is not supported"},
		"mutual recursion through dispatch": {"package p\ntype I interface{ M() uint64 }\ntype A struct{}\ntype B struct{}\nfunc (A) M() uint64 { var i I = B{}; return i.M() }\nfunc (B) M() uint64 { return f() }\nfunc f() uint64 { var i I = A{}; return i.M() }\nfunc Process(s, i []byte) uint64 { return f() }\n",
			"alloc.go:5:1: recursion is not supported"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(c.source)}}}}})
			if err == nil || err.Error() != c.want {
				t.Fatalf("got  %v\nwant %q", err, c.want)
			}
		})
	}
}

// The checked program lists conversions, dynamic types (IDs in key order),
// method slots, method tables, dynamic calls, assertions and type switches.
func TestCheckedProgramInterfaces(t *testing.T) {
	source := ifacePrefix + `var x Solid = &R{1, 1}
	x.Scale(2)
	var g Getter[int8] = Box[int8]{-1}
	var a any = g
	if sh, ok := a.(Shape); ok { s[0] = byte(sh.Area()) }
	switch v := a.(type) { case Box[int8]: s[1] = byte(v.V); case nil: }
	_ = a == uint16(3)
	a = nil
	var r R
	a = r
	return uint64(g.Get())
}
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var conversions []string
	for _, c := range checked.Conversions {
		conversions = append(conversions, fmt.Sprintf("%s->%s@%d", typeKey(c.From), typeKey(c.To), c.Index))
	}
	if got, want := strings.Join(conversions, " "), "program.R->interface{}@0 *program.R->program.Solid@-1 program.Box[int8]->program.Getter[int8]@-1 untyped nil->interface{}@-1 program.R->interface{}@-1"; got != want {
		t.Errorf("conversions:\n got %s\nwant %s", got, want)
	}
	var dynamic []string
	for _, d := range checked.DynamicTypes {
		var methods []string
		for _, m := range d.Methods {
			methods = append(methods, fmt.Sprintf("%d:%s:%v", m.Slot.Index, m.Func.Name(), m.Deref))
		}
		dynamic = append(dynamic, fmt.Sprintf("%d %s boxed=%v direct=%v size=%d cmp=%v [%s]", d.ID, d.Key, d.Boxed, d.Direct, d.Size, d.Comparable, strings.Join(methods, " ")))
	}
	want := []string{
		"1 *program.R boxed=true direct=true size=8 cmp=true [0:Area:true 2:Scale:false]",
		"2 program.Box[int8] boxed=true direct=false size=1 cmp=true [1:Box[int8].Get:false]",
		"3 program.R boxed=true direct=false size=16 cmp=true [0:Area:false]",
		"4 uint16 boxed=false direct=false size=2 cmp=true []",
	}
	if got := strings.Join(dynamic, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("dynamic types:\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	var slots []string
	for _, s := range checked.MethodSlots {
		slots = append(slots, fmt.Sprintf("%d %s called=%v", s.Index, s.Key, s.Called))
	}
	if got, want := strings.Join(slots, "; "), "0 Area() (uint64) called=true; 1 Get() (int8) called=true; 2 Scale(uint64) called=true"; got != want {
		t.Errorf("slots: %s\nwant %s", got, want)
	}
	if len(checked.DynamicCalls) != 3 || len(checked.TypeAssertions) != 1 || len(checked.TypeSwitches) != 1 {
		t.Errorf("calls %d, assertions %d, switches %d", len(checked.DynamicCalls), len(checked.TypeAssertions), len(checked.TypeSwitches))
	}
	var impl []string
	for _, d := range checked.Implementers(types.Universe.Lookup("any").Type()) {
		impl = append(impl, d.Key)
	}
	if got := strings.Join(impl, " "); got != "*program.R program.Box[int8] program.R" {
		t.Errorf("implementers of any: %s", got)
	}
	// The method of the generic type is instantiated for the dynamic call
	// though no direct call names it.
	found := false
	for _, inst := range checked.Instances {
		found = found || inst.Name == "Box[int8].Get"
	}
	if !found {
		t.Error("Box[int8].Get was not instantiated")
	}
}

// Converting a reference to an interface is an escape: the referenced local
// moves to the heap; an interface value holding only heap data moves nothing.
func TestInterfaceHeapMoves(t *testing.T) {
	source := `package p
type I interface{ M(p *uint64) *uint64 }
type T struct{}
func (T) M(p *uint64) *uint64 { return p }
func keep(p *uint64) any { return p }
func Process(s, i []byte) uint64 {
	a := uint64(1)
	var x any = &a
	b := uint64(2)
	y := keep(&b)
	var k [4]byte
	var z any = k[:]
	c := uint64(3)
	var t I = T{}
	r := t.M(&c)
	d := uint64(4)
	_ = d
	var w any = d
	_, _, _, _ = x, y, z, w
	var kp *uint64
	switch v := w.(type) {
	case uint64:
		kp = &v
	case *uint64:
		kp = v
	}
	return *r + *kp
}
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, fn := range checked.Functions {
		var names []string
		for _, v := range fn.HeapVars {
			names = append(names, v.Name())
		}
		sort.Strings(names)
		got[fn.Object.Name()] = fmt.Sprintf("%v escapes=%v", names, fn.Borrows.Escapes)
	}
	for name, want := range map[string]string{
		"Process": "[a b c k v] escapes=[]", // v: the uint64 clause's variable
		"keep":    "[] escapes=[0]",
	} {
		if got[name] != want {
			t.Errorf("%s: %s, want %s", name, got[name], want)
		}
	}
}

// Programs without interface values emit no interface runtime; the C of an
// interface program is deterministic.
func TestInterfaceRuntimeOnlyWhenUsed(t *testing.T) {
	plain := "package p\ntype C interface{ ~uint8 }\nfunc Id[T C](x T) T { return x }\nfunc Process(s, i []byte) uint64 { return uint64(Id(s[0])) }\n"
	out, err := Compile("a.go", []byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("gosvm_iface")) || bytes.Contains(out, []byte("gosvm_alloc")) {
		t.Fatal("a constraint-only program got the interface runtime")
	}
	source := ifaceSource(t)
	first, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: source}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for k := 0; k < 3; k++ {
		again, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: source}}}}})
		if err != nil || !bytes.Equal(first, again) {
			t.Fatalf("emitted C differs between builds: %v", err)
		}
	}
}

// Unexported interface methods are package-qualified: a type in another
// package with a method of the same name does not implement the interface.
func TestInterfaceCrossPackage(t *testing.T) {
	lib := `package lib
type Counter interface {
	inc() uint64
	Get() uint64
}
type Getter interface{ Get() uint64 }
type C struct{ n uint64 }
func (c *C) inc() uint64 { c.n++; return c.n }
func (c *C) Get() uint64 { return c.n }
func New() Counter { return &C{} }
func Bump(c Counter) uint64 { return c.inc() }
`
	app := `package app
import "lib"
type Mine struct{ v uint64 }
func (m Mine) inc() uint64 { return 100 }
func (m Mine) Get() uint64 { return m.v }
func Process(s, i []byte) uint64 {
	c := lib.New()
	lib.Bump(c)
	lib.Bump(c)
	var g lib.Getter = c
	var h lib.Getter = Mine{v: uint64(i[0])}
	_, isCounter := h.(lib.Counter)
	_, isCounter2 := g.(lib.Counter)
	r := g.Get()*10 + h.Get()
	if isCounter {
		r = r + 1000
	}
	if isCounter2 {
		r = r + 100
	}
	return r
}
`
	program := &Program{Entry: "app", Packages: []PackageSources{{"lib", []Source{{Name: "lib.go", Data: []byte(lib)}}}, {"app", []Source{{Name: "app.go", Data: []byte(app)}}}}}
	checked, err := Check(program)
	if err != nil {
		t.Fatal(err)
	}
	var slots []string
	for _, s := range checked.MethodSlots {
		slots = append(slots, s.Key)
	}
	if got := strings.Join(slots, "; "); got != "Get() (uint64); lib.inc() (uint64)" {
		t.Errorf("slots: %s", got)
	}
	c, err := CompileProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	in := make([]byte, 128)
	in[0] = 5
	if got := runHost(t, bin, fmt.Sprintf("%x\n", in)); !strings.HasPrefix(got, "125:") {
		t.Fatalf("result %.10s, want 125", got)
	}
}
