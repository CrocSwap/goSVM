package compiler

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"go/ast"
	"strings"
	"testing"
)

// Check-level tables, the backend view and C-level properties of struct and
// array comparison, type aliases, arrays of composite elements and panic
// (docs/GO_TYPES.md, docs/GO_ARRAYS.md, docs/ERROR_CONTRACT.md#panic).

const valuePrefix = `package p
type P struct{ X, Y int16 }
type Q struct {
	A uint8
	_ uint32
	S string
	I any
	R *P
	N [2]P
}
type Node struct {
	Kids [2]*Node
	Back *[2]Node
	N    uint64
}
type Grid [3][4]uint8
type Int [4]uint64
func (z *Int) Inc() { z[0]++ }
type Pair[T any] struct{ A, B T }
type Wrap[T any] struct{ Items [2]T }
func First[T any](a [3]T) T { return a[0] }
type Word = uint64
type PP = P
type Words = [4]Word
type M = map[Word]PP
type F = func(Word) Word
type IP = Pair[int8]
type Ptr = *P
type Iface = interface{ M() }
type E = error
func (p PP) Sum() int16 { return p.X + p.Y }
type Code uint16
type Label string
type Fault struct{}
func (Fault) Error() string { return "fault" }
type Str uint8
func (Str) String() string { return "s" }
func Process(s, i []byte) uint64 {
`

func TestValueTypesAcceptedForms(t *testing.T) {
	for name, body := range map[string]string{
		"struct equality":         `a, b := P{1, 2}, P{X: int16(i[0])}; if a == b || a != b { s[0] = 1 }`,
		"struct with every kind":  `x := P{}; a := Q{A: 1, S: "x", I: 3, R: &x}; b := a; b.N[1].Y = 2; if a == b { s[0] = 1 }`,
		"struct literal operand":  `p := P{1, 2}; if p == (P{1, 2}) { s[0] = 1 }; s[1] = byte(len(fmt(p == P{})))`,
		"empty struct equality":   `var a, b struct{}; if a == b { s[0] = 1 }`,
		"struct switch":           `p := P{X: int16(i[0])}; switch p { case P{}: s[0] = 1; case P{1, 0}, P{0, 1}: s[0] = 2 }`,
		"struct in interface":     `var a, b any = P{1, 2}, Q{}; if a == b || a == (P{1, 2}) { s[0] = 1 }`,
		"array of structs":        `var a [4]P; a[i[0]%4].X = 3; a[1].Y++; a[2].X += 4; p := &a[3]; p.Y = 5; b := a; if a == b { s[0] = byte(a[0].X) }`,
		"array literals":          `a := [...]P{2: {X: 1}, 0: {Y: -1}}; b := [2][2]P{{{1, 2}}, {}}; s[0] = byte(len(a) + int(b[0][0].Y))`,
		"array of arrays":         `var g Grid; g[1][2] = 3; g[i[0]%3][i[1]%4]++; r := g[1]; r[0] = 9; g[2] = r; for c := range g[i[2]] { s[c] = g[2][c] }`,
		"array of pointers":       `x := P{}; a := [2]*P{&x, nil}; a[0].X = 1; n := Node{}; n.Kids[0] = &Node{N: 2}; s[0] = byte(x.X) + byte(n.Kids[0].N)`,
		"pointer to own array":    `var kids [2]Node; n := Node{Back: &kids}; n.Back[1].N = 3; s[0] = byte(kids[1].N)`,
		"array of slices":         `a := [2][]byte{i[:2], nil}; a[1] = append(a[1], 1); s[0] = byte(len(a[0]) + len(a[1]))`,
		"array of maps":           `a := [2]map[int]int{{1: 2}}; s[0] = byte(a[0][1] + a[1][1])`,
		"array of strings":        `a := [2]string{"a", string(i[:1])}; if a == [2]string{"a", "b"} { s[0] = 1 }`,
		"array method elements":   `var a [2]Int; a[1].Inc(); a[0] = a[1]; if a[0] == a[1] { s[0] = byte(a[0][0]) }`,
		"range arrays of structs": `var a [3]P; for j, p := range a { a[j].X = p.Y + 1 }; for _, p := range &a { s[0] += byte(p.X) }`,
		"slices of arrays":        `rows := make([][2]P, 2); rows[1][0].X = 3; v := rows[0][:]; v[1].Y = 2; s[0] = byte(rows[1][0].X)`,
		"views of arrays":         `var a [4]P; v := a[1:3]; v[0].X = 7; s[0] = byte(a[1].X)`,
		"map keys and values":     `m := map[[2]P]int{}; m[[2]P{{1, 2}}] = 3; n := map[int][2]P{1: {{X: 4}}}; s[0] = byte(m[[2]P{{1, 2}}]) + byte(n[1][0].X)`,
		"generic arrays":          `w := Wrap[P]{}; w.Items[1].X = 3; s[0] = byte(First([3]P{{X: 4}}).X + w.Items[1].X)`,
		"generic array elements":  `var a [2]Pair[uint8]; a[1].A = 4; if a[0] != a[1] { s[0] = a[1].A }`,
		"struct parameters":       `s[0] = byte(swap([2]P{{X: 1}})[1].X)`,
		"elided pointer elements": `a := [2]*P{{1, 2}, nil}; s[0] = byte(a[0].Y)`,
		"element field addresses": `var a [2]P; p := &a[1].X; *p = 3; var g Grid; v := g[1][:]; v[2] = 4; s[0] = byte(a[1].X) + g[1][2]`,
		"pointer to struct array": `var a [3]P; pa := &a; pa[1].X = 2; pa[2].Y++; q := &pa[0]; q.X = 1; s[0] = byte(a[1].X + a[2].Y + a[0].X)`,
		"empty struct arrays":     `var a [4]struct{}; b := a; if a == b { s[0] = byte(len(a)) }`,
		"arrays of rich structs":  `var a, b [2]Q; a[1].I = 3; a[0].S = "x"; m := map[[2]Q]int{a: 1}; var x any = a; if a == b || x == b || m[b] == 1 { s[0] = 1 }`,
		"aliases":                 `var w Word = 3; p := PP{1, 2}; var ws Words; ws[0] = w; m := M{w: p}; var f F = func(x Word) Word { return x }; ip := IP{A: -1}; var q Ptr = &p; s[0] = byte(ws[0] + f(w)) + byte(m[3].Sum()) + byte(ip.A) + byte(q.X)`,
		"alias interfaces":        `var e E; var x Iface; if e == nil && x == nil { s[0] = 1 }`,
		"panic string":            `if i[0] == 0 { panic("boom") }; panic(string(i[:2]))`,
		"panic values":            `switch i[0] { case 0: panic(int8(-1)); case 1: panic(Code(3)); case 2: panic(Label("x")); case 3: panic(i[1] > 2); case 4: panic(P{}); case 5: panic(nil); case 6: var e error; panic(e); case 7: panic(Fault{}); case 8: panic(Str(1)); case 9: panic(uint64(1) << 63); case 10: panic(1) }`,
		"panic in closure":        `f := func() { panic("x") }; if i[0] == 0 { f() }`,
		"panic in deferred call":  `defer func() { if i[0] == 1 { panic("deferred") } }()`,
		"panic in generic code":   `s[0] = byte(must(i[0]))`,
		"panic as last statement": `if i[0] == 0 { return 1 }; panic("unreachable")`,
	} {
		t.Run(name, func(t *testing.T) {
			source := valuePrefix + body + "\nreturn 0 }\n" + valueSuffix
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

const valueSuffix = `
func swap(a [2]P) [2]P { a[0], a[1] = a[1], a[0]; return a }
func fmt(b bool) []byte { if b { return []byte{1} }; return nil }
func must[T comparable](v T) T { var zero T; if v == zero { panic("zero") }; return v }
`

func TestValueTypesRejections(t *testing.T) {
	cases := map[string][2]string{
		"recover":          {`defer func() { _ = recover() }()`, recoverRule},
		"defer panic":      {`defer panic("x")`, "defer panic(...) unsupported; defer a function literal that panics"},
		"defer recover":    {`defer recover()`, recoverRule},
		"array too large":  {`var a [40]Int; _ = a`, "array storage exceeds experimental 1024-byte per-value limit"},
		"nested too large": {`var a [9][16]uint64; _ = a`, "array storage exceeds experimental 1024-byte per-value limit"},
		"unnamed struct":   {`var a [2]struct{ X uint8 }; _ = a`, "unsupported type struct{X uint8}"},
		"float panic":      {`panic(1.5)`, "unsupported type float64"},
		"print":            {`print(1)`, builtinRule},
		"range index call": {`var g Grid; for c := range g[f()] { _ = c }`, "index-only range over an array requires a variable, field, dereference or call"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			source := valuePrefix + tc[0] + "\nreturn 0 }\nfunc f() int { return 0 }\n"
			if got := allocMessage(t, source); got != tc[1] {
				t.Fatalf("got %q, want %q", got, tc[1])
			}
		})
	}
	// Arrays of Context stay rejected.
	source := "package p\nimport \"gosvm/solana\"\ntype K [2]solana.Context\nfunc Process(c solana.Context) uint64 { return 0 }\n"
	if _, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "k.go", Data: []byte(source)}}}}}); err == nil || !strings.HasSuffix(err.Error(), "arrays of Context unsupported") {
		t.Fatalf("got %v", err)
	}
}

// Aliases of types in other packages (as in type Uint160 = uint256.Int),
// of generic instantiations and through chains of aliases behave as the
// aliased types: methods, map keys, interfaces and comparisons.
func TestCrossPackageAliases(t *testing.T) {
	wide := `package wide
type Int [4]uint64
func (z *Int) Add(x, y *Int) *Int { var c uint64; for j := range z { s := x[j] + y[j] + c; c = 0; if s < x[j] || (s == x[j] && y[j]+c != 0 && c != 0) { c = 1 }; z[j] = s }; return z }
func (z *Int) Eq(x *Int) bool { return *z == *x }
type U128 struct{ Lo, Hi uint64 }
func (u U128) IsZero() bool { return u == U128{} }
type Pair[T any] struct{ A, B T }
type Uint = Int
`
	app := `package app
import "wide"
type (
	Uint256 = wide.Int
	Uint160 = wide.Uint
	U = wide.U128
	PW = wide.Pair[Uint256]
	Shelf = [3]Uint160
)
type Named Uint160
func (n Named) Low() uint64 { return n[0] }
type Zeroer interface{ IsZero() bool }
func Process(s, i []byte) uint64 {
	var a, b Uint160
	a[0], b[0] = uint64(i[0]), uint64(i[1])
	var c Uint256
	c.Add(&a, &b)
	var sh Shelf
	sh[1] = c
	pw := PW{A: c}
	m := map[Uint160]U{c: {Lo: 7}}
	var z Zeroer = m[c]
	n := Named(c)
	s[0] = byte(c[0])
	s[1] = byte(m[c].Lo)
	if z.IsZero() { s[2] = 1 }
	if sh[1].Eq(&pw.A) { s[3] = 1 }
	if pw.A == c && (U{}).IsZero() { s[4] = 1 }
	s[5] = byte(n.Low())
	return 6
}
`
	program := &Program{Entry: "app", Packages: []PackageSources{
		{"app", []Source{{Name: "app/program.go", Data: []byte(app)}}},
		{"wide", []Source{{Name: "wide/wide.go", Data: []byte(wide)}}},
	}}
	c, err := CompileProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, valueTypesDriver, false)
	in := make([]byte, 128)
	in[0], in[1] = 5, 9
	got := runHost(t, bin, fmt.Sprintf("%x\n", in))
	want := make([]byte, 1024)
	copy(want, []byte{14, 7, 0, 1, 1, 14})
	if got != fmt.Sprintf("6:%x\n", want) {
		t.Fatalf("got %q", got)
	}
}

// Arrays of pointers keep Go's escape rules: a local whose address is
// stored into an array literal or element moves to the heap.
func TestCompositeArrayEscapes(t *testing.T) {
	source := `package p
type P struct{ X uint8 }
func f() [2]*P { x := P{1}; return [2]*P{&x, nil} }
func g() [2]*P { var a [2]*P; y := P{2}; a[1] = &y; return a }
func h() uint8 { z := P{3}; a := [1]P{z}; return a[0].X }
func Process(s, i []byte) uint64 { s[0] = f()[0].X + g()[1].X + h(); return 0 }
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "e.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	moved := map[string]int{}
	for _, fn := range checked.Functions {
		moved[fn.Object.Name()] = len(fn.HeapVars)
	}
	if moved["f"] != 1 || moved["g"] != 1 || moved["h"] != 0 {
		t.Fatalf("heap moves: %v", moved)
	}
}

// Check records every panic call's log line; the backend view is complete
// and the C is deterministic. Programs without panic get no panic runtime.
func TestPanicSites(t *testing.T) {
	source := valuePrefix + `switch i[0] {
	case 0: panic("boom")
	case 1: panic(string(i[:2]))
	case 2: panic(int8(i[1]))
	case 3: panic(Code(i[1]))
	case 4: panic(Label("x"))
	case 5: panic(i[1] > 2)
	case 6: panic(P{})
	case 7: panic(nil)
	case 8: var e error; panic(e)
	case 9: panic(Fault{})
	case 10: panic(Str(1))
	case 11: panic(-7)
	case 12: panic(Label(string(i[:1])))
	}
	return 0 }` + valueSuffix
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	ast.Inspect(checked.Files[0], func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if site := checked.Panics[call]; site != nil {
				got = append(got, fmt.Sprintf("%d|%s|%s|%s", site.Value, site.Text, site.Prefix, site.Suffix))
			}
		}
		return true
	})
	// Constant and opaque lines are Text (1); strings, signed, unsigned
	// and bool values are formatted at run time (2-5); interface values
	// log Text unless nil (6). Panics in instances of generic code are
	// keyed by the instance's own nodes (its Decl), so must's is not here.
	want := []string{
		"1|panic: boom||",
		"2||panic: |",
		"3||panic: |",
		"4||panic: p.Code(|)",
		"1|panic: p.Label(\"x\")||",
		"5||panic: |",
		"1|panic: (p.P)||",
		"1|panic: panic called with nil argument||",
		"6|panic: (error)||",
		"1|panic: (p.Fault)||",
		"1|panic: (p.Str)||",
		"1|panic: -7||",
		"2||panic: p.Label(\"|\")",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("panic sites:\n%s", strings.Join(got, "\n"))
	}
	a, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "p.go", Data: []byte(source)}}}}})
	if !bytes.Equal(a, b) || !bytes.Contains(a, []byte("gosvm_panic(")) {
		t.Fatal("panic C is not deterministic or has no panic call")
	}
	plain, err := Compile("q.go", []byte("package p\nfunc Process(s, i []byte) uint64 { return 0 }\n"))
	if err != nil || bytes.Contains(plain, []byte("gosvm_panic")) || bytes.Contains(plain, []byte("sol_log_")) {
		t.Fatalf("a program without panic has the panic runtime: %v", err)
	}
}

// A panic logs one line, at most 512 bytes, with invalid UTF-8 replaced,
// then aborts; deferred calls do not run (Go would run them, but the
// transaction fails and rolls back either way).
func TestPanicLogLine(t *testing.T) {
	source := `package p
func Process(s, i []byte) uint64 {
	defer func() { s[1] = 9 }()
	s[0] = 1
	b := make([]byte, int(i[1])*4)
	for j := range b { b[j] = 'a' + byte(j%26) }
	if len(b) > 3 { b[1] = 0xff; b[2] = 0xe2; b[3] = 0x82 }
	switch i[0] {
	case 0: panic(string(b))
	case 1: panic(int64(-9223372036854775807 - 1))
	case 2: panic(uint64(18446744073709551615))
	}
	return 2
}
`
	c, err := Compile("p.go", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, valueTypesDriver, false)
	for _, tc := range []struct {
		in   []byte
		line string
	}{
		// 0xff is invalid; 0xe2 0x82 is cut off by the end of the line, or
		// followed by an ASCII byte: each byte becomes '?', as Go's
		// utf8.DecodeRune reports it.
		{[]byte{0, 1}, "panic: a???"},
		{[]byte{0, 2}, "panic: a???efgh"},
		{[]byte{0, 200}, "panic: a???" + strings.Repeat("efghijklmnopqrstuvwxyzabcd", 20)[:512-11]},
		{[]byte{1, 0}, "panic: -9223372036854775808"},
		{[]byte{2, 0}, "panic: 18446744073709551615"},
	} {
		in := make([]byte, 128)
		copy(in, tc.in)
		out := make([]byte, 1024)
		out[0] = 1 // the deferred write does not happen
		want := fmt.Sprintf("L:%s P:%x\n", hex.EncodeToString([]byte(tc.line)), out)
		if got := runHost(t, bin, fmt.Sprintf("%x\n", in)); got != want {
			t.Errorf("%x: got %q, want %q", tc.in, got, want)
		}
	}
}
