package compiler

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Check-level tables, conversion and heap tests for strings
// (docs/GO_STRINGS.md).

const stringPrefix = `package p
type Name string
type Rec struct {
	K string
	T [2]string
}
type Text interface{ ~string }
func Cat[T Text](a, b T) T { return a + b }
func Less[T interface{ ~string | ~int8 }](a, b T) bool { return a < b }
type Box[T any] struct{ V T }
type E struct{ msg string }
func (e *E) Error() string { return e.msg }
type Code uint8
func (c Code) Error() string { return "code" }
type B byte
func fail(x byte) error {
	if x == 0 {
		return nil
	}
	return &E{msg: "bad"}
}
func pair() (string, error) { return "ok", nil }
func Process(s, i []byte) uint64 {
`

func TestStringAcceptedForms(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"literals":             "a, b := \"x\\n\\x00é\", `raw\\n`; s[0] = a[0] + b[0]",
		"empty":                `var e string; if e == "" { s[0] = byte(len(e)) }`,
		"constants":            `const c = "ab" + "c"; const n Name = "n"; x := c + string(n); s[0] = x[3]`,
		"len index slice":      `x := string(i); y := x[1:3]; z := x[2:]; w := x[:1]; s[0] = y[0] + z[0] + w[0] + byte(len(x))`,
		"concatenation":        `x := string(i); y := x + "-" + x + (x + x); s[0] = byte(len(y))`,
		"add assign":           `x := "a"; x += string(i); r := Rec{}; r.K += x; r.T[1] += x; p := &r.K; *p += "z"; xs := []string{x}; xs[0] += x; s[0] = byte(len(x))`,
		"comparisons":          `x, y := string(i[:1]), "b"; if x == y || x != y || x < y || x <= y || x > y || x >= y { s[0] = 1 }`,
		"conversions":          `b := []byte("abc"); r := []rune(string(b)); x := string(r) + string(b) + string(rune(r[0])) + string(i[0]); _ = Name(x); s[0] = byte(len(x))`,
		"integer conversions":  `x := string(rune(int8(-1))) + string(rune(int64(i[0]))) + string(rune(uint32(i[1]))); s[0] = x[0]`,
		"range":                `for k, r := range string(i) { s[k] = byte(r) }; for range "ab" {}; var k int; var r rune; for k, r = range "é" { _ = r }; _ = k`,
		"switch":               `switch string(i[:2]) { case "ab", "cd": s[0] = 1; case string(s[:2]): s[0] = 2; default: }`,
		"struct fields":        `r := Rec{K: "k", T: [2]string{"a", "b"}}; if r.T == [2]string{"a", "b"} { s[0] = r.K[0] }`,
		"arrays":               `var a [3]string; a[1] = "x"; b := a; if a == b { s[0] = a[1][0] }`,
		"slices":               `xs := []string{"a"}; xs = append(xs, "b", "c"); ys := make([]string, 2); copy(ys, xs); s[0] = byte(len(ys[1]))`,
		"pointers":             `p := new(string); *p = "x"; q := &Rec{K: "k"}; s[0] = (*p)[0] + q.K[0]`,
		"interfaces":           `var a any = "x"; var b any = Name("x"); if a != b && a == "x" { s[0] = a.(string)[0] }; switch v := b.(type) { case Name: _ = v; case string: }`,
		"generics":             `x := Cat("a", "b"); y := Cat(Name("a"), "b"); if Less(x, string(y)) && Less(int8(-1), 1) { s[0] = 1 }; bx := Box[string]{V: x}; s[1] = bx.V[0]`,
		"error values":         `err := fail(i[0]); if err != nil { s[0] = err.Error()[0] }; var e2 error = Code(1); if e2 == Code(1) { s[1] = 1 }`,
		"error type switch":    `switch e := fail(i[0]).(type) { case nil: case *E: s[0] = e.msg[0]; case Code: }`,
		"error results":        `v, err := pair(); if err == nil { s[0] = v[0] }`,
		"error assertions":     `var a any = fail(1); if e, ok := a.(error); ok { _ = e.(*E) }`,
		"append and copy":      `b := append([]byte(nil), "ab"...); b = append(b, string(i)...); n := copy(b, "xy"); s[0] = byte(n)`,
		"bytes of constant":    `b := []byte(""); c := []byte("abc"); s[0] = byte(cap(b) + cap(c))`,
		"string from bytes":    `var k [4]byte; x := string(k[:]); s[0] = byte(len(x))`,
		"moved string":         `x := "a"; p := &x; var q *string = p; _ = q; s[0] = (*p)[0]`,
		"escaping pointer":     `xs := []*string{}; for j := 0; j < 2; j++ { v := string(i[j : j+1]); xs = append(xs, &v) }; s[0] = (*xs[1])[0]`,
		"method on named":      `n := Name("x"); _ = n; var c error = Code(2); s[0] = byte(len(c.Error()))`,
		"multiple results":     `a, b := "x", string(i); a, b = b, a; s[0] = byte(len(a) + len(b))`,
		"constant index":       `s[0] = "abc"[2]; s[1] = "abc"[i[0]%3]; x := "abcd"[1:3]; s[2] = x[0]`,
		"named byte elements":  `bs := []B("ab"); x := string(bs); s[0] = byte(bs[0]) + x[1]`,
		"untyped string boxed": `var a any = "x"; if s, ok := a.(string); ok { _ = s }`,
	} {
		t.Run(name, func(t *testing.T) {
			source := stringPrefix + body + "\nreturn 0 }\n"
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

func TestStringRejections(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ body, want string }{
		"compound bool":     {`x := true; x += false; _ = x`, "invalid operation: operator + not defined on x (variable of type bool)"},
		"float conversion":  {`var f float64 = 1; _ = f`, "unsupported type float64"},
		"function of float": {`var f func() float32; _ = f`, "unsupported type float32"},
		"three-index":       {`x := "abc"; _ = x[0:1:2]`, "invalid operation: 3-index slice of string"},
		"assign to byte":    {`x := "abc"; x[0] = 'b'`, "cannot assign to x[0] (neither addressable nor a map index expression)"},
	} {
		t.Run(name, func(t *testing.T) {
			source := stringPrefix + c.body + "\nreturn 0 }\n"
			if got := allocMessage(t, source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
	// Standard-library packages outside the overlay are rejected
	// (docs/GO_STDLIB.md); errors, strings, unicode/utf8, strconv and io are
	// available on chain.
	for _, path := range []string{"fmt", "bufio", "math/big"} {
		source := fmt.Sprintf("package p\nimport _x %q\nfunc Process(s, i []byte) uint64 { return 0 }\n", path)
		want := fmt.Sprintf("alloc.go:2:11: could not import %s (%s)", path, stdlibImportRule(path))
		_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte(source)}}}}})
		if err == nil || err.Error() != want {
			t.Errorf("import %s: got %v\nwant %s", path, err, want)
		}
	}
	// A missing user package keeps the module hint.
	_, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "alloc.go", Data: []byte("package p\nimport _x \"shared/missing\"\nfunc Process(s, i []byte) uint64 { return 0 }\n")}}}}})
	if err == nil || !strings.Contains(err.Error(), "use a module-backed program for user packages") {
		t.Errorf("user package: %v", err)
	}
}

// TestStringRuntimeOnlyWhenUsed: programs without string values carry no
// string runtime (string constants used only in constant expressions are
// folded); programs that never allocate strings carry no heap runtime.
func TestStringRuntimeOnlyWhenUsed(t *testing.T) {
	t.Parallel()
	constant := "package p\nconst k = \"abc\"\nfunc Process(s, i []byte) uint64 { s[0] = byte(len(k + \"d\")); return uint64(len(k)) }\n"
	out, err := Compile("a.go", []byte(constant))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("gosvm_string")) {
		t.Fatal("a constant-only program got the string runtime")
	}
	readOnly := "package p\nfunc Process(s, i []byte) uint64 { x := \"abc\"; if x[1:] == \"bc\" { s[0] = x[0] }; for _, r := range x { s[1] = byte(r) }; return 0 }\n"
	out, err = Compile("a.go", []byte(readOnly))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("gosvm_string")) || bytes.Contains(out, []byte("gosvm_alloc")) || bytes.Contains(out, []byte("gosvm_concat")) {
		t.Fatal("a non-allocating string program got the heap runtime, or no string runtime")
	}
	alloc := "package p\nfunc Process(s, i []byte) uint64 { x := string(i); return uint64(len(x + x)) }\n"
	out, err = Compile("a.go", []byte(alloc))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("gosvm_concat")) || !bytes.Contains(out, []byte("gosvm_alloc")) {
		t.Fatal("an allocating string program lacks the heap runtime")
	}
	source := stringSource(t)
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

// intStringTypes are the integer types converted with string(x) in
// intStringSource. vet rejects string(x) for integers other than rune and
// byte in Go source, so the native oracle converts through reflect, which
// implements the language conversion.
var intStringTypes = []string{"int8", "int16", "int32", "int64", "int", "uint8", "uint16", "uint32", "uint64", "uint"}

// intStringSource converts in[1:9] (little-endian), truncated to each
// integer type, to a string and writes each result length-prefixed.
func intStringSource() string {
	var b strings.Builder
	b.WriteString("package p\nfunc Process(s, i []byte) uint64 {\n\tv := uint64(0)\n\tfor j := 0; j < 8; j++ {\n\t\tv = v | uint64(i[1+j])<<(8*j)\n\t}\n\to := 0\n\tvar x string\n")
	for _, typ := range intStringTypes {
		fmt.Fprintf(&b, "\tx = string(%s(v))\n\ts[o] = byte(len(x))\n\to = o + 1 + copy(s[o+1:], x)\n", typ)
	}
	b.WriteString("\treturn uint64(o)\n}\n")
	return b.String()
}

// intStringNative is intStringSource's result computed by Go's conversions.
func intStringNative(in []byte) string {
	v := uint64(0)
	for j := 0; j < 8; j++ {
		v |= uint64(in[1+j]) << (8 * j)
	}
	values := []any{int8(v), int16(v), int32(v), int64(v), int(v), uint8(v), uint16(v), uint32(v), v, uint(v)}
	out := make([]byte, 1024)
	o := 0
	for _, x := range values {
		s := reflect.ValueOf(x).Convert(reflect.TypeOf("")).String()
		out[o] = byte(len(s))
		o = o + 1 + copy(out[o+1:], s)
	}
	return fmt.Sprintf("%d:%x\n", o, out)
}

// TestStringIntegerConversions checks string(x) for every integer type
// against Go's conversion: valid runes encode, everything else (negative
// values, surrogates, values above U+10FFFF, including values that only
// truncate into range) is U+FFFD.
func TestStringIntegerConversions(t *testing.T) {
	t.Parallel()
	c, err := Compile("a.go", []byte(intStringSource()))
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	values := []uint64{0, 'a', 0x7f, 0x80, 0xff, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff, 0x10000,
		0x10ffff, 0x110000, 0x7fffffff, 0x80000000, 0xffffffff, 0x100000041, 1<<63 - 1, 1 << 63, 1<<64 - 1, 1<<64 - 0x41,
		0xffff_ffff_0000_d800, 0x0000_0001_0000_0041}
	for j := 0; j < 200; j++ {
		values = append(values, uint64(j)*0x9e3779b97f4a7c15)
	}
	var input strings.Builder
	var want strings.Builder
	for _, v := range values {
		in := make([]byte, 128)
		for j := 0; j < 8; j++ {
			in[1+j] = byte(v >> (8 * j))
		}
		fmt.Fprintf(&input, "%x\n", in)
		want.WriteString(intStringNative(in))
	}
	if got := runHost(t, bin, input.String()); got != want.String() {
		t.Fatalf("string(integer) conversions differ from Go:\n%s\nwant\n%s", got, want.String())
	}
}

func stringHeapInput(kind byte, n, la, lb int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3], in[4], in[5] = 8, byte(n), byte(n>>8), kind, byte(la-1), byte(lb-1)
	for j := 8; j < 128; j++ {
		in[j] = byte('a' + j%26) // ASCII: one rune per byte
	}
	return in
}

// roundupsize is Go 1.22's size class rounding for noscan objects.
func roundupsize(n int) int {
	for _, c := range []int{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512} {
		if c >= n {
			return c
		}
	}
	panic("size")
}

// stringHeapLimit is the number of mode-8 iterations of kind that fit the
// default 32 KiB heap (32,760 usable bytes) after a and b (la and lb bytes)
// are allocated; -1 when an iteration allocates nothing.
func stringHeapLimit(kind byte, la, lb int) int {
	round := func(n int) int { return (n + 7) &^ 7 }
	per := map[byte]int{
		0: round(la + lb),          // a + b
		1: round(la),               // string(in[...])
		2: roundupsize(la),         // []byte(a): rawbyteslice capacity
		3: 8,                       // string(rune): always 8
		4: roundupsize(4 * la),     // []rune(a): rawruneslice
		5: 0,                       // a + "": the non-empty operand itself
		6: 16,                      // boxing a string header
		7: round(len("constant!")), // []byte(constant): exactly its length
	}[kind]
	if per == 0 {
		return -1
	}
	return (32760 - round(la) - round(lb)) / per
}

// TestStringHeap checks the heap cost of every allocating string operation
// exactly: the iteration that does not fit aborts after the predicted number
// of iterations; native Go never runs out, so the expectation is computed.
func TestStringHeap(t *testing.T) {
	t.Parallel()
	bin := hostCompile(t, compileStrings(t), DefaultHeapBytes, allocDriver, false)
	oom := func(n int) string {
		out := make([]byte, 1024)
		out[0], out[1] = byte(n), byte(n>>8)
		return fmt.Sprintf("P:%x\n", out)
	}
	checked := 0
	for kind := byte(0); kind < 8; kind++ {
		for _, size := range [][2]int{{1, 1}, {5, 3}, {8, 8}, {9, 17}, {17, 9}, {24, 32}, {32, 32}} {
			la, lb := size[0], size[1]
			limit := stringHeapLimit(kind, la, lb)
			if limit < 0 {
				in := stringHeapInput(kind, 6000, la, lb)
				if got, want := runHost(t, bin, fmt.Sprintf("%x\n", in)), nativeStringCall(in); got != want {
					t.Errorf("kind %d (%d, %d): 6000 iterations: %.16s, want %.16s", kind, la, lb, got, want)
				}
				checked++
				continue
			}
			fits := stringHeapInput(kind, limit, la, lb)
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", fits)), nativeStringCall(fits); got != want {
				t.Errorf("kind %d (%d, %d): %d iterations: %.16s, want %.16s", kind, la, lb, limit, got, want)
			}
			over := stringHeapInput(kind, limit+1, la, lb)
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", over)), oom(limit); got != want {
				t.Errorf("kind %d (%d, %d): %d iterations: %.16s, want %.16s", kind, la, lb, limit+1, got, want)
			}
			if got := nativeStringCall(over); strings.HasPrefix(got, "P:") {
				t.Errorf("kind %d: native Go panicked", kind)
			}
			checked += 2
		}
	}
	t.Logf("%d heap boundaries match the documented costs", checked)
}

// TestStringDocumentationExample compiles the example in docs/GO_STRINGS.md.
func TestStringDocumentationExample(t *testing.T) {
	t.Parallel()
	doc := string(mustRead(t, "../../docs/GO_STRINGS.md"))
	start := strings.Index(doc, "```go\n")
	if start < 0 {
		t.Fatal("example not found")
	}
	body := doc[start+len("```go\n"):]
	body = body[:strings.Index(body, "```")]
	c, err := Compile("example.go", []byte("package main\n\n"+body))
	if err != nil {
		t.Fatal(err)
	}
	hostCompile(t, c, DefaultHeapBytes, "", true)
}
