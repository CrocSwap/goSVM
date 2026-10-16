package compiler

import (
	"strings"
	"testing"
)

// Package-level tables above the 1,024-byte per-value array limit
// (docs/GO_GLOBALS.md#large-tables): admitted only when no value of the
// array type is ever copied, so the table never occupies a stack frame.

// largeTablePrefix declares tables like holiman/uint256's
// `var pows = [60]Int{...}` (Int = [4]uint64, 1,920 bytes).
const largeTablePrefix = `package p
type Int [4]uint64
type Pair struct{ K uint32; V uint64 }
`

func TestLargeTablesAccepted(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		// Read-only data: a constant [60]Int and a [300]Pair table (4,800
		// bytes, above the 4 KiB SBF stack frame).
		"read-only uint256 table": {`var pows = [60]Int{{1}, {10}, {100}, 59: {0, 0, 0, 7}}
func Process(o, in []byte) uint64 {
	n := uint64(0)
	for i := range pows { n += pows[i][0] + pows[i][3] }
	x := pows[in[0]]
	return n*1000 + x[0] + uint64(len(pows))
}`, "118160"},
		"read-only struct table indexed in place": {`var pairs = [300]Pair{1: {K: 2, V: 3}, 299: {K: 4, V: 5}}
func Process(o, in []byte) uint64 {
	return uint64(pairs[1].K)*100 + pairs[299].V*10 + uint64(cap(pairs)) % 7
}`, "256"},
		// The globals block: a zero [200]Int (6,400 bytes) written in place.
		"writable zero table": {`var acc [200]Int
func init() { for i := range acc { acc[i][i%4] = uint64(i) } }
func Process(o, in []byte) uint64 {
	acc[199][3]++
	acc[5] = Int{9}
	p := &acc[7]
	p[0] += 2
	return acc[199][3]*10000 + acc[5][0]*100 + acc[7][3] + acc[6][2]
}`, "2000913"},
		// Element addresses passed to read-only pointer parameters keep the
		// table read-only (readonlyptr.go), as uint256's Log10 does with
		// z.Lt(&pows[t-20]).
		"read-only through pointer parameters": {`var pows = [60]Int{{1}, {10}, 59: {0, 0, 0, 7}}
func (z *Int) Lt(x *Int) bool { return z[3] < x[3] || z[3] == x[3] && z[0] < x[0] }
func top(x *Int) uint64 { return x[3] }
func Process(o, in []byte) uint64 {
	var z Int
	z[0] = uint64(in[0])
	n := uint64(0)
	if z.Lt(&pows[1]) { n++ }
	if pows[59].Lt(&z) { n += 10 }
	return n*100 + top(&pows[59])
}`, "107"},
		"named table type and qualified uses": {`type Table [128]Pair
var tab Table
func Process(o, in []byte) uint64 {
	for i := range len(tab) { tab[i].V = uint64(i) }
	for range tab { tab[3].K++ }
	return tab[127].V*1000 + uint64(tab[3].K)
}`, "127128"},
	} {
		t.Run(name, func(t *testing.T) {
			p := globalsProgram(unitFiles("p", largeTablePrefix+tc.src))
			if got := resultOf(t, runGlobalHost(t, p, []byte{2})); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLargeTablesRejected(t *testing.T) {
	const limit = "array storage exceeds experimental 1024-byte per-value limit"
	for name, src := range map[string]string{
		"local":                     `var pows [60]Int; func Process(o, in []byte) uint64 { var l [60]Int; return l[0][0] + pows[0][0] }`,
		"copy to a local":           `var pows [60]Int; func Process(o, in []byte) uint64 { l := pows; return l[0][0] }`,
		"assignment":                `var pows, b [60]Int; func Process(o, in []byte) uint64 { b = pows; return b[0][0] }`,
		"parameter":                 `var pows [60]Int; func f(t [60]Int) uint64 { return t[0][0] }; func Process(o, in []byte) uint64 { return f(pows) }`,
		"pointer parameter":         `var pows [60]Int; func f(t *[60]Int) uint64 { return t[0][0] }; func Process(o, in []byte) uint64 { return f(&pows) }`,
		"comparison":                `var pows, b [60]Int; func Process(o, in []byte) uint64 { if pows == b { return 1 }; return 0 }`,
		"interface":                 `var pows [60]Int; func Process(o, in []byte) uint64 { var x any = pows; _ = x; return 0 }`,
		"range with value":          `var pows [60]Int; func Process(o, in []byte) uint64 { n := uint64(0); for _, v := range pows { n += v[0] }; return n }`,
		"slicing":                   `var pows [60]Int; func Process(o, in []byte) uint64 { s := pows[:]; return s[0][0] }`,
		"writable initializer":      `var pows = [60]Int{{1}}; func Process(o, in []byte) uint64 { pows[0][0]++; return pows[0][0] }`,
		"written through a pointer": `var pows = [60]Int{{1}}; func set(p *Int) { p[0] = 2 }; func Process(o, in []byte) uint64 { set(&pows[3]); return pows[3][0] }`,
		"pointer kept":              `var pows = [60]Int{{1}}; var keep *Int; func hold(p *Int) { keep = p }; func Process(o, in []byte) uint64 { hold(&pows[3]); return keep[0] }`,
		"computed initializer":      `var pows = mk(); func mk() (t [60]Int) { return }; func Process(o, in []byte) uint64 { return pows[0][0] }`,
		"struct field":              `type S struct{ t [60]Int }; var s S; func Process(o, in []byte) uint64 { return s.t[0][0] }`,
		"type only":                 `type T [60]Int; func Process(o, in []byte) uint64 { return 0 }`,
		"pointer receiver method":   `type T [60]Int; func (t *T) At(i int) uint64 { return t[i][0] }; var tab T; func Process(o, in []byte) uint64 { return tab.At(1) }`,
		"nested large elements":     `var big [2][60]Int; func Process(o, in []byte) uint64 { return big[1][2][3] }`,
		"literal outside a global":  `var pows [60]Int; func Process(o, in []byte) uint64 { return [60]Int{}[0][0] + pows[0][0] }`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Check(globalsProgram(unitFiles("p", largeTablePrefix+src)))
			if err == nil || !strings.HasSuffix(err.Error(), limit) {
				t.Fatalf("got %v, want %q", err, limit)
			}
		})
	}
}
