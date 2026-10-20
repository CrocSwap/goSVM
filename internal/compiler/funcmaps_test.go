package compiler

import (
	"go/types"
	"testing"
)

// Maps combined with function values, closures and defer
// (docs/GO_FUNCTIONS.md, docs/GO_MAPS.md): accepted forms, recursion through
// map-typed signatures, and the backend view.

const funcMapPrefix = `package p
type Counter map[string]uint64
func (c Counter) Inc(k string) { c[k]++ }
func (c *Counter) Reset() { *c = Counter{} }
type Set[T comparable] map[T]struct{}
func (s Set[T]) Put(v T) { s[v] = struct{}{} }
type Op func(a, b uint64) uint64
func add(a, b uint64) uint64 { return a + b }
func fill(m map[uint8]uint64, f func(uint8) uint64) { m[1] = f(1) }
func Process(s, i []byte) uint64 {
`

func TestFuncMapAcceptedForms(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"closure capturing a map":             `m := map[uint8]uint8{}; f := func(k uint8) { m[k]++ }; f(1); f(1); return uint64(m[1])`,
		"closure replacing a map":             `m := map[uint8]uint8{1: 1}; f := func() { m = nil }; f(); return uint64(len(m))`,
		"map of function values":              `t := map[string]Op{"add": add}; t["mul"] = func(a, b uint64) uint64 { return a * b }; return t["add"](1, 2) + t["mul"](2, 3)`,
		"absent function value":               `t := map[string]func(){}; if t["x"] == nil { return 1 }; return 0`,
		"comma-ok function value":             `t := map[int]func() int{1: func() int { return 4 }}; f, ok := t[1]; if ok { return uint64(f()) }; return 0`,
		"function taking a map":               `m := map[uint8]uint64{}; fill(m, func(k uint8) uint64 { return uint64(k) + 1 }); return m[1]`,
		"function value over maps":            `f := func(m map[string]int) map[string]int { m["a"]++; return m }; return uint64(f(f(map[string]int{}))["a"])`,
		"deferred closure on a map":           `m := map[uint8]uint8{}; func() { defer func() { m[1] = 7 }(); m[1] = 1 }(); return uint64(m[1])`,
		"deferred call saving a map":          `m := map[uint8]uint8{}; func() { defer delete2(m, 1); m[1] = 1 }(); return uint64(len(m))`,
		"loop defer saving a map":             `m := map[uint8]uint8{1: 1, 2: 2}; func() { for k := uint8(1); k < 3; k++ { defer delete2(m, k) } }(); return uint64(len(m))`,
		"named map result":                    `return uint64(len(named()))`,
		"value method value of a map":         `c := Counter{}; f := c.Inc; f("a"); return c["a"]`,
		"pointer method value":                `c := Counter{"a": 1}; r := c.Reset; r(); return uint64(len(c))`,
		"method expressions":                  `c := Counter{}; Counter.Inc(c, "x"); (*Counter).Inc(&c, "x"); return c["x"]`,
		"generic set method value":            `st := Set[uint16]{}; p := st.Put; p(3); return uint64(len(st))`,
		"function value in interface key map": `m := map[any]func() uint64{1: func() uint64 { return 3 }}; return m[1]()`,
		"function value slices in a map":      `fs := map[string][]func() uint64{}; fs["a"] = append(fs["a"], func() uint64 { return 2 }); return fs["a"][0]()`,
	} {
		t.Run(name, func(t *testing.T) {
			source := funcMapPrefix + body + "\n}\nfunc delete2(m map[uint8]uint8, k uint8) { delete(m, k) }\nfunc named() (m map[string]bool) { defer func() { m[\"d\"] = true }(); m = map[string]bool{}; return }\n"
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

// Map-typed signatures take part in the recursion check like any other: a
// call through a func(map[K]V) value may run every target of an identical
// signature, and signatures over different map types (or a named map type
// and its underlying type) are different classes.
func TestFuncMapRecursion(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ source, want string }{
		"map closure calling itself": {funcMapPrefix + "var f func(map[string]int) int\nf = func(m map[string]int) int { return f(m) }\nreturn uint64(f(nil))\n}\n",
			"f.go:12:5: recursion is not supported"},
		"same map signature wrapper": {funcMapPrefix + "g := func(m map[uint8]uint64) uint64 { return m[0] }\nw := func(m map[uint8]uint64) uint64 { return g(m) }\nreturn w(nil)\n}\n",
			"f.go:12:6: recursion is not supported"},
		"function stored in a map calls itself": {funcMapPrefix + "t := map[string]func() uint64{}\nt[\"a\"] = func() uint64 { return t[\"a\"]() }\nreturn t[\"a\"]()\n}\n",
			"f.go:12:10: recursion is not supported"},
		"map method value calling its method": {funcMapPrefix + "return 0\n}\nfunc (c Counter) R(k string) { f := c.R; f(k) }\n",
			"f.go:13:1: recursion is not supported"},
		"different map element types": {funcMapPrefix + "g := func(m map[uint8]uint32) uint64 { return uint64(m[0]) }\nw := func(m map[uint8]uint64) uint64 { return g(nil) + m[0] }\nreturn w(nil)\n}\n",
			""},
		"named and unnamed map types": {funcMapPrefix + "g := func(m map[string]uint64) uint64 { return m[\"a\"] }\nw := func(c Counter) uint64 { return g(c) }\nreturn w(Counter{})\n}\n",
			""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := funcMessage(t, c.source); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// The backend view: map-typed signatures form their own FuncClasses, map
// values are captured like any variable, and loop defer records save a map
// as its 8-byte reference.
func TestCheckedProgramFuncMaps(t *testing.T) {
	t.Parallel()
	source := funcMapPrefix + `m := map[uint8]uint64{}
f := func(k uint8) uint64 { m[k]++; return m[k] }
g := func(mm map[uint8]uint64) uint64 { return mm[1] }
for k := uint8(0); k < 2; k++ {
	defer delete3(m, k)
}
return f(1) + g(m)
}
func delete3(m map[uint8]uint64, k uint8) { delete(m, k) }
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "f.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	// map[string]uint64 (Counter, in the prefix) and map[uint8]uint64.
	if len(checked.Maps) != 2 || checked.Maps[0].SlotSize != 32 || checked.Maps[1].SlotSize != 24 {
		t.Fatalf("maps: %+v %+v", *checked.Maps[0], *checked.Maps[1])
	}
	mapParam := 0
	for _, class := range checked.FuncClasses {
		if p := class.Signature.Params(); p.Len() == 1 && isMap(p.At(0).Type()) {
			mapParam++
		}
	}
	if mapParam != 1 {
		t.Fatalf("%d classes over a map parameter", mapParam)
	}
	captured := false
	for _, cl := range checked.Closures {
		for _, v := range cl.Captures {
			if v.Name() == "m" && isMap(v.Type()) {
				captured = true
				if cl.EnvBytes != 16 {
					t.Fatalf("environment %d bytes", cl.EnvBytes)
				}
			}
		}
	}
	if !captured {
		t.Fatal("m is not captured")
	}
	var loop *DeferSite
	for _, d := range checked.DeferSites {
		if d.InLoop {
			loop = d
		}
	}
	if loop == nil || len(loop.Saved) != 2 || !isMap(loop.Saved[0]) || types.TypeString(loop.Saved[1], nil) != "uint8" || loop.RecordBytes != 32 {
		t.Fatalf("loop defer: %+v", loop)
	}
}
