package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
	"testing"
)

// Package-level variables and init functions (docs/GO_GLOBALS.md): forms,
// read-only placement, rejections, Go's initialization order, the globals
// block's heap accounting, traps during initialization and the backend view.

func globalsProgram(packages ...PackageSources) *Program {
	return &Program{Entry: packages[0].Path, Packages: packages}
}

func unitFiles(path string, files ...string) PackageSources {
	unit := PackageSources{Path: path}
	for j, data := range files {
		unit.Sources = append(unit.Sources, Source{Name: fmt.Sprintf("%s/f%d.go", path, j), Data: []byte(data)})
	}
	return unit
}

// runGlobalHost compiles p with the default heap and runs Process once over
// the 1 KiB output and the input in, returning "result:hex" or "P:hex".
func runGlobalHost(t *testing.T, p *Program, in []byte) string {
	t.Helper()
	c, err := CompileProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	padded := make([]byte, 128)
	copy(padded, in)
	return runHost(t, bin, fmt.Sprintf("%x\n", padded))
}

func resultOf(t *testing.T, out string) string {
	t.Helper()
	code, _, ok := strings.Cut(out, ":")
	if !ok {
		t.Fatalf("no result: %q", out)
	}
	return code
}

// Each program's Process returns the value native Go returns (worked out by
// hand and noted beside each case); each run starts from fresh variables.
func TestGlobalAcceptedForms(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		want string
	}{
		"zero values": {`type P struct{ a uint8; b [2]uint16 }
var u uint64; var i int8; var b bool; var s string; var sl []uint32; var m map[uint8]uint8; var p *uint64; var e error; var f func() uint64; var st P; var arr [3]int64
func Process(o, in []byte) uint64 {
	n := u + uint64(i) + uint64(len(s)) + uint64(len(sl)) + uint64(len(m)) + uint64(st.a) + uint64(st.b[1]) + uint64(arr[2])
	if !b && p == nil && e == nil && f == nil && sl == nil && m == nil { n += 100 }
	u++; st.b[1] = 7; arr[0] = -1
	return n + u + uint64(st.b[1])
}`, "108"},
		"dependency order": {`var a = b + c(); var b uint64 = 2; var d uint64 = 10
func c() uint64 { return d * 3 }
func Process(o, in []byte) uint64 { return a }`, "32"},
		"init after variables, in source order": {`var x uint64 = 1
func init() { x = x * 10 }
func init() { x = x + 5 }
var y = x
func Process(o, in []byte) uint64 { return x*100 + y }`, "1501"},
		"blank and multiple values": {`var n uint64
var _ = bump()
var a, b = pair()
func bump() uint64 { n++; return n }
func pair() (uint64, uint64) { n += 10; return n, n + 1 }
func Process(o, in []byte) uint64 { return n*10000 + a*100 + b }`, "111112"},
		"pointers to package storage": {`type S struct{ f [2]uint64 }
var s S; var p = &s.f[1]; var q *uint64
func set(x *uint64, v uint64) { *x = v }
func Process(o, in []byte) uint64 { set(p, 9); local := uint64(4); q = &local; local++; return s.f[1]*10 + *q }`, "95"},
		"closures over package variables": {`var count uint64
var inc = func() { count++ }
func Process(o, in []byte) uint64 { inc(); inc(); f := func() uint64 { count += 10; return count }; return f() + count }`, "24"},
		"function-valued variables": {`var op func(uint64) uint64
var ops = []func(uint64) uint64{double}
func double(x uint64) uint64 { return 2 * x }
func init() { op = func(x uint64) uint64 { return x + 1 } }
func Process(o, in []byte) uint64 { return op(ops[0](5)) }`, "11"},
		"interfaces and errors": {`type E struct{ code uint8 }
func (e *E) Error() string { return "e" }
var ErrA = &E{1}; var ErrB error = &E{2}
func find(k uint8) error { if k == 0 { return ErrA }; return ErrB }
func Process(o, in []byte) uint64 { n := uint64(0); if find(0) == error(ErrA) { n++ }; if find(1) == ErrB { n += 10 }; if find(1) != error(ErrA) { n += 100 }; return n }`, "111"},
		"maps and strings": {`var m = map[string]uint64{"a": 1}
var s = "ab"; var t string
func init() { t = s + "c"; m[t] = 3 }
func Process(o, in []byte) uint64 { m["a"] += 5; return m["a"]*10 + m["abc"] + uint64(len(t))*100 }`, "363"},
		"generic code writes a global": {`var hits uint64
func touch[T any](v T) T { hits++; return v }
var first = touch[uint8](7)
func Process(o, in []byte) uint64 { touch[uint64](1); return hits*10 + uint64(first) }`, "27"},
		"method values and pointer methods": {`type C struct{ n uint64 }
func (c *C) Add(k uint64) { c.n += k }
func (c C) Get() uint64 { return c.n }
var c C
var add = c.Add
func Process(o, in []byte) uint64 { add(3); c.Add(4); return c.Get() }`, "7"},
		"read-only tables": {`var pows = [...]uint64{1, 10, 100}
var tbl = []uint16{5, 6, 7}
type K struct{ a uint8; b [2]bool }
var ks = []K{{1, [2]bool{true, false}}, {a: 2}}
var name = "xyz"
func Process(o, in []byte) uint64 { n := pows[2] + uint64(tbl[1]) + uint64(len(name)) + uint64(ks[1].a); if ks[0].b[0] { n += 1000 }; for _, v := range tbl { n += uint64(v) }; b := make([]uint16, 2); copy(b, tbl); b = append(b, tbl...); return n + uint64(b[1])*10000 + uint64(b[4]) }`, "61136"},
		"empty struct singleton": {`type order struct{}
func (order) Get(b []byte) uint64 { return uint64(b[0])<<8 | uint64(b[1]) }
var BigEndian order
func Process(o, in []byte) uint64 { return BigEndian.Get([]byte{1, 2}) }`, "258"},
		"escaping locals and arrays": {`var keep []uint64; var arrs [][]uint64
func Process(o, in []byte) uint64 { a := [2]uint64{1, 2}; keep = a[:]; a[1] = 9; arrs = append(arrs, keep); return keep[1] + arrs[0][0] }`, "10"},
	} {
		t.Run(name, func(t *testing.T) {
			got := runGlobalHost(t, globalsProgram(unitFiles("program", "package p\n"+tc.src)), nil)
			if r := resultOf(t, got); r != tc.want {
				t.Fatalf("result %s, want %s", r, tc.want)
			}
		})
	}
}

// Packages initialize in Go 1.21's order (imports first, then by import
// path) and files within a package by name; qualified variables are read,
// written, addressed and called across packages.
func TestGlobalPackages(t *testing.T) {
	logPkg := unitFiles("ex/log", `package log
var Trace []byte
func Mark(b byte) byte { Trace = append(Trace, b); return b }`)
	p := globalsProgram(
		unitFiles("ex/app", `package app
import ("ex/log"; "ex/b"; "ex/a")
var X = log.Mark('x') + a.A
func init() { log.Mark('X') }
func Process(o, in []byte) uint64 {
	b.Counter++
	p := &a.Arr[1]
	*p = 5
	b.Hook = func(v uint64) uint64 { return v + 100 }
	copy(o, log.Trace)
	return b.Counter*1000 + a.Arr[1]*100 + b.Hook(uint64(X)) - uint64(X)
}`),
		logPkg,
		unitFiles("ex/a", `package a
import ("ex/log"; "ex/c")
var A = log.Mark('a') + c.C
var Arr [2]uint64
func init() { log.Mark('A') }`, `package a
import "ex/log"
func init() { log.Mark('2') }`),
		unitFiles("ex/b", `package b
import "ex/log"
var Counter = uint64(log.Mark('b'))
var Hook func(uint64) uint64`),
		unitFiles("ex/c", `package c
import "ex/log"
var C = log.Mark('c') - 'c'`),
	)
	got := runGlobalHost(t, p, nil)
	// log has no imports; then b (all imports done), then c, a, app. A
	// pre-1.21 depth-first order would run c and a before b.
	if want := fmt.Sprintf("%d:%x", 99*1000+5*100+100, "bcaA2xX"); !strings.HasPrefix(got, want) {
		t.Fatalf("got %.40s, want %s", got, want)
	}
	checked, err := Check(p)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, pi := range checked.Inits {
		order = append(order, fmt.Sprintf("%s:%v:%d", pi.Package.Path(), pi.Vars != nil, len(pi.Funcs)))
	}
	if got, want := strings.Join(order, " "), "ex/b:true:0 ex/c:true:0 ex/a:true:2 ex/app:true:1"; got != want {
		t.Fatalf("Inits %s, want %s", got, want)
	}
	if got := packageOrder(map[string]*types.Package{"z": types.NewPackage("z", "z"), "a": types.NewPackage("a", "a")}); strings.Join(got, ",") != "a,z" {
		t.Fatalf("independent packages by path: %v", got)
	}
}

// Read-only placement is decided by every use; anything that could write the
// variable keeps it in the globals block.
func TestGlobalReadOnly(t *testing.T) {
	src := `package p
type S struct{ a uint64; b [2]uint8 }
func (s *S) Ptr() uint64 { return s.a }
func (s S) Val() uint64 { return s.a }
func (s *S) Set() { s.a = 1 }
func rd(p *uint64) uint64 { return *p }
func rd2(p *S) uint64 { return rd(&p.a) + p.Val() + p.Ptr() + uint64(p.b[1]) }
func keep(p *uint64) *uint64 { return p }
func later(p *uint64) func() uint64 { return func() uint64 { return *p } }
func move(p *uint64) uint64 { q := p; return *q }
func bump(p *[2]uint8) { p[1]++ }
func viaBump(p *S) { bump(&p.b) }
var r1 = 5
var r2 = [3]uint8{1, 2, 3}
var r3 = S{a: 1}
var r4 = []uint32{1, 2}
var r5 = "str"
var r6 S
var r7 = []S{{a: 1}}
var r8 = [2]bool{true}
var r9 = []uint8{1, 2, 3}
var w1 = 5
var w2 = [3]uint8{1, 2, 3}
var w3 = S{a: 1}
var w4 = []uint32{1, 2}
var w5 = []uint32{1, 2}
var w6 = []uint32{1, 2}
var r10 S
var r11 = [2]uint64{4, 5}
var r12 = S{a: 2}
var w8 = [2]uint64{}
var w9 = S{}
var w10 = [2]uint64{}
var w11 = []S{{a: 1}}
var w12 uint64 = 3
var w13 = 1
var w14 = 2
var w15 = []uint32{3}
var w16 uint64
var w7 S
var w17 = [2]uint64{1}
var w18 = [2]uint64{1}
var w19 uint64 = 1
var w20 = S{a: 1}
var w21 = S{a: 1}
var x1 = uint64(r1) + 1  // not constant
var x2 = []uint32{}      // empty slice literal: a non-nil, empty slice
var x3 []string          // strings are not plain data
var x4 *uint64
var x5 = [2]string{"a"}
var x6, x7 = two()
func two() (uint64, uint64) { return 1, 2 }
func sum(s []uint32) uint32 { return s[0] }
func set[T any](p *T, v T) { *p = v }
func init() { w13 = 3 }
func Process(o, in []byte) uint64 {
	n := uint64(r1) + uint64(r2[in[0]%3]) + r3.Val() + uint64(len(r4)) + uint64(r4[0]) + uint64(len(r5)) + r6.a + r7[0].a
	for _, v := range r4 { n += uint64(v) }
	c := r3; c.a++
	if r4 != nil && r8[0] { n++ }
	buf := make([]uint8, 2)
	copy(buf, r9)
	buf = append(buf, r9...)
	n += uint64(buf[3])
	w1++
	w2[0] = 9
	w3.b[1]++
	_ = sum(w4)
	w5 = append(w5, 1)
	s := w6[:1]; s[0] = 7
	_ = r10.Ptr()
	n += rd(&r11[1]) + rd2(&r12)
	w7.Set()
	_ = keep(&w17[0])
	_ = later(&w18[1])
	n += move(&w19)
	viaBump(&w20)
	n += rd2(&w21) + *keep(&w21.a)
	f := w9.Ptr; _ = f()
	p := &w8[1]; *p = 2
	q := w10[:]; q[0] = 1
	w11[0].a = 3
	set(&w12, 4)
	func() { w14 += 1 }()
	t := w15; t[0] = 1
	for w16 = range 3 {}
	return n + c.a + x1 + uint64(len(x2)) + uint64(len(x3)) + x6 + x7 + uint64(len(x5[0])) + uint64(w13+w14)
}`
	checked, err := Check(globalsProgram(unitFiles("program", src)))
	if err != nil {
		t.Fatal(err)
	}
	offset := int64(0)
	for _, g := range checked.Globals {
		want := strings.HasPrefix(g.Var.Name(), "r")
		if g.ReadOnly != want {
			t.Errorf("%s: read-only %v, want %v", g.Var.Name(), g.ReadOnly, want)
		}
		if g.ReadOnly != (g.Offset < 0) || !g.ReadOnly && g.Offset < offset {
			t.Errorf("%s: offset %d", g.Var.Name(), g.Offset)
		}
		if !g.ReadOnly {
			offset = g.Offset
		}
	}
	if checked.GlobalBytes == 0 || checked.GlobalBytes%8 != 0 {
		t.Fatalf("GlobalBytes %d", checked.GlobalBytes)
	}
	// The program compiles and runs; read-only data is constant C storage.
	c, err := CompileProgram(globalsProgram(unitFiles("program", src)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c), "static const array") || !strings.Contains(string(c), "gosvm_globals") {
		t.Fatal("read-only data or the globals block missing")
	}
	hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
}

func TestGlobalRejections(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"context variable": {`import "gosvm/solana"
var d solana.Context
func Process(c solana.Context) uint64 { return 0 }`, "f0.go:3:5: Context must come from the entrypoint, not a package-level variable"},
		"float variable": {`var x float64
func Process(s, i []byte) uint64 { return 0 }`, "f0.go:2:5: unsupported type float64"},
		"unsupported initializer": {`var x = func() uint64 { go g(); return 1 }()
func g() {}
func Process(s, i []byte) uint64 { return x }`, "f0.go:2:25: unsupported statement *ast.GoStmt"},
		"unsupported init body": {`func init() { go g() }
func g() {}
func Process(s, i []byte) uint64 { return 0 }`, "f0.go:2:15: unsupported statement *ast.GoStmt"},
		"recursion through a variable": {`var f func(uint64) uint64
func init() { f = func(n uint64) uint64 { if n == 0 { return 0 }; return f(n - 1) } }
func Process(s, i []byte) uint64 { return f(3) }`, "recursion is not supported"},
		"initialization cycle": {`var a = b
var b = a
func Process(s, i []byte) uint64 { return 0 }`, "initialization cycle"},
		"block beyond the heap budget": {`type Big struct{ a, b, c, d, e [128]uint64 }
var x1, x2, x3, x4, x5, x6, x7 Big
func Process(s, i []byte) uint64 { x1.a[0]++; x2.a[0]++; x3.a[0]++; x4.a[0]++; x5.a[0]++; x6.a[0]++; x7.a[0]++; return 0 }`,
			"f0.go:3:5: package-level variables need 35840 heap bytes; the 32768-byte heap budget leaves 32760"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Check(globalsProgram(unitFiles("program", "package p\n"+tc.src)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	// The same block fits a 64 KiB budget.
	src := "package p\ntype Big struct{ a, b, c, d, e [128]uint64 }\nvar x1, x2, x3, x4, x5, x6, x7 Big\nfunc Process(s, i []byte) uint64 { x1.a[0]++; x2.a[0]++; x3.a[0]++; x4.a[0]++; x5.a[0]++; x6.a[0]++; x7.a[0]++; return 0 }"
	p := globalsProgram(unitFiles("program", src))
	p.HeapBytes = 65536
	if _, err := Check(p); err != nil {
		t.Fatal(err)
	}
}

// The globals block is the invocation heap's first allocation: with a block
// of B bytes, 32,760 - B bytes remain for everything else.
func TestGlobalHeapAccounting(t *testing.T) {
	src := `package p
var counters [5]uint64 // 40 bytes
var flag bool          // 1 byte: the block is 48 bytes
func Process(o, in []byte) uint64 {
	counters[0]++
	flag = true
	n := int(in[0]) | int(in[1])<<8
	b := make([]byte, n)
	return uint64(len(b))
}`
	checked, err := Check(globalsProgram(unitFiles("program", src)))
	if err != nil {
		t.Fatal(err)
	}
	if checked.GlobalBytes != 48 {
		t.Fatalf("GlobalBytes %d, want 48", checked.GlobalBytes)
	}
	fits := 32760 - 48
	for n, abort := range map[int]bool{fits: false, fits + 1: true} {
		got := runGlobalHost(t, globalsProgram(unitFiles("program", src)), []byte{byte(n), byte(n >> 8)})
		if strings.HasPrefix(got, "P:") != abort {
			t.Errorf("make(%d): %.20s", n, got)
		}
	}
}

// A trap during initialization aborts before Process runs: nothing is
// written (Go panics before main).
func TestGlobalInitTrap(t *testing.T) {
	for name, src := range map[string]string{
		"initializer": `var zero uint64
var bad = 10 / zero
func Process(o, in []byte) uint64 { o[0] = 1; return bad }`,
		"init function": `var t = []uint8{1}
func init() { t[1] = 2 }
func Process(o, in []byte) uint64 { o[0] = 1; return 0 }`,
		"nil map in init": `var m map[uint8]uint8
func init() { m[1] = 1 }
func Process(o, in []byte) uint64 { o[0] = 1; return 0 }`,
	} {
		t.Run(name, func(t *testing.T) {
			got := runGlobalHost(t, globalsProgram(unitFiles("program", "package p\n"+src)), nil)
			if !strings.HasPrefix(got, "P:00") {
				t.Fatalf("got %.20s", got)
			}
		})
	}
}

// A package variable used as an operand is read after the calls of the
// same expression, as gc does: a call that changes it is seen
// (docs/ERROR_CONTRACT.md#evaluation-order).
func TestGlobalOperandOrder(t *testing.T) {
	src := `package p
var x uint64 = 1
func f() uint64 { x = 10; return 0 }
func Process(o, in []byte) uint64 { return x + f() }`
	if got := resultOf(t, runGlobalHost(t, globalsProgram(unitFiles("program", src)), nil)); got != "10" {
		t.Fatalf("x must be read after f(), as gc reads it: got %s", got)
	}
}

// Programs without package variables or init functions get none of the
// runtime; programs whose variables are all read-only get no initialization.
func TestGlobalRuntimeOnlyWhenUsed(t *testing.T) {
	for name, tc := range map[string]struct {
		src          string
		block, inits bool
	}{
		"none":      {`func Process(o, in []byte) uint64 { return 0 }`, false, false},
		"read-only": {`var pows = [2]uint64{1, 10}; var name = "n"; func Process(o, in []byte) uint64 { return pows[in[0]&1] + uint64(len(name)) }`, false, false},
		"init only": {`func init() {}; func Process(o, in []byte) uint64 { return 0 }`, false, true},
		"writable":  {`var n uint64; func Process(o, in []byte) uint64 { n++; return n }`, true, true},
	} {
		c, err := Compile("p.go", []byte("package p\n"+tc.src))
		if err != nil {
			t.Fatal(err)
		}
		s := string(c)
		if strings.Contains(s, "gosvm_globals") != tc.block || strings.Contains(s, "gosvm_init") != tc.inits || strings.Contains(s, "gosvm_process") != tc.inits {
			t.Errorf("%s: block %v, init %v", name, strings.Contains(s, "gosvm_globals"), strings.Contains(s, "gosvm_init"))
		}
		if name == "none" && (strings.Contains(s, "gosvm_ro") || strings.Contains(s, "heap")) {
			t.Errorf("none: emitted package-variable runtime")
		}
	}
}

// The backend view: Globals in package order then declaration order; each
// package's synthetic Vars initializer assigns its non-read-only
// initializers in go/types order; init functions follow in source order.
func TestCheckedProgramGlobals(t *testing.T) {
	src := `package p
var a = b + 1
var b uint64 = 2
var _ = b
var t = [2]uint64{1, 2}
var c, d = two()
func two() (uint64, uint64) { return 3, 4 }
func init() {}
func init() { a++ }
func Process(o, in []byte) uint64 { return a + c + d + t[in[0]&1] }`
	checked, err := Check(globalsProgram(unitFiles("program", src)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range checked.Globals {
		names = append(names, fmt.Sprintf("%s:%v", g.Var.Name(), g.ReadOnly))
	}
	// b is constant and never written: read-only, and a reads it there.
	if got := strings.Join(names, " "); got != "a:false b:true t:true c:false d:false" {
		t.Fatalf("Globals %s", got)
	}
	if len(checked.Inits) != 1 || len(checked.Inits[0].Funcs) != 2 || checked.Inits[0].Vars == nil {
		t.Fatalf("Inits %+v", checked.Inits)
	}
	vars := checked.Inits[0].Vars
	if vars.PackageInit == nil || vars.Object.Name() != "init.vars" {
		t.Fatalf("Vars %+v", vars)
	}
	var stmts []string
	for _, s := range vars.Decl.Body.List {
		stmts = append(stmts, types.ExprString(s.(*ast.AssignStmt).Lhs[0]))
	}
	if got := strings.Join(stmts, " "); got != "a _ c" {
		t.Fatalf("initializer order %s, want a _ c", got)
	}
	found := false
	for _, fn := range checked.Functions {
		found = found || fn == vars
	}
	if !found {
		t.Fatal("Vars is not in Functions")
	}
	if checked.GlobalBytes != 24 {
		t.Fatalf("GlobalBytes %d, want 24", checked.GlobalBytes)
	}
}
