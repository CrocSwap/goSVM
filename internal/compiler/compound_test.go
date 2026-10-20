package compiler

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// Compound assignment (docs/GO_INTEGERS.md#compound-assignment): accepted
// forms on every target, rejections with exact messages, the order of
// operand evaluation, and the emitted C.

const compoundPrefix = `package p
type P struct {
	X   int16
	A   [3]uint32
	Q   *P
}
type Amount uint64
type Ctr map[string]int
func (c Ctr) bump(k string, by int) { c[k] += by }
func (p *P) scale(f int16) { p.X *= f }
func Sum[T ~uint8 | ~int64 | ~uint64](xs []T) (s T) { for _, x := range xs { s += x }; return }
func Process(s, i []byte) uint64 {
`

// compoundMessage is Check's diagnostic for a Process body after
// compoundPrefix (with the file name), or "".
func compoundMessage(t *testing.T, body string) string {
	t.Helper()
	return funcMessage(t, compoundPrefix+body+"\n}\n")
}

func TestCompoundAcceptedForms(t *testing.T) {
	t.Parallel()
	ops := []string{"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "&^=", "<<=", ">>="}
	types := []string{"uint8", "uint16", "uint32", "uint64", "uint", "int8", "int16", "int32", "int64", "int", "Amount", "byte"}
	for _, typ := range types {
		var b strings.Builder
		fmt.Fprintf(&b, "x := %s(i[0]) + 3; y := %s(i[1]) | 1; c := uint8(i[2]) %% 70\n", typ, typ)
		for _, op := range ops {
			rhs := "y"
			if op == "<<=" || op == ">>=" {
				rhs = "c"
			}
			fmt.Fprintf(&b, "x %s %s\n", op, rhs)
		}
		b.WriteString("return uint64(x)")
		t.Run(typ, func(t *testing.T) {
			if got := compoundMessage(t, b.String()); got != "" {
				t.Fatalf("rejected: %s", got)
			}
		})
	}
	for name, body := range map[string]string{
		"field":              `var p P; p.X -= int16(i[0]); return uint64(p.X)`,
		"field via pointer":  `p := &P{}; p.X ^= 7; p.A[1] <<= 3; return uint64(p.A[1])`,
		"pointer chain":      `p := &P{Q: &P{}}; p.Q.X %= 5; return uint64(p.Q.X)`,
		"array element":      `var a [4]int8; a[i[0]%4] -= 3; return uint64(a[0])`,
		"pointer to array":   `a := &[2]uint64{}; a[1] |= 9; return a[1]`,
		"slice element":      `xs := make([]int32, 3); xs[i[0]%3] *= -2; return uint64(xs[0])`,
		"byte slice element": `s[0] += i[0]; s[1] &^= 0x0f; s[2] >>= i[1] % 9; return 0`,
		"nested element":     `ps := []P{{}}; ps[0].A[2] += 1; return uint64(ps[0].A[2])`,
		"pointer deref":      `v := uint16(3); p := &v; *p <<= 2; return uint64(v)`,
		"map element":        `m := map[string]int{}; m["a"] += 2; m["a"] *= 3; m["b"] -= 1; return uint64(m["a"])`,
		"map of structs":     `m := map[uint8]*P{1: {}}; m[1].X += 4; return uint64(m[1].X)`,
		"named map method":   `c := Ctr{}; c.bump("x", 3); return uint64(c["x"])`,
		"pointer receiver":   `p := &P{X: 2}; p.scale(-3); return uint64(p.X)`,
		"captured":           `n := uint64(0); f := func(d uint64) { n += d }; f(2); f(3); return n`,
		"named result":       `return named(uint8(i[0]))`,
		"loop post":          `t := uint64(0); for j := 0; j < 10; j += 3 { t += uint64(j) }; return t`,
		"loop post captured": `var fs []func() int; for j := 1; j < 50; j *= 3 { fs = append(fs, func() int { return j }) }; return uint64(fs[1]())`,
		"generic":            `return uint64(Sum([]int64{-1, 2})) + Sum([]uint64{3})`,
		"signed shift count": `x := int32(-8); x >>= int8(i[0] % 3); x <<= int64(1); return uint64(x)`,
		"parenthesized":      `x := uint8(1); (x) += 2; return uint64(x)`,
		"string unchanged":   `t := "a"; t += string(i[:1]); return uint64(len(t))`,
		"untyped constant":   `x := int64(5); x /= -2; x %= 3; x -= 1 << 3; return uint64(x)`,
		"map element of map": `m := map[int]map[int]uint8{1: {}}; m[1][2] += 5; return uint64(m[1][2])`,
	} {
		t.Run(name, func(t *testing.T) {
			source := compoundPrefix + body + "\n}\nfunc named(v uint8) (r uint64) { defer func() { r *= 2 }(); r = uint64(v); r += 1; return }\n"
			if got := funcMessage(t, source); got != "" {
				t.Fatalf("rejected: %s", got)
			}
			c, err := Compile("f.go", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			hostCompile(t, c, DefaultHeapBytes, "", true)
		})
	}
}

func TestCompoundRejections(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ body, want string }{
		"bool":           {`b := i[0] > 1; b += true; return 0`, "f.go:13:16: invalid operation: operator + not defined on b (variable of type bool)"},
		"string minus":   {`t := "a"; t -= "b"; return 0`, "f.go:13:11: invalid operation: operator - not defined on t (variable of type string)"},
		"float":          {`f := 1.5; f += 1; return 0`, "f.go:13:6: unsupported type float64"},
		"pointer":        {`p := &P{}; p += 1; return 0`, "f.go:13:12: invalid operation: p += 1 (mismatched types *P and untyped int)"},
		"map struct":     {`m := map[int]P{}; m[1].X += 1; return 0`, "f.go:13:19: cannot assign to struct field m[1].X in map"},
		"mismatch":       {`x := uint8(1); x += uint16(2); return 0`, "f.go:13:16: invalid operation: x += uint16(2) (mismatched types uint8 and uint16)"},
		"constant zero":  {`x := uint8(1); x /= 0; return 0`, "f.go:13:21: invalid operation: division by zero"},
		"negative shift": {`x := uint8(1); x <<= -1; return 0`, "f.go:13:22: invalid operation: negative shift count -1 (untyped int constant)"},
		"blank":          {`_ += 1; return 0`, "f.go:13:1: cannot use _ as value or type"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := compoundMessage(t, c.body); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// The target's operands are read after the right operand's calls, as gc
// does (docs/ERROR_CONTRACT.md#evaluation-order): a right operand that
// reassigns the slice, map or index variable of its own target updates the
// new target. Elements changed by the right operand are seen.
func TestCompoundOperandOrder(t *testing.T) {
	t.Parallel()
	source := `package p
func Process(s, i []byte) uint64 {
	b := []uint64{1, 2, 3}
	r := func() uint64 { b = []uint64{10, 20, 30}; return 1 }
	old := b
	b[0] += r()
	j := 0
	cc := []uint64{1, 2}
	rj := func() uint64 { j = 1; return 1 }
	cc[j] += rj()
	m := map[int]uint64{1: 1}
	om := m
	rm := func() uint64 { m = map[int]uint64{1: 500}; return 2 }
	m[1] += rm()
	a := []uint64{1, 2}
	ra := func() uint64 { a[0] = 50; return 7 }
	a[0] += ra()
	plain := []uint64{1, 2, 3}
	pold := plain
	rp := func() uint64 { plain = []uint64{10, 20, 30}; return 9 }
	plain[0] = rp()
	s[0], s[1], s[2], s[3], s[4] = byte(old[0]), byte(b[0]), byte(cc[0]), byte(cc[1]), byte(om[1])
	s[5], s[6], s[7], s[8] = byte(m[1]), byte(a[0]), byte(pold[0]), byte(plain[0])
	return 0
}
`
	c, err := Compile("f.go", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	bin := hostCompile(t, c, DefaultHeapBytes, allocDriver, false)
	got := runHost(t, bin, strings.Repeat("00", 128)+"\n")
	// The old b untouched (1), the new b's element 10+1; cc[1] = 2+1 (j
	// became 1); the old map untouched (1), the new map's 500+2 (byte 0xf6);
	// a[0] = 50+7; the plain assignment likewise stores into the new slice.
	// These are native Go 1.22.0's results.
	want := fmt.Sprintf("0:%x%s\n", []byte{1, 11, 1, 3, 1, 0xf6, 57, 1, 9}, strings.Repeat("00", 1024-9))
	if got != want {
		t.Fatalf("got  %.40s\nwant %.40s", got, want)
	}
}

// Programs without compound assignment take none of its paths; a program
// with one gets the signed-division runtime only for a signed /= or %=.
func TestCompoundEmission(t *testing.T) {
	t.Parallel()
	compile := func(body string) []byte {
		t.Helper()
		c, err := Compile("f.go", []byte("package p\nfunc Process(s, i []byte) uint64 {\n"+body+"\n}\n"))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	plain := compile(`x := int64(i[0]); x = x / 3; return uint64(x)`)
	compound := compile(`x := int64(i[0]); x /= 3; return uint64(x)`)
	unsigned := compile(`x := uint64(i[0]); x /= 3; return x`)
	for name, c := range map[string][]byte{"plain": plain, "compound": compound} {
		if !bytes.Contains(c, []byte("gosvm_quo64(")) {
			t.Errorf("%s: no signed division runtime", name)
		}
	}
	if bytes.Contains(unsigned, []byte("gosvm_quo64")) {
		t.Error("unsigned /= emits the signed division runtime")
	}
	if !bytes.Contains(unsigned, []byte("== 0) abort();")) {
		t.Error("/= without a zero-divisor trap")
	}
}
