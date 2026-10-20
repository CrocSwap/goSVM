package compiler

import (
	"go/ast"
	"go/types"
	"strings"
	"testing"
)

// TestVariadicForms: declared variadic functions and methods, generic ones,
// calls with no, one and several extra arguments, spread slices, nil
// spreads, deferred calls and interface elements check.
func TestVariadicForms(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"none":         `func f(xs ...uint64) uint64 { return uint64(len(xs)) }; func Process(s, i []byte) uint64 { return f() }`,
		"several":      `func f(a byte, xs ...uint64) uint64 { return uint64(a) + uint64(len(xs)) }; func Process(s, i []byte) uint64 { return f(1, 2, 3, uint64(s[0])) }`,
		"spread":       `func f(xs ...byte) uint64 { return uint64(len(xs)) }; func Process(s, i []byte) uint64 { return f(s...) + f(nil...) }`,
		"method":       `type A struct{ n uint64 }; func (a *A) Add(xs ...uint64) { for _, x := range xs { a.n += x } }; func Process(s, i []byte) uint64 { var a A; a.Add(1, 2); a.Add(); return a.n }`,
		"generic":      `func n[T any](xs ...T) int { return len(xs) }; func Process(s, i []byte) uint64 { return uint64(n(1, 2) + n[byte]() + n("a")) }`,
		"defer":        `func f(xs ...uint64) {}; func Process(s, i []byte) uint64 { defer f(1, 2); return 0 }`,
		"interface":    `type E struct{}; func (E) Error() string { return "e" }; func f(errs ...error) int { return len(errs) }; func Process(s, i []byte) uint64 { return uint64(f(E{}, nil)) }`,
		"package init": `func f(xs ...uint64) uint64 { return uint64(len(xs)) }; var n = f(1, 2, 3); func Process(s, i []byte) uint64 { return n }`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := stdlibCheck(t, "package p;"+body); got != "" {
				t.Fatal(got)
			}
		})
	}
}

// TestVariadicRejections: variadic function values, literals, function
// types and interface methods stay unsupported.
func TestVariadicRejections(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ body, want string }{
		"function value": {`func f(xs ...uint64) {}; func Process(s, i []byte) uint64 { g := f; _ = g; return 0 }`, "variadic function types are unsupported"},
		"literal":        {`func Process(s, i []byte) uint64 { g := func(xs ...uint64) {}; g(); return 0 }`, "variadics are unsupported"},
		"method value":   {`type A struct{}; func (A) M(xs ...uint64) {}; func Process(s, i []byte) uint64 { var a A; g := a.M; _ = g; return 0 }`, "variadic function types are unsupported"},
		"interface":      {`type I interface{ M(...uint64) }; func Process(s, i []byte) uint64 { var x I; _ = x; return 0 }`, "interface method M: variadics are unsupported"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := stdlibCheck(t, "package p;"+c.body); !strings.Contains(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestVariadicLowering: Check rewrites every call that does not end in
// "..." into one that passes the final parameter as a slice literal or nil
// (docs/BACKEND_INTERFACE.md#variadic-functions).
func TestVariadicLowering(t *testing.T) {
	t.Parallel()
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(
		"package p\nfunc f(a byte, xs ...uint64) uint64 { return uint64(len(xs)) }\nfunc Process(s, i []byte) uint64 { return f(1) + f(2, 3, 4) + f(5, nil...) }\n")}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, file := range checked.lowered {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "f" {
				last := call.Args[len(call.Args)-1]
				kind := "nil"
				if lit, ok := last.(*ast.CompositeLit); ok {
					kind = types.TypeString(checked.Info.Types[lit].Type, nil) + "{" + strings.Repeat("x", len(lit.Elts)) + "}"
				} else if !checked.Info.Types[last].IsNil() {
					kind = "?"
				}
				if !call.Ellipsis.IsValid() {
					kind += " without ..."
				}
				calls = append(calls, kind)
			}
			return true
		})
	}
	if got := strings.Join(calls, ", "); got != "nil, []uint64{xx}, nil" {
		t.Fatalf("lowered calls: %s", got)
	}
}

// TestPointerToPointerBorrows: a reference stored through a **T is stored
// into heap data, so a local stored that way moves to the heap, and a
// parameter whose address is taken is the caller's obligation.
func TestPointerToPointerBorrows(t *testing.T) {
	t.Parallel()
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(`package p
func viaLocal() *uint64 {
	var p *uint64
	pp := &p
	x := uint64(5)
	*pp = &x
	return p
}
func store(pp **uint64) {
	y := uint64(1)
	*pp = &y
}
func param(p *uint64) *uint64 {
	pp := &p
	return *pp
}
func Process(s, i []byte) uint64 {
	var q *uint64
	store(&q)
	v := uint64(7)
	return *viaLocal() + *q + *param(&v)
}
`)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	moved := map[string]string{}
	escapes := map[string][]int{}
	for _, fn := range checked.Functions {
		var names []string
		for _, v := range fn.HeapVars {
			names = append(names, v.Name())
		}
		moved[fn.Object.Name()] = strings.Join(names, " ")
		escapes[fn.Object.Name()] = fn.Borrows.Escapes
	}
	if moved["viaLocal"] != "x" || moved["store"] != "y" || moved["Process"] != "v" {
		t.Fatalf("heap moves %v", moved)
	}
	if len(escapes["param"]) != 1 || escapes["param"][0] != 0 {
		t.Fatalf("param escapes %v", escapes["param"])
	}
}
