package compiler

import (
	"bytes"
	"go/ast"
	"go/types"
	"strings"
	"testing"
)

// Function values, closures and defer (docs/GO_FUNCTIONS.md): accepted
// forms, rejections with exact messages, the backend view and byte identity.

const funcPrefix = `package p
type C struct{ n uint64 }
func (c C) Get() uint64 { return c.n }
func (c *C) Inc(d uint64) uint64 { c.n = c.n + d; return c.n }
type Op func(a, b uint64) uint64
type I interface{ M() uint64 }
func (c C) M() uint64 { return c.n }
func add(a, b uint64) uint64 { return a + b }
func two() (a, b uint64) { return 1, 2 }
func Id[T any](x T) T { return x }
func apply(f func(uint64) uint64, x uint64) uint64 { return f(x) }
func Process(s, i []byte) uint64 {
`

// funcMessage is Check's diagnostic for a Process body after funcPrefix
// (without the file name), or "".
func funcMessage(t *testing.T, source string) string {
	t.Helper()
	_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
	_, compileErr := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
	if (err == nil) != (compileErr == nil) || err != nil && err.Error() != compileErr.Error() {
		t.Fatalf("Check %v, CompileProgram %v", err, compileErr)
	}
	if err == nil {
		return ""
	}
	if !strings.HasPrefix(err.Error(), "f.go:") {
		t.Fatalf("diagnostic is not source-located: %q", err)
	}
	return err.Error()
}

func TestFuncAcceptedForms(t *testing.T) {
	for name, body := range map[string]string{
		"function value":           `f := add; return f(1, 2)`,
		"named function type":      `var o Op = add; return o(3, 4)`,
		"conversion":               `o := Op(add); var f func(uint64, uint64) uint64 = o; return f(1, 1)`,
		"nil conversion":           `f := (func())(nil); if f == nil { return 1 }; return 0`,
		"nil comparison":           `var f func(); if f != nil { f() }; return 0`,
		"generic instance":         `f := Id[uint8]; return uint64(f(s[0]))`,
		"inferred instance":        `var f func(uint16) uint16 = Id; return uint64(f(2))`,
		"literal":                  `return func(a uint64) uint64 { return a * 2 }(3)`,
		"capture":                  `x := uint64(1); f := func() { x++ }; f(); return x`,
		"capture parameter":        `f := func() byte { return s[0] }; return uint64(f())`,
		"nested capture":           `x := uint64(1); f := func() func() uint64 { return func() uint64 { return x } }; return f()()`,
		"method value":             `c := C{1}; f := c.Get; g := c.Inc; g(1); return f()`,
		"method value via pointer": `p := &C{2}; f := p.Get; g := p.Inc; return f() + g(1)`,
		"method expression":        `f := C.Get; g := (*C).Get; h := (*C).Inc; c := C{1}; return f(c) + g(&c) + h(&c, 1)`,
		"generic method value":     `b := Box[uint8]{V: 3}; f := b.Get; return uint64(f())`,
		"field":                    `h := Hold{f: add}; return h.f(1, 2)`,
		"slice":                    `fs := []Op{add, Op(add)}; fs = append(fs, add); return fs[2](1, 2)`,
		"array":                    `var fs [3]func() uint64; fs[1] = C{4}.Get; return fs[1]()`,
		"interface":                `var a any = add; g := a.(func(uint64, uint64) uint64); return g(1, 2)`,
		"multiple results":         `f := two; a, b := f(); return a + b`,
		"higher order":             `return apply(func(x uint64) uint64 { return x + 1 }, 2)`,
		"named results":            `a, b := two(); return a + b`,
		"defer function":           `defer add(1, 2); return 0`,
		"defer method":             `c := C{1}; defer c.Inc(1); defer c.Get(); return 0`,
		"defer dynamic":            `var x I = C{1}; defer x.M(); return 0`,
		"defer value":              `f := add; defer f(1, 2); return 0`,
		"defer literal":            `defer func(v uint64) { s[0] = byte(v) }(3); return 0`,
		"defer copy":               `d := make([]byte, 2); defer copy(d, i); return 0`,
		"defer in loop":            `for j := range 3 { defer add(uint64(j), 1) }; return 0`,
		"func type parameter":      `return Twice(func(x int8) int8 { return x - 1 }, 3)`,
		"interface with func":      `var u U = UF{}; return u.Do(add)`,
	} {
		t.Run(name, func(t *testing.T) {
			source := funcPrefix + body + "\n}\n" + `type Box[T any] struct{ V T }
func (b Box[T]) Get() T { return b.V }
type Hold struct{ f Op }
func Twice[T any](f func(T) T, x T) uint64 { f(f(x)); return 0 }
type U interface{ Do(f Op) uint64 }
type UF struct{}
func (UF) Do(f Op) uint64 { return f(1, 2) }
`
			c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
			if err != nil {
				t.Fatal(err)
			}
			hostCompile(t, c, DefaultHeapBytes, allocDriver, true)
		})
	}
}

func TestFuncRejections(t *testing.T) {
	for name, c := range map[string]struct{ source, want string }{
		// Recursion through function values: a call through a value may run
		// every target of its signature (the address-taken set).
		"closure calling itself": {funcPrefix + "var f func() uint64\nf = func() uint64 { return f() }\nreturn f()\n}\n",
			"f.go:14:5: recursion is not supported"},
		"function passing itself": {funcPrefix + "return g(1)\n}\nfunc g(x uint64) uint64 { return apply(g, x) }\n",
			"f.go:11:1: recursion is not supported"},
		"literal calling its enclosing function": {funcPrefix + "return 0\n}\nfunc h() uint64 { f := func() uint64 { return h() }; return f() }\n",
			"f.go:15:1: recursion is not supported"},
		"method value calling its method": {funcPrefix + "return 0\n}\nfunc (c C) R() uint64 { f := c.R; return f() }\n",
			"f.go:15:1: recursion is not supported"},
		"mutual recursion through parameters": {funcPrefix + "return a(1)\n}\nfunc a(x uint64) uint64 { return apply(b, x) }\nfunc b(x uint64) uint64 { return apply(a, x) }\n",
			"f.go:11:1: recursion is not supported"},
		// String- and error-typed signatures take part in the same check.
		"string closure calling itself": {funcPrefix + "var f func(string) string\nf = func(s string) string { return f(s + \"x\") }\nreturn uint64(len(f(\"\")))\n}\n",
			"f.go:14:5: recursion is not supported"},
		"string method value calling its method": {funcPrefix + "return 0\n}\ntype L string\nfunc (l L) Up(s string) string { f := l.Up; return f(s) }\n",
			"f.go:16:1: recursion is not supported"},
		"error method through a function value": {funcPrefix + "g := func() string { var err error = E{}; return err.Error() }\nreturn uint64(len(g()))\n}\ntype E struct{ f func() string }\nfunc (e E) Error() string { return e.f() }\n",
			"f.go:13:6: recursion is not supported"},
		// Conservative: a wrapper calling a value of its own signature may
		// run itself, so it is rejected even when no cycle forms at run time.
		"same-signature wrapper": {funcPrefix + "inc := func(x uint64) uint64 { return x + 1 }\nw := func(x uint64) uint64 { return inc(x) }\nreturn w(1)\n}\n",
			"f.go:14:6: recursion is not supported"},
		"interface method value": {funcPrefix + "var x I = C{}\nf := x.M\nreturn f()\n}\n",
			"f.go:14:6: " + ifaceMethodValueRule},
		"interface method expression value": {funcPrefix + "f := I.M\nreturn f(C{})\n}\n",
			"f.go:13:6: " + ifaceMethodValueRule},
		"variadic literal": {funcPrefix + "f := func(xs ...uint64) {}\nf()\nreturn 0\n}\n",
			"f.go:13:6: variadics are unsupported"},
		"variadic function type": {funcPrefix + "var f func(...uint64)\n_ = f\nreturn 0\n}\n",
			"f.go:13:5: variadic function types are unsupported"},
		"pointer to function": {funcPrefix + "f := add\np := &f\nreturn (*p)(1, 2)\n}\n",
			"f.go:14:6: " + pointerElementRule},
		"float parameter": {funcPrefix + "var f func(float64)\n_ = f\nreturn 0\n}\n",
			"f.go:13:5: unsupported type float64"},
		"recover": {funcPrefix + "defer func() { recover() }()\nreturn 0\n}\n",
			"f.go:13:16: " + builtinRule},
		"defer panic": {funcPrefix + "defer panic(1)\nreturn 0\n}\n",
			"f.go:13:7: " + builtinRule},
		"go statement": {funcPrefix + "go add(1, 2)\nreturn 0\n}\n",
			"f.go:13:1: unsupported statement *ast.GoStmt"},
		"zero-size receiver": {funcPrefix + "z := Z{}\nf := z.Get\nreturn f()\n}\ntype Z struct{ a [0]byte; b uint64 }\nfunc (z Z) Get() uint64 { return z.b }\n",
			"f.go:14:6: method values must not bind program.Z: it contains zero-length arrays or empty structs"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := funcMessage(t, c.source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
	for name, c := range map[string]struct{ source, want string }{
		"sdk function value": {"package p\nimport \"gosvm/solana\"\nfunc Process(c solana.Context) uint64 { f := solana.Count; return f(c) }\n",
			"f.go:3:46: " + strings.Replace(intrinsicValueRule, "%s", "gosvm/solana", 1)},
		"defer sdk call": {"package p\nimport \"gosvm/solana\"\nfunc Process(c solana.Context) uint64 { defer solana.Count(c); return 0 }\n",
			"f.go:3:41: " + strings.Replace(deferIntrinsicRule, "%s", "gosvm/solana", 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := funcMessage(t, c.source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// The backend view: targets, classes, closures with their environments,
// defer sites, and captured variables moved to the heap.
func TestCheckedProgramFunctions(t *testing.T) {
	source := funcPrefix + `x := uint64(s[0])
	f := func() uint64 { return x }
	g := add
	m := C{1}.Get
	for j := range 2 {
		defer func() { x = x + uint64(j) }()
	}
	defer m()
	return f() + g(1, 2) + apply(func(v uint64) uint64 { return v }, 1)
}
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for i, target := range checked.FuncTargets {
		if target.ID != uint64(i+1) {
			t.Fatalf("target %d has ID %d", i, target.ID)
		}
		kinds = append(kinds, target.Kind.String())
	}
	// f's literal, add, C.Get bound, the deferred literal, the argument literal.
	if got := strings.Join(kinds, ","); got != "literal,function,method value,literal,literal" {
		t.Fatalf("targets %s", got)
	}
	if len(checked.Closures) != 3 {
		t.Fatalf("%d closures", len(checked.Closures))
	}
	captures := func(cl *Closure) string {
		var names []string
		for _, v := range cl.Captures {
			names = append(names, v.Name())
		}
		return strings.Join(names, ",")
	}
	if captures(checked.Closures[0]) != "x" || checked.Closures[0].EnvBytes != 16 ||
		captures(checked.Closures[1]) != "x,j" || checked.Closures[1].EnvBytes != 24 ||
		captures(checked.Closures[2]) != "" || checked.Closures[2].EnvBytes != 0 {
		t.Fatalf("closures: %s/%d %s/%d %s/%d", captures(checked.Closures[0]), checked.Closures[0].EnvBytes,
			captures(checked.Closures[1]), checked.Closures[1].EnvBytes, captures(checked.Closures[2]), checked.Closures[2].EnvBytes)
	}
	// Calls through values: f(), g(1, 2), m() (deferred), and f(x) in apply.
	signatures := map[string]int{}
	for call, class := range checked.IndirectCalls {
		if checked.FuncClasses[class.Index] != class {
			t.Fatal("class index")
		}
		signatures[types.TypeString(class.Signature, nil)]++
		_ = call
	}
	if signatures["func() uint64"] != 2 || signatures["func(a uint64, b uint64) uint64"] != 1 || signatures["func(uint64) uint64"] != 1 {
		t.Fatalf("indirect calls by signature: %v", signatures)
	}
	if len(checked.DeferSites) != 2 || !checked.DeferSites[0].InLoop || checked.DeferSites[0].Kind != DeferLiteral ||
		checked.DeferSites[0].RecordBytes != 24 || checked.DeferSites[1].InLoop || checked.DeferSites[1].Kind != DeferIndirect {
		t.Fatalf("defer sites: %+v %+v", checked.DeferSites[0], checked.DeferSites[1])
	}
	var process *Function
	for _, fn := range checked.Functions {
		if fn.Object.Name() == "Process" {
			process = fn
		}
	}
	var moved []string
	for _, v := range process.HeapVars {
		moved = append(moved, v.Name())
	}
	if strings.Join(moved, ",") != "x,j" {
		t.Fatalf("moved %v", moved)
	}
	// Every call through a value resolves to its signature's targets.
	for call, class := range checked.IndirectCalls {
		targets := checked.CallTargets(call)
		if len(targets) != len(class.Targets) || len(targets) == 0 {
			t.Fatalf("call targets %d, class %d", len(targets), len(class.Targets))
		}
		for _, target := range targets {
			if !types.Identical(target.Signature, class.Signature) {
				t.Fatal("target signature differs from the call's")
			}
		}
	}
	for e, target := range checked.FuncValues {
		if _, ok := e.(*ast.FuncLit); ok != (target.Kind == TargetLiteral) {
			t.Fatalf("value %T has kind %s", e, target.Kind)
		}
	}
}

// A closure can capture the Context; a deferred one writes account data.
func TestFuncContextCapture(t *testing.T) {
	source := "package p\nimport \"gosvm/solana\"\nfunc Process(c solana.Context) uint64 {\n" +
		"defer func() { solana.Data(c, 0)[0] = 1 }()\nn := func() uint64 { return solana.Count(c) }\nreturn n()\n}\n"
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	hostCompile(t, c, DefaultHeapBytes, "", true)
}

// Programs without function values or defer emit no function-value
// runtime; named results alone need none either.
func TestFuncRuntimeOnlyWhenUsed(t *testing.T) {
	for _, tc := range []struct {
		body string
		used bool
	}{
		{`return 1`, false},
		{`a, b := two(); return a + b`, false},
		{`var f func(); _ = f; return 0`, true},
		{`defer add(1, 2); return 0`, true},
	} {
		source := "package p\nfunc add(a, b uint64) uint64 { return a + b }\nfunc two() (a, b uint64) { return 1, 2 }\nfunc Process(s, i []byte) uint64 {\n" + tc.body + "\n}\n"
		c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := bytes.Contains(c, []byte("typedef u64 gosvm_func;")); got != tc.used {
			t.Errorf("%q: runtime emitted %v", tc.body, got)
		}
	}
}
