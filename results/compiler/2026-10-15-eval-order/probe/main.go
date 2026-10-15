// Command probe prints gc's observable evaluation order for the constructs
// recorded in ../README.md. Run it with the pinned Go 1.22.0, with default
// flags and with -gcflags=-l and -gcflags='-N -l'; the outputs must agree.
package main

import "fmt"

var x int
var s []int
var arr [3]int
var m map[int]int
var p *T
var str string
var b []byte
var i int
var fv func() int
var iface I

type T struct{ a, b int }

func (t T) Val() int        { return t.a }
func (t *T) Ptr() int       { return t.a }
func (t T) Arg(v int) int   { return t.a*100 + v }
func (t *T) PArg(v int) int { return t.a*100 + v }

type I interface{ M(int) int }
type A struct{ n int }

func (a A) M(v int) int { return a.n*100 + v }

type B struct{ n int }

func (b B) M(v int) int { return b.n*1000 + v }

func setX(v int) int    { x = v; return 0 }
func setS() int         { s = []int{100, 200, 300}; return 0 }
func setS0() int        { s[0] = 77; return 0 }
func setArr() int       { arr[0] = 55; return 0 }
func setM() int         { m[1] = 99; return 0 }
func newM() int         { m = map[int]int{1: 500}; return 0 }
func setP() int         { p = &T{40, 41}; return 0 }
func setPa() int        { p.a = 33; return 0 }
func setStr() int       { str = "zz"; return 0 }
func setB() int         { b[0] = 'Q'; return 0 }
func setI() int         { i = 2; return 0 }
func setFv() int        { fv = func() int { return 2 }; return 0 }
func setIface() int     { iface = B{7}; return 0 }
func lenS() int         { s = append(s, 9); return 0 }
func boolX(v int) bool  { x = v; return true }
func id(v int) int      { return v }
func two(a, b int) int  { return a*1000 + b }
func bint(v bool) int {
	if v {
		return 1
	}
	return 0
}

func reset() {
	x = 1
	s = []int{1, 2, 3}
	arr = [3]int{1, 2, 3}
	m = map[int]int{1: 1}
	p = &T{1, 2}
	str = "ab"
	b = []byte("ab")
	i = 0
	fv = func() int { return 1 }
	iface = A{1}
	out = nil
	x8, x64 = 1, 1
}

func probe(name string, f func() string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%-30s panic: %v\n", name, r)
		}
	}()
	reset()
	fmt.Printf("%-30s %s\n", name, f())
}

var out []int
var sv []int

func main() {
	// Binary and unary operands against calls.
	probe("x + f()", func() string { return fmt.Sprint(x + setX(10)) })
	probe("f() + x", func() string { return fmt.Sprint(setX(10) + x) })
	probe("-x + f()", func() string { return fmt.Sprint(-x + setX(10)) })
	probe("x*2 + f()", func() string { return fmt.Sprint(x*2 + setX(10)) })
	probe("x + f() + x", func() string { return fmt.Sprint(x + setX(10) + x) })
	probe("id(x) + f()", func() string { return fmt.Sprint(id(x) + setX(10)) })
	probe("uint8(x) + f()", func() string { return fmt.Sprint(uint8(x) + uint8(setX(10))) })
	probe("x / (f()+1)", func() string { return fmt.Sprint(x / (setX(10) + 1)) })
	// Call arguments.
	probe("two(x, f())", func() string { return fmt.Sprint(two(x, setX(10))) })
	probe("two(x, two(x, f()))", func() string { return fmt.Sprint(two(x, two(x, setX(10)))) })
	probe("two(x+f(), x)", func() string { return fmt.Sprint(two(x+setX(10), x)) })
	probe("(*p).Arg(f()) value recv", func() string { return fmt.Sprint((*p).Arg(setP())) })
	probe("p.PArg(f()) ptr recv", func() string { return fmt.Sprint(p.PArg(setP())) })
	probe("two(p.Val(), f())", func() string { return fmt.Sprint(two(p.Val(), setP())) })
	probe("iface.M(f())", func() string { return fmt.Sprint(iface.M(setIface())) })
	probe("fv()+f() fv read", func() string { return fmt.Sprint(fv() + setFv()) })
	probe("fvArg(fv, f())", func() string { return fmt.Sprint(callWith(fv, setFv())) })
	probe("fv2(f()) callee var", func() string { return fmt.Sprint(gv(setGv())) })
	probe("variadic vs(x, f())", func() string { return fmt.Sprint(vs(x, setX(10))) })
	probe("method value p.Val, f()", func() string { return fmt.Sprint(callMV(p.Val, setP())) })
	// Index, slice, selector and dereference.
	probe("s[0] + f() (new s)", func() string { return fmt.Sprint(s[0] + setS()) })
	probe("s[0] + f() (s[0]=77)", func() string { return fmt.Sprint(s[0] + setS0()) })
	probe("s[i] + f() (i=2)", func() string { return fmt.Sprint(s[i] + setI()) })
	probe("arr[0] + f()", func() string { return fmt.Sprint(arr[0] + setArr()) })
	probe("m[1] + f() (m[1]=99)", func() string { return fmt.Sprint(m[1] + setM()) })
	probe("m[1] + f() (new m)", func() string { return fmt.Sprint(m[1] + newM()) })
	probe("m[string(b)] + f()", func() string { ms := map[string]int{"ab": 1, "Qb": 2}; return fmt.Sprint(ms[string(b)] + setB()) })
	probe("p.a + f() (new p)", func() string { return fmt.Sprint(p.a + setP()) })
	probe("p.a + f() (p.a=33)", func() string { return fmt.Sprint(p.a + setPa()) })
	probe("(*p).b + f()", func() string { return fmt.Sprint((*p).b + setP()) })
	probe("s[0:1][0] + f()", func() string { return fmt.Sprint(s[0:1][0] + setS()) })
	probe("two(len(s[0:1]), f())", func() string { return fmt.Sprint(sl(s[0:2], setS())) })
	probe("str[0:1] + f()", func() string { return fmt.Sprint(str[0:1] + fmt.Sprint(setStr())) })
	probe("arr[0:1][0] + f()", func() string { return fmt.Sprint(arr[0:1][0] + setArr()) })
	// Builtins and conversions.
	probe("len(s) + f()", func() string { return fmt.Sprint(len(s) + lenS()) })
	probe("cap(s) + f()", func() string { return fmt.Sprint(cap(s) + lenS()) })
	probe("len(str) + f()", func() string { return fmt.Sprint(len(str) + setStr()) })
	probe("len(append(s,x)) + f()", func() string { return fmt.Sprint(len(append(s, x)) + setX(10)) })
	probe("s[0] + copy(s, ...)", func() string { return fmt.Sprint(s[0] + copy(s, []int{9})) })
	probe("s[0] + len(append(s[:0],9))", func() string { return fmt.Sprint(s[0] + len(append(s[:0], 9))) })
	probe("x + *new-ish()", func() string { return fmt.Sprint(x + *newX()) })
	probe("str + f()", func() string { return fmt.Sprint(str + fmt.Sprint(setStr())) })
	probe("str == f()", func() string { return fmt.Sprint(str == fmt.Sprint(setStr())) })
	probe("str[0] + f()", func() string { return fmt.Sprint(int(str[0]) + setStr()) })
	probe("string(b) + f()", func() string { return fmt.Sprint(string(b) + fmt.Sprint(setB())) })
	probe("[]byte(str)[0] + f()", func() string { return fmt.Sprint(int([]byte(str)[0]) + setStr()) })
	probe("any(x) == f()", func() string { return fmt.Sprint(any(x) == any(setX(1)+1)) })
	probe("eq(any(x), f())", func() string { return fmt.Sprint(eqa(any(x), setX(10))) })
	probe("*p == T{f()+40,41}", func() string { return fmt.Sprint(*p == T{setP() + 40, 41}) })
	probe("arr == [3]int{55+f(),2,3}", func() string { return fmt.Sprint(arr == [3]int{55 + setArr(), 2, 3}) })
	probe("ii.(int) + f()", func() string { var ii any = x; return fmt.Sprint(ii.(int) + setAny(&ii)) })
	probe("ii.(*T).a + f()", func() string { var ii any = p; return fmt.Sprint(ii.(*T).a + setAnyP(&ii)) })
	probe("ii.(I) == f()", func() string { var ii any = A{1}; return fmt.Sprint(ii.(I) == anyI(&ii)) })
	// Short circuit.
	probe("two(bint(x==1 && true), f())", func() string { return fmt.Sprint(two(bint(x == 1 && true), setX(10))) })
	probe("(x==1 || false) != f()", func() string { return fmt.Sprint((x == 1 || false) != boolX(10)) })
	probe("(x == 1) != f()", func() string { return fmt.Sprint((x == 1) != boolX(10)) })
	// Composite literals.
	probe("T{x, f()}", func() string { return fmt.Sprint(T{x, setX(10)}) })
	probe("[]int{x, f()}", func() string { return fmt.Sprint([]int{x, setX(10)}) })
	probe("[2]int{x, f()}", func() string { return fmt.Sprint([2]int{x, setX(10)}) })
	probe("&T{x, f()}", func() string { return fmt.Sprint(*&T{x, setX(10)}) })
	probe("map{1: x, 2: f()}", func() string { mm := map[int]int{1: x, 2: setX(10)}; return fmt.Sprint(mm[1], mm[2]) })
	probe("map{f(): 1, 2: x}", func() string { mm := map[int]int{setX(10): 1, 2: x}; return fmt.Sprint(mm[0], mm[2]) })
	probe("map{x: f()}", func() string { mm := map[int]int{x: setX(10)}; return fmt.Sprint(mm) })
	probe("map{k: 3, 1: 2} (k=1)", func() string { k := 1; mm := map[int]int{k: 3, 1: 2}; return fmt.Sprint(mm[1]) })
	probe("map{1: 2, k: 3} (k=1)", func() string { k := 1; mm := map[int]int{1: 2, k: 3}; return fmt.Sprint(mm[1]) })
	probe("map{k: 3, 1: T{2,2}}", func() string { k := 1; mm := map[int]T{k: {3, 3}, 1: {2, 2}}; return fmt.Sprint(mm[1]) })
	probe("map{k: 3, 1: v} dyn", func() string { k, v := 1, 2; mm := map[int]int{k: 3, 1: v}; return fmt.Sprint(mm[1]) })
	probe("map[any]{k: 3, 1: 2}", func() string { var k any = 1; mm := map[any]int{k: 3, 1: 2}; return fmt.Sprint(mm[1]) })
	probe("map[int]any{k: 3, 1: 2}", func() string { k := 1; mm := map[int]any{k: 3, 1: 2}; return fmt.Sprint(mm[1]) })
	probe("map[string]{k: 3, \"a\": 2}", func() string { k := "a"; mm := map[string]int{k: 3, "a": 2}; return fmt.Sprint(mm["a"]) })
	probe("map[[1]int]{k: 3, {1}: 2}", func() string { k := [1]int{1}; mm := map[[1]int]int{k: 3, {1}: 2}; return fmt.Sprint(mm[[1]int{1}]) })
	// Assignments.
	probe("sv[i] = f() (i=2)", func() string { s2 := []int{0, 0, 0}; sv = s2; sv[i] = setI(); return fmt.Sprint(s2) })
	probe("s[0] = f() (new s)", func() string { old := s; s[0] = setS(); return fmt.Sprint(old, s) })
	probe("p.a = f() (new p)", func() string { old := p; p.a = setP(); return fmt.Sprint(*old, *p) })
	probe("*p = T{f(), 9} (new p)", func() string { old := p; *p = T{setP(), 9}; return fmt.Sprint(*old, *p) })
	probe("arr[i] = f() (i=2)", func() string { arr[i] = 7 + setI(); return fmt.Sprint(arr) })
	probe("m[i] = f() (i=2)", func() string { m[i] = 7 + setI(); return fmt.Sprint(m) })
	probe("m[1] = f() (new m)", func() string { old := m; m[1] = 7 + newM(); return fmt.Sprint(old, m) })
	probe("s[0] += f() (new s)", func() string { old := s; s[0] += setS(); return fmt.Sprint(old, s) })
	probe("x += f()", func() string { x += setX(10); return fmt.Sprint(x) })
	probe("m[1] += f() (new m)", func() string { old := m; m[1] += newM(); return fmt.Sprint(old, m) })
	probe("sv[i] += f() (i=2)", func() string { s2 := []int{0, 0, 0}; sv = s2; sv[i] += 1 + setI(); return fmt.Sprint(s2) })
	probe("sv[i]++", func() string { s2 := []int{0, 0, 0}; sv = s2; sv[i]++; return fmt.Sprint(s2) })
	probe("r1, r2 = x, f()", func() string { var r1, r2 int; r1, r2 = x, setX(10); return fmt.Sprint(r1, r2) })
	probe("var a, c = x, f()", func() string { var a, c = x, setX(10); return fmt.Sprint(a, c) })
	probe("a, c = c, a", func() string { a, c := 1, 2; a, c = c, a; return fmt.Sprint(a, c) })
	probe("s2[j], j = 1, 2", func() string { s2 := []int{0, 0, 0}; j := 0; s2[j], j = 1, 2; return fmt.Sprint(s2, j) })
	probe("j, s2[j] = 1, 2", func() string { s2 := []int{0, 0, 0}; j := 0; j, s2[j] = 1, 2; return fmt.Sprint(s2, j) })
	probe("sv[i], r = pairI()", func() string { s2 := []int{0, 0, 0}; sv = s2; var r int; sv[i], r = pairI(); return fmt.Sprint(s2, r) })
	probe("sv[i], r = f(), g()", func() string { s2 := []int{0, 0, 0}; sv = s2; var r int; sv[i], r = 5, setI(); return fmt.Sprint(s2, r) })
	probe("s = append(s, x, f())", func() string { s = append(s, x, setX(10)); return fmt.Sprint(s) })
	probe("s = append(s[:1], f())", func() string { s = append(s[:1], setS()); return fmt.Sprint(s) })
	probe("v, ok := m[i] i=f", func() string { v, ok := m[i+setI()-2]; return fmt.Sprint(v, ok) })
	// Statements.
	probe("return x, f()", func() string { a, c := retXF(); return fmt.Sprint(a, c) })
	probe("return x+0, f() named", func() string { a, c := retNamed(); return fmt.Sprint(a, c) })
	probe("defer record(x, f())", func() string { deferProbe(); return fmt.Sprint(out) })
	probe("defer recordM(iface, f())", func() string { deferIface(); return fmt.Sprint(out) })
	probe("defer iface.M(f())", func() string { deferIfaceCall(); return fmt.Sprint(out) })
	probe("defer fv-call(f())", func() string { deferFv(); return fmt.Sprint(out) })
	probe("switch x {case f()+1}", func() string { return switchProbe() })
	probe("switch x+f() {}", func() string { return switchProbe2() })
	probe("if x == f()+10", func() string {
		if x == setX(10)+10 {
			return "eq"
		}
		return "ne"
	})
	probe("for range s[:f()+1]", func() string {
		r := 0
		for _, v := range s[:setS()+1] {
			r += v
		}
		return fmt.Sprint(r)
	})
	probe("closure c + set()", func() string { c := 1; set := func() int { c = 10; return 0 }; return fmt.Sprint(c + set()) })
	probe("closure two(c, set())", func() string { c := 1; set := func() int { c = 10; return 0 }; return fmt.Sprint(two(c, set())) })
	probe("c + setPtr(&c)", func() string { c := 1; q := &c; return fmt.Sprint(c + setPtr(q)) })
	probe("c + setPtr(&c) direct", func() string { c := 1; return fmt.Sprint(c + setPtr(&c)) })
	// Traps against calls (which one wins).
	probe("z[5] = panic()", func() string { var z []int; z[5] = mustPanic("rhs"); return "" })
	probe("z[5] + panic()", func() string { var z []int; return fmt.Sprint(z[5] + mustPanic("call")) })
	probe("z[f()] = panic()", func() string { var z []int; z[id(5)] = mustPanic("rhs"); return "" })
	probe("q.a + panic() (nil q)", func() string { var q *T; return fmt.Sprint(q.a + mustPanic("call")) })
	probe("q.a = panic() (nil q)", func() string { var q *T; q.a = mustPanic("rhs"); return "" })
	probe("x/z + panic() (z=0)", func() string { z := 0; return fmt.Sprint(x/z + mustPanic("call")) })
	probe("nilmap[1] = panic()", func() string { var mm map[int]int; mm[1] = mustPanic("rhs"); return "" })
	probe("nilmap[1] += panic()", func() string { var mm map[int]int; mm[1] += mustPanic("rhs"); return "" })
	probe("nilfv(panic())", func() string { var g func(int) int; return fmt.Sprint(g(mustPanic("arg"))) })
	probe("niliface.M(panic())", func() string { var ii I; return fmt.Sprint(ii.M(mustPanic("arg"))) })
	probe("ii.(string) + panic()", func() string { var ii any = 1; return fmt.Sprint(ii.(string) + fmt.Sprint(mustPanic("call"))) })
	probe("ii.(*T).a + panic()", func() string { var ii any = 1; return fmt.Sprint(ii.(*T).a + mustPanic("call")) })
	probe("z[5:9] + panic()", func() string { var z []int; return fmt.Sprint(len(z[5:9]) + mustPanic("call")) })
	probe("x << -z + panic()", func() string { z := 1; return fmt.Sprint(x<<-z + mustPanic("call")) })
	probe("z[0], r = panic(), 1", func() string { var z []int; var r int; z[0], r = mustPanic("rhs"), 1; return fmt.Sprint(r) })
	probe("two(z[5], panic())", func() string { var z []int; return fmt.Sprint(two(z[5], mustPanic("arg"))) })
	probe("make neg + panic()", func() string { n := -1; return fmt.Sprint(len(make([]int, n)) + mustPanic("call")) })
	probe("str[9] + panic()", func() string { return fmt.Sprint(int(str[9]) + mustPanic("call")) })
	probe("(*q).Val() vs panic() arg", func() string { var q *T; return fmt.Sprint(q.Arg(mustPanic("arg"))) })
	probe("defer q.Arg(panic()) nil q", func() string { deferNilRecv(); return "" })
	probe("anyK[k] + panic()", func() string { var k any = []int{}; mk := map[any]int{}; return fmt.Sprint(mk[k] + mustPanic("call")) })
	// Map literals: which entries gc inserts first (static: constant keys
	// and values, no interface conversion), seen through a duplicate key.
	probe("map[int]any{k: 3, 1: nil}", func() string { k := 1; mm := map[int]any{k: 3, 1: nil}; return fmt.Sprint(mm[1]) })
	probe("map[int]any{k: 3, 1: (*T)(nil)}", func() string { k := 1; mm := map[int]any{k: 3, 1: (*T)(nil)}; return fmt.Sprint(mm[1]) })
	probe("map[int]any{k: 3, 1: \"s\"}", func() string { k := 1; mm := map[int]any{k: 3, 1: "s"}; return fmt.Sprint(mm[1]) })
	probe("map[int]S{k:, 1: {A: 1}} iface", func() string { k := 1; mm := map[int]S{k: {B: 3}, 1: {A: 1}}; return fmt.Sprint(mm[1].B) })
	probe("map[int]S{k:, 1: {B: 1}}", func() string { k := 1; mm := map[int]S{k: {B: 3}, 1: {B: 1}}; return fmt.Sprint(mm[1].B) })
	probe("map[int][]int{k: {3}, 1: nil}", func() string { k := 1; mm := map[int][]int{k: {3}, 1: nil}; return fmt.Sprint(mm[1]) })
	probe("map[int][]int{k: nil, 1: {1}}", func() string { k := 1; mm := map[int][]int{k: nil, 1: {1}}; return fmt.Sprint(mm[1]) })
	probe("map[int]*T{k: p, 1: nil}", func() string { k := 1; mm := map[int]*T{k: p, 1: nil}; return fmt.Sprint(mm[1] == nil) })
	probe("map[int]*T{k: nil, 1: {}}", func() string { k := 1; mm := map[int]*T{k: nil, 1: {}}; return fmt.Sprint(mm[1] == nil) })
	probe("map[int]func(){k: f, 1: nil}", func() string { k := 1; mm := map[int]func(){k: func() {}, 1: nil}; return fmt.Sprint(mm[1] == nil) })
	probe("map[int]map[int]int{k:{}, 1: nil}", func() string { k := 1; mm := map[int]map[int]int{k: {}, 1: nil}; return fmt.Sprint(mm[1] == nil) })
	probe("map[int]Named{k: 3, 1: Cn}", func() string { k := 1; mm := map[int]Named{k: 3, 1: Cn}; return fmt.Sprint(mm[1]) })
	probe("map[int]int{k: 3, 1: 1<<2}", func() string { k := 1; mm := map[int]int{k: 3, 1: 1 << 2}; return fmt.Sprint(mm[1]) })
	probe("map[int]int{k: 3, 1: -2}", func() string { k := 1; mm := map[int]int{k: 3, 1: -2}; return fmt.Sprint(mm[1]) })
	probe("map[int]bool{k: false, 1: true}", func() string { k := 1; mm := map[int]bool{k: false, 1: true}; return fmt.Sprint(mm[1]) })
	probe("map[int][2]string{k:, 1: {a,b}}", func() string { k := 1; mm := map[int][2]string{k: {"x"}, 1: {"a", "b"}}; return fmt.Sprint(mm[1]) })
	probe("map[S2]int{k: 3, {1,2}: 2}", func() string { k := S2{1, 2}; mm := map[S2]int{k: 3, {1, 2}: 2}; return fmt.Sprint(mm[S2{1, 2}]) })
	probe("map[int]int{k: 3, len(\"a\"): 2}", func() string { k := 1; mm := map[int]int{k: 3, len("a"): 2}; return fmt.Sprint(mm[1]) })
	probe("map[int]int{k: 3, 1: 2, k2: 4}", func() string { k, k2 := 1, 1; mm := map[int]int{k: 3, 1: 2, k2: 4}; return fmt.Sprint(mm[1]) })
	probe("map[int]int{k: 3, 1: 2}[1]", func() string { k := 1; return fmt.Sprint(map[int]int{k: 3, 1: 2}[1]) })
	probe("[]map{{k: 3, 1: 2}}", func() string { k := 1; ms := []map[int]int{{k: 3, 1: 2}}; return fmt.Sprint(ms[0][1]) })
	// m[k] = append(m[k], f()): gc's map-append special case.
	probe("pm.m[i] = append(pm.m[i], f())", func() string { pm = &PM{m: map[int][]int{0: {1}, 2: {2}}}; old := pm; pm.m[i] = append(pm.m[i], setPMI()); return fmt.Sprint(old.m, pm.m) })
	probe("gm[i] = append(gm[i], f())", func() string { gm = map[int][]int{0: {1}, 2: {2}}; gm[i] = append(gm[i], setGMI()); return fmt.Sprint(gm) })
	probe("gm[i] = append(gm[i], f()) new", func() string { gm = map[int][]int{0: {1}, 2: {2}}; old := gm; gm[i] = append(gm[i], newGM()); return fmt.Sprint(old, gm) })
	probe("pm.n[i] += f()", func() string { pm = &PM{n: map[int]int{0: 1, 2: 2}}; old := pm; pm.n[i] += setPMI(); return fmt.Sprint(old.n, pm.n) })
	probe("pm.n[i] = f()", func() string { pm = &PM{n: map[int]int{0: 1, 2: 2}}; old := pm; pm.n[i] = 5 + setPMI(); return fmt.Sprint(old.n, pm.n) })
	// Receivers and selectors.
	probe("p.Val() + f()", func() string { return fmt.Sprint(p.Val() + setP()) })
	probe("two(gt.a, gt.Arg(f()))", func() string { gt = T{1, 2}; return fmt.Sprint(two(gt.a, gt.Arg(setGT()))) })
	probe("gt.Arg(f()) value recv global", func() string { gt = T{1, 2}; return fmt.Sprint(gt.Arg(setGT())) })
	probe("gt.PArg(f()) ptr recv global", func() string { gt = T{1, 2}; return fmt.Sprint(gt.PArg(setGT())) })
	probe("garr[i] + f()", func() string { garr = [3]int{1, 2, 3}; return fmt.Sprint(garr[i] + setI()) })
	probe("s[i] = s[i] + f()", func() string { s[i] = s[i] + setI(); return fmt.Sprint(s) })
	probe("two(s[id(0)+f()], s[0])", func() string { return fmt.Sprint(two(s[id(0)+setS()], s[0])) })
	probe("str + str2 + f()", func() string { str2 := "c"; return fmt.Sprint(str + str2 + fmt.Sprint(setStr())) })
	probe("x == 1 && f() && x == 10", func() string { return fmt.Sprint(x == 1 && boolX(10) && x == 10) })
	probe("f() && x == 10", func() string { return fmt.Sprint(boolX(10) && x == 10) })
	probe("two(x, bint(f() && true))", func() string { return fmt.Sprint(two(x, bint(boolX(10) && true))) })
	probe("closure two(c, inc()), c", func() string { c := 1; inc := func() int { c++; return c }; return fmt.Sprint(two(c, inc()), c) })
	probe("two(*q, f()) local ptr", func() string { c := 1; q := &c; return fmt.Sprint(two(*q, setPtr(q))) })
	probe("two(a[0], f(a[:])) local array", func() string { a := [2]int{1, 2}; sl2 := a[:]; return fmt.Sprint(two(a[0], setSl(sl2))) })
	probe("eqArr(a, f(a[:])) local array", func() string { a := [2]int{1, 2}; sl2 := a[:]; return fmt.Sprint(eqArr(a, setSl(sl2))) })
	probe("two(t.a, t.Set()) ptr method", func() string { t := T{1, 2}; return fmt.Sprint(two(t.a, t.Set())) })
	// Map keys: gc's slow-key maps (keys that are not 4- or 8-byte memory
	// or strings, or elements above 128 bytes) copy a non-addressable key
	// where it appears.
	probe("m8[x8+0] + f()", func() string { m8 := map[uint8]int{1: 1, 10: 10}; return fmt.Sprint(m8[x8+0] + setX8()) })
	probe("m8[x8] + f()", func() string { m8 := map[uint8]int{1: 1, 10: 10}; return fmt.Sprint(m8[x8] + setX8()) })
	probe("m64[x64+0] + f()", func() string { m64 := map[uint64]int{1: 1, 10: 10}; return fmt.Sprint(m64[x64+0] + setX64()) })
	probe("mi[x+0] + f()", func() string { mi := map[int]int{1: 1, 10: 10}; return fmt.Sprint(mi[x+0] + setX(10)) })
	probe("m16[uint16(x)] + f()", func() string { m16 := map[uint16]int{1: 1, 10: 10}; return fmt.Sprint(m16[uint16(x)] + setX(10)) })
	probe("m32[int32(x)] + f()", func() string { m32 := map[int32]int{1: 1, 10: 10}; return fmt.Sprint(m32[int32(x)] + setX(10)) })
	probe("mb[x == 1] + f()", func() string { mb := map[bool]int{true: 1, false: 2}; return fmt.Sprint(mb[x == 1] + setX(10)) })
	probe("ms2[S2{x, 0}] + f()", func() string { ms2 := map[S2]int{{1, 0}: 1, {10, 0}: 10}; return fmt.Sprint(ms2[S2{x, 0}] + setX(10)) })
	probe("ms3[S3{x8}] + f() (1-byte)", func() string { ms3 := map[S3]int{{1}: 1, {10}: 10}; return fmt.Sprint(ms3[S3{x8}] + setX8()) })
	probe("ma[x] + f() (any key)", func() string { ma := map[any]int{1: 1, 10: 10}; return fmt.Sprint(ma[x] + setX(10)) })
	probe("mstr[str+\"\"] + f()", func() string { mstr := map[string]int{"ab": 1, "zz": 2}; return fmt.Sprint(mstr[str+""] + setStr()) })
	probe("mbig[x+0] + f() (elem>128)", func() string { mbig := map[int][17]int{1: {1}, 10: {10}}; return fmt.Sprint(mbig[x+0][0] + setX(10)) })
	probe("mbig[x] + f() (elem>128)", func() string { mbig := map[int][17]int{1: {1}, 10: {10}}; return fmt.Sprint(mbig[x][0] + setX(10)) })
	probe("m8[x8+0] = f()", func() string { m8 := map[uint8]int{}; m8[x8+0] = 5 + setX8(); return fmt.Sprint(m8) })
	probe("m8[x8+0] += f()", func() string { m8 := map[uint8]int{}; m8[x8+0] += 5 + setX8(); return fmt.Sprint(m8) })
	probe("mi[x+0] = f()", func() string { mi := map[int]int{}; mi[x+0] = 5 + setX(10); return fmt.Sprint(mi) })
	probe("map[uint8]int{x8+0: f()}", func() string { m8 := map[uint8]int{x8 + 0: setX8()}; return fmt.Sprint(m8) })
	probe("map[int]int{x+0: f()}", func() string { mi := map[int]int{x + 0: setX(10)}; return fmt.Sprint(mi) })
	probe("map[any]int{x: f()}", func() string { ma := map[any]int{x: setX(10)}; return fmt.Sprint(ma) })
	probe("v, ok := m8[x8+0+f()]", func() string { m8 := map[uint8]int{1: 1, 10: 10}; v, ok := m8[x8+0+uint8(setX8())]; return fmt.Sprint(v, ok) })
	probe("two(m8[x8+0], f())", func() string { m8 := map[uint8]int{1: 1, 10: 10}; return fmt.Sprint(two(m8[x8+0], setX8())) })
	probe("gm8[x8+0] = append(gm8[x8+0], f())", func() string { gm8 = map[uint8][]int{1: {1}, 10: {10}}; gm8[x8+0] = append(gm8[x8+0], setX8()); return fmt.Sprint(gm8) })
	probe("gm64[x64] = append(gm64[x64], f())", func() string { gm64 = map[uint64][]int{1: {1}, 10: {10}}; gm64[x64] = append(gm64[x64], setX64()); return fmt.Sprint(gm64) })
	probe("gms[str] = append(gms[str], f())", func() string { gms = map[string][]int{"ab": {1}, "zz": {2}}; gms[str] = append(gms[str], setStr()); return fmt.Sprint(gms) })
	probe("gmn[ns] = append(gmn[ns], f())", func() string { gmn = map[NS][]int{"ab": {1}, "zz": {2}}; ns = "ab"; gmn[ns] = append(gmn[ns], setNS()); return fmt.Sprint(gmn) })
	probe("gm8[x8] = append(gm8[x8], f())", func() string { gm8 = map[uint8][]int{1: {1}, 10: {10}}; gm8[x8] = append(gm8[x8], setX8()); return fmt.Sprint(gm8) })
	// Conversions to interfaces: gc copies a non-addressable operand of a
	// type its runtime converts by address where it appears.
	probe("eqa(x8+0, f())", func() string { return fmt.Sprint(eqa(x8+0, setX8())) })
	probe("eqa(x8, f())", func() string { return fmt.Sprint(eqa(x8, setX8())) })
	probe("eqa(x+0, f())", func() string { return fmt.Sprint(eqa(x+0, setX(10))) })
	probe("eqa(uint16(x)+0, f())", func() string { return fmt.Sprint(eqa(uint16(x)+0, setX(10))) })
	probe("eqa(uint32(x)+0, f())", func() string { return fmt.Sprint(eqa(uint32(x)+0, setX(10))) })
	probe("eqa(x == 1, f())", func() string { return fmt.Sprint(eqa(x == 1, setX(10))) })
	probe("eqa(T{x, 0}, f())", func() string { return fmt.Sprint(eqa(T{x, 0}, setX(10))) })
	probe("eqa(gt, f())", func() string { gt = T{1, 2}; return fmt.Sprint(eqa(gt, setGT())) })
	probe("eqa(&s[x-1], f()) ptr", func() string { return fmt.Sprint(eqa(&s[x-1], setX(2)) == any(&s[0])) })
	probe("eqa(str+\"\", f())", func() string { return fmt.Sprint(eqa(str+"", setStr())) })
	probe("eqa(s[0:1]...) slice", func() string { return fmt.Sprint(eqa(sv2(s), setS())) })
	probe("eqa(any(x8+0), f()) explicit", func() string { return fmt.Sprint(eqa(any(x8+0), setX8())) })
	probe("eqa(m8x[x8], f()) map read", func() string { m8x := map[uint8]uint8{1: 1, 10: 10}; return fmt.Sprint(eqa(m8x[x8], setX8())) })
	probe("ia == x8+0 vs f()", func() string { var ia any = uint8(1); return fmt.Sprint(two(bint(ia == x8+0), setX8())) })
	probe("eqa(S3{x8}, f())", func() string { return fmt.Sprint(eqa(S3{x8}, setX8())) })
	probe("eqa([1]int{x}, f())", func() string { return fmt.Sprint(eqa([1]int{x}, setX(10))) })
	probe("eqa(struct{a int}{x}, f())", func() string { return fmt.Sprint(eqa(struct{ a int }{x}, setX(10))) })
	probe("gmp[gp] = append(gmp[gp], f())", func() string { t1 := &T{}; gp = t1; gmp = map[*T][]int{t1: {1}}; gmp[gp] = append(gmp[gp], setGP()); return fmt.Sprint(len(gmp[t1]), len(gmp[gp])) })
}

func callWith(f func() int, _ int) int { return f() }

var gv = func(v int) int { return 1 }

func setGv() int                     { gv = func(v int) int { return 2 }; return 0 }
func vs(a ...int) []int              { return a }
func sl(a []int, _ int) []int        { return a }
func callMV(f func() int, _ int) int { return f() }
func eqa(a any, _ int) any           { return a }
func pairI() (int, int)              { i = 2; return 5, 6 }
func retXF() (int, int)              { return x, setX(10) }
func retNamed() (r1, r2 int)         { return x + 0, setX(10) }
func setPtr(q *int) int              { *q = 10; return 0 }
func newX() *int                     { x = 10; return new(int) }
func setAny(ii *any) int             { *ii = 10; return 0 }
func setAnyP(ii *any) int            { *ii = &T{40, 41}; return 0 }
func anyI(ii *any) I                 { *ii = B{2}; return B{2} }
func mustPanic(s string) int         { panic(s) }
func record(a, b int)                { out = append(out, a, b) }
func recordM(ii I, _ int)            { out = append(out, ii.M(0)) }
func deferProbe() {
	defer record(x, setX(10))
}
func deferIface() {
	defer recordM(iface, setIface())
}
func deferIfaceCall() {
	defer func() { out = append(out, x) }()
	defer iface.M(setXIface())
}
func setXIface() int { iface = B{7}; return 0 }
func deferFv() {
	defer fvr(setFv())
}
func fvr(v int) { out = append(out, fv()) }
func deferNilRecv() {
	var q *T
	defer q.Arg(mustPanic("arg"))
}
func switchProbe() string {
	switch x {
	case setX(10) + 1:
		return "case f()+1"
	case 10:
		return "case 10"
	}
	return "none"
}
func switchProbe2() string {
	switch x + setX(10) {
	case 1:
		return "1"
	case 10:
		return "10"
	}
	return "none"
}
