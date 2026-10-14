package compiler

import (
	"bytes"
	"strings"
	"testing"
)

// Signed integers and uint are ordinary subset types: every form that
// admits an unsigned scalar admits them, and len/cap results, range keys and
// math/bits counts are ordinary int values.
func TestIntegerChecksAccept(t *testing.T) {
	for name, source := range map[string]string{
		"locals":        `func Process(s, i []byte) uint64 { var a int8 = -128; b := int16(a) * 3; var c int32; d := int64(-1); e := 5; var f uint = 7; c = int32(b) - 1; return uint64(a) + uint64(b) + uint64(c) + uint64(d) + uint64(e) + uint64(f) }`,
		"params":        `func f(a int8, b int, c uint) (int64, int) { return int64(a) * int64(b), b - int(c) };func Process(s, i []byte) uint64 { x, y := f(-1, 2, 3); return uint64(x) ^ uint64(y) }`,
		"named":         `type T int32;type N int;type U uint;func (t T) Neg() T { return -t };func Process(s, i []byte) uint64 { var t T = -5; n := N(t.Neg()); return uint64(n) + uint64(U(n)) }`,
		"fields arrays": `type K [4]int16;type S struct{ A int8; B int; C K; D [2]uint };func Process(s, i []byte) uint64 { var x S; x.C[1] = -2; x.D[0]--; x.A++; return uint64(x.C[1]) + uint64(x.D[0]) + uint64(x.A) + uint64(x.B) }`,
		"pointers":      `func bump(p *int64) { *p = *p - 1 };func Process(s, i []byte) uint64 { var v int64; bump(&v); var a [2]int8; p := &a; p[1]--; return uint64(v) + uint64(a[1]) }`,
		"constants":     `const Min int64 = -1 << 63;const Max = 1<<63 - 1;const Neg int8 = -128;func Process(s, i []byte) uint64 { x := int64(i[0]); if x > Min && x < Max && int8(x) != Neg { return uint64(x / -2) }; return 0 }`,
		"operators":     `func Process(s, i []byte) uint64 { x, y := int32(i[0])-100, int32(i[1])-100; r := x+y-x*y; r = r/(y|1) + r%(y|1) + (x&y | x^y) &^ y; return uint64(-r) + uint64(^r) + uint64(+r) }`,
		"shifts":        `func Process(s, i []byte) uint64 { x := int16(i[0]) - 9; n := int(i[1]) - 3; var u uint = 2; return uint64(x<<n) + uint64(x>>n) + uint64(x<<u) + uint64(uint8(7)>>int8(n)) + uint64(x>>15) }`,
		"comparisons":   `func Process(s, i []byte) uint64 { x := int8(i[0]); if x < -1 || x >= 100 || x == -128 || -x > 3 { return 1 }; return 0 }`,
		"switch":        `func Process(s, i []byte) uint64 { switch x := int64(i[0]) - 5; x { case -5, -1: return 1; case 0: return 2 }; return 3 }`,
		"range":         `func Process(s, i []byte) uint64 { t := uint64(0); for k := range int8(i[0]) - 5 { t = t + uint64(k) }; for k := range len(s) - 1 { t = t + uint64(k) }; return t }`,
		"incdec":        `type S struct{ N int32 };func Process(s, i []byte) uint64 { var a S; a.N--; var b int64 = 9223372036854775807; b++; return uint64(a.N) + uint64(b) }`,
		"int values":    `func n(s []byte) int { return len(s) - 1 };func Process(s, i []byte) uint64 { k := len(i); k = k - n(s); m := -cap(s); for j := range s { k = k + j*2 }; return uint64(k) + uint64(m) }`,
		"conversions":   `func Process(s, i []byte) uint64 { x := int64(i[0]) - 200; return uint64(int8(x)) + uint64(uint16(x)) + uint64(int32(uint8(x))) + uint64(uint(x)) + uint64(int(uint32(x))) }`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkMessage(t, source); got != "" {
				t.Fatalf("rejected: %s", got)
			}
			if _, err := Compile("int.go", []byte("package p\n"+source)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIntegerChecksReject(t *testing.T) {
	for name, c := range map[string]struct{ source, want string }{
		"float":             {`func Process(s, i []byte) uint64 { var f float64; _ = f; return 0 }`, "unsupported type float64"},
		"uintptr":           {`func Process(s, i []byte) uint64 { var p uintptr; _ = p; return 0 }`, "unsupported type uintptr"},
		"complex":           {`func Process(s, i []byte) uint64 { var c complex64; _ = c; return 0 }`, "unsupported type complex64"},
		"string":            {`func Process(s, i []byte) uint64 { r := 'a'; _ = r; x := "s"; _ = x; return 0 }`, "unsupported type string"},
		"constant range":    {`func Process(s, i []byte) uint64 { var x int8 = 128; return uint64(x) }`, "cannot use 128 (untyped int constant) as int8 value in variable declaration (overflows)"},
		"negative uint":     {`func Process(s, i []byte) uint64 { var x uint = -1; return uint64(x) }`, "cannot use -1 (untyped int constant) as uint value in variable declaration (overflows)"},
		"negative shift":    {`func Process(s, i []byte) uint64 { x := int64(i[0]); return uint64(x << -1) }`, "invalid operation: negative shift count -1 (untyped int constant)"},
		"constant div0":     {`func Process(s, i []byte) uint64 { x := int32(i[0]); return uint64(x / 0) }`, "invalid operation: division by zero"},
		"constant overflow": {`func Process(s, i []byte) uint64 { const m int64 = -1 << 63; return uint64(m * -1) }`, "m * -1 (constant 9223372036854775808 of type int64) overflows int64"},
		"untyped too big":   {`func Process(s, i []byte) uint64 { return uint64(1 << 64) }`, "constant 18446744073709551616 overflows uint64"},
		"compound":          {`func Process(s, i []byte) uint64 { x := int8(1); x += 2; return uint64(x) }`, "compound assignment unsupported"},
		"int slice":         {`func Process(s, i []byte) uint64 { var w []int8; _ = w; return 0 }`, "unsupported type []int8"},
		"int view":          {`func Process(s, i []byte) uint64 { var k [4]int8; w := k[:]; return uint64(w[0]) }`, "slice views require byte/uint8 elements"},
		"array limit":       {`type K [129]int;func Process(s, i []byte) uint64 { return 0 }`, "array storage exceeds experimental 1024-byte per-value limit"},
		"float element":     {`type K [2]float32;func Process(s, i []byte) uint64 { return 0 }`, arrayElementRule},
		"other builtin":     {`func Process(s, i []byte) uint64 { return uint64(min(int8(i[0]), -1)) }`, "only len and cap builtins supported"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkMessage(t, c.source); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Programs that do not use int8, int16 or int32 emit no signed typedefs, and
// programs without a signed / or % emit no division helpers, so their C is
// unchanged (the frozen corpus checks every recorded program's hash).
func TestSignedRuntimeOnlyWhenUsed(t *testing.T) {
	for _, tc := range []struct {
		source           string
		typedefs, divide bool
	}{
		{"func Process(s, i []byte) uint64 { return uint64(len(s)) }", false, false},
		{"func Process(s, i []byte) uint64 { x := int64(len(s)); return uint64(x * -3) }", false, false},
		{"func Process(s, i []byte) uint64 { const c int64 = -7 / 2; return uint64(c + int64(len(s))) }", false, false},
		{"func Process(s, i []byte) uint64 { x := int8(len(s)); return uint64(x) }", true, false},
		{"func Process(s, i []byte) uint64 { x := len(s); return uint64(x % 3) }", false, true},
	} {
		c, err := Compile("p.go", []byte("package p\n"+tc.source))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(c, []byte("typedef signed char i8;")) != tc.typedefs || bytes.Contains(c, []byte("gosvm_quo64")) != tc.divide {
			t.Errorf("%s: typedefs %v, division helpers %v", tc.source, bytes.Contains(c, []byte("typedef signed char")), bytes.Contains(c, []byte("gosvm_quo64")))
		}
		if strings.Contains(string(c), "__divdi3") {
			t.Fatal("libcall")
		}
	}
}
