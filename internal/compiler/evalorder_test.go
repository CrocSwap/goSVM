package compiler

import (
	"go/ast"
	"go/types"
	"strings"
	"testing"
)

func checkEvalOrder(t *testing.T, src string) *CheckedProgram {
	t.Helper()
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(src)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return checked
}

// hoistedText spells Hoisted as "owner: e1, e2" lines (statements by kind).
func hoistedText(p *CheckedProgram) []string {
	var out []string
	for owner, list := range p.Hoisted {
		name := ""
		if e, ok := owner.(ast.Expr); ok {
			name = types.ExprString(e)
		} else {
			switch owner.(type) {
			case *ast.ReturnStmt:
				name = "return"
			case *ast.AssignStmt:
				name = "assign"
			case *ast.KeyValueExpr:
				name = "entry"
			default:
				name = "statement"
			}
		}
		var parts []string
		for _, e := range list {
			parts = append(parts, types.ExprString(e))
		}
		out = append(out, name+": "+strings.Join(parts, ", "))
	}
	return out
}

// Check records where gc's order differs observably from left to right:
// a package variable read before a call that writes it, and nowhere else.
func TestEvalOrderHoisted(t *testing.T) {
	p := checkEvalOrder(t, `package p
var x uint64
func f() uint64 { x = 10; return 1 }
func pure(v uint64) uint64 { y := v; y++; return y }
func Process(o, in []byte) uint64 {
	a := uint64(in[0])
	b := a + f()     // a local nothing can write: left to right is gc's order
	c := a + pure(x) // pure writes nothing
	return x + f() + b + c
}`)
	got := hoistedText(p)
	if len(got) != 1 || got[0] != "return: f()" {
		t.Fatalf("Hoisted %q, want [return: f()]", got)
	}
	var call, read ast.Expr
	for owner, list := range p.Hoisted {
		call = list[0]
		read = owner.(*ast.ReturnStmt).Results[0].(*ast.BinaryExpr).X.(*ast.BinaryExpr).X.(*ast.BinaryExpr).X
	}
	if !p.Sequenced(call) || p.Sequenced(read) {
		t.Fatalf("Sequenced(%s) %v, Sequenced(%s) %v", types.ExprString(call), p.Sequenced(call), types.ExprString(read), p.Sequenced(read))
	}
	if len(p.MapLiteralOrder)+len(p.SharedOperands)+len(p.HoistedConversions) != 0 {
		t.Fatal("unexpected map orders, shared operands or conversions")
	}
}

// Captured and address-taken locals are writable by calls; a call that
// may panic makes an earlier trap observable (its log line).
func TestEvalOrderLocalsAndTraps(t *testing.T) {
	p := checkEvalOrder(t, `package p
func set(q *uint64) uint64 { *q = 3; return 0 }
func boom(v byte) uint64 {
	if v == 7 {
		panic("boom")
	}
	return 0
}
func Process(o, in []byte) uint64 {
	c := uint64(in[0])
	inc := func() uint64 { c++; return 0 }
	d := uint64(in[1])
	q := &d
	e := c + inc()
	g := d + set(q)
	h := in[5] + byte(boom(in[2]))
	return e + g + uint64(h)
}`)
	got := strings.Join(hoistedText(p), "; ")
	for _, want := range []string{"assign: inc()", "assign: set(q)", "assign: boom(in[2])"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Hoisted %q lacks %q", got, want)
		}
	}
}

// Map literals insert static entries first; m[k] = append(m[k], f()) reuses
// the target's early copies; slow-path interface keys convert early.
func TestEvalOrderMaps(t *testing.T) {
	p := checkEvalOrder(t, `package p
type PM struct{ m map[uint64][]uint64 }
var pm *PM
var gi uint64
var g uint64
func w() uint64 { pm = &PM{m: map[uint64][]uint64{}}; g++; return 0 }
func Process(o, in []byte) uint64 {
	pm = &PM{m: map[uint64][]uint64{}}
	k := uint64(in[0])
	m := map[uint64]uint64{k: 3, 1: 2}
	pm.m[gi] = append(pm.m[gi], w())
	ma := map[any]uint64{}
	return m[1] + uint64(len(pm.m[0])) + ma[g] + w()
}`)
	for lit, order := range p.MapLiteralOrder {
		if len(lit.Elts) != 2 || len(order) != 2 || order[0] != 1 || order[1] != 0 {
			t.Fatalf("MapLiteralOrder %v", order)
		}
	}
	if len(p.MapLiteralOrder) != 1 {
		t.Fatalf("MapLiteralOrder %v", p.MapLiteralOrder)
	}
	shared := 0
	for from, to := range p.SharedOperands {
		if types.ExprString(from) != "pm.m" || types.ExprString(to) != "pm.m" || from == to {
			t.Fatalf("SharedOperands %s -> %s", types.ExprString(from), types.ExprString(to))
		}
		shared++
	}
	if shared != 1 {
		t.Fatalf("%d shared operands", shared)
	}
	converted := 0
	for e := range p.HoistedConversions {
		if types.ExprString(e) != "g" {
			t.Fatalf("HoistedConversions %s", types.ExprString(e))
		}
		converted++
	}
	if converted != 1 {
		t.Fatalf("%d hoisted conversions", converted)
	}
}

// Where no call can change an operand read before it (locals nothing can
// write, calls that write nothing), Check records nothing, so the C is
// unchanged.
func TestEvalOrderNothingToHoist(t *testing.T) {
	p := checkEvalOrder(t, `package p
type T struct{ a uint64 }
func (t T) Get() uint64 { return t.a }
func two(a, b uint64) uint64 { return a*10 + b }
func Process(o, in []byte) uint64 {
	t := T{uint64(in[0])}
	s := []uint64{1, 2, 3}
	n := two(t.a, t.Get()) + s[1] + two(uint64(len(s)), uint64(in[1]))
	if n > 3 && two(n, 1) > 2 {
		n++
	}
	return n
}`)
	if len(p.Hoisted) != 0 {
		t.Fatalf("Hoisted %q", hoistedText(p))
	}
}

// Variadic calls (rewritten by Check to pass a slice literal) and writes
// through pointers to pointers are analyzed like any other operands.
func TestEvalOrderVariadicAndPointerToPointer(t *testing.T) {
	p := checkEvalOrder(t, `package p
var x uint64
var q *uint64
var pq = &q
func f() uint64 { x = 10; return 1 }
func setQ() uint64 { y := uint64(5); *pq = &y; return 2 }
func sum(a uint64, xs ...uint64) uint64 {
	for _, v := range xs {
		a += v
	}
	return a
}
func setP(pp **uint64) uint64 { y := uint64(7); *pp = &y; return 3 }
func Process(o, in []byte) uint64 {
	a := sum(x, x, f())
	b := sum(1, x)
	l := uint64(in[0])
	c := sum(1, l, f())
	v := uint64(in[1])
	pv := &v
	d := *pv + setP(&pv)
	e := **pq + setQ()
	return a + b + c + d + e
}`)
	got := strings.Join(hoistedText(p), "\n")
	for _, want := range []string{"sum(x, (ast: <nil>){…}...): f()", "assign: setP(&pv)", "assign: setQ()"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Hoisted %q, want %q", got, want)
		}
	}
	if n := len(p.Hoisted); n != 3 {
		t.Fatalf("Hoisted %q: want 3 owners (sum(1, x) and sum(1, l, f()) are left to right)", got)
	}
}
