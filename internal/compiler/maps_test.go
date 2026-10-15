package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// Check-level tables, the backend view and determinism for maps
// (docs/GO_MAPS.md).

const mapPrefix = `package p
type P struct{ X, Y int16 }
type K struct {
	A uint8
	S string
	B [2]int32
}
type Set map[string]struct{}
func (s Set) Add(k string) { s[k] = struct{}{} }
func (s Set) Has(k string) bool { _, ok := s[k]; return ok }
type Counter[T comparable] struct{ m map[T]int }
func (c *Counter[T]) Inc(k T) int { if c.m == nil { c.m = map[T]int{} }; c.m[k]++; return c.m[k] }
func Get[K comparable, V any](m map[K]V, k K) (V, bool) { v, ok := m[k]; return v, ok }
func Size[M ~map[K]V, K comparable, V any](m M) int { return len(m) }
type Shape interface{ Area() uint64 }
type Sq uint64
func (q Sq) Area() uint64 { return uint64(q * q) }
type Node struct {
	Kids map[uint8]*Node
	Tags map[string]uint64
}
func build() map[string]int { return map[string]int{"a": 1} }
type T struct {
	V any
	N int
}
type I interface{ Has(string) bool }
type Z struct {
	A uint64
	E struct{}
}
type Tree map[string]Tree
func Process(s, i []byte) uint64 {
`

func mapMessage(t *testing.T, source string) string { return allocMessage(t, source) }

func TestMapAcceptedForms(t *testing.T) {
	for name, body := range map[string]string{
		"make and index":       `m := make(map[uint64]uint64); m[1] = 2; s[0] = byte(m[1] + m[3])`,
		"make hint":            `m := make(map[int8]uint8, i[0]); neg := -1; n := make(map[uint32]bool, neg); m[-1] = 1; n[1] = true; s[0] = byte(len(m) + len(n))`,
		"hint types":           `a := make(map[int]int, int64(i[0])); b := make(map[int]int, uint64(i[1])); c := make(map[int]int, int8(i[2])); s[0] = byte(len(a) + len(b) + len(c))`,
		"literal":              `m := map[string]int{"a": 1, "b": 2, string(i[:1]): 3}; s[0] = byte(m["a"])`,
		"empty literal":        `m := map[K]P{}; m[K{A: 1}] = P{1, 2}; s[0] = byte(m[K{A: 1}].Y)`,
		"nil map":              `var m map[string]uint64; delete(m, "x"); clear(m); if m == nil && len(m) == 0 { s[0] = byte(m["x"]) }`,
		"comma ok":             `m := map[uint16]string{1: "x"}; v, ok := m[1]; var w string; var ok2 bool; w, ok2 = m[2]; _, ok3 := m[3]; var v4, ok4 = m[4]; if ok && !ok2 && !ok3 && !ok4 { s[0] = v[0] + byte(len(w)+len(v4)) }`,
		"comma ok in if":       `m := map[int]int{}; if v, ok := m[1]; !ok { s[0] = byte(v) }`,
		"delete clear":         `m := map[int32]int64{1: 1}; delete(m, 1); m[2] = 2; clear(m); s[0] = byte(len(m))`,
		"inc dec":              `m := map[uint8]int8{}; m[1]++; m[1]--; m[2]--; u := map[string]uint32{}; u["x"]++; s[0] = byte(m[2]) + byte(u["x"])`,
		"string add assign":    `m := map[int]string{}; m[1] += "a"; m[1] += string(i[:2]); s[0] = byte(len(m[1]))`,
		"multi assign":         `m := map[int]int{}; x := 1; m[1], x = x, 2; m[2], m[3] = m[1], x; s[0] = byte(m[2] + m[3])`,
		"struct keys":          `m := map[K]uint64{}; k := K{A: 1, S: "s", B: [2]int32{-1, 2}}; m[k] = 3; s[0] = byte(m[k])`,
		"array keys":           `m := map[[3]byte]int{}; m[[3]byte{1, 2, 3}] = 4; var a [3]byte; copy(a[:], i); m[a]++; s[0] = byte(m[a])`,
		"string array keys":    `m := map[[2]string]bool{}; m[[2]string{"a", ""}] = true; s[0] = byte(len(m))`,
		"bool keys":            `m := map[bool]int16{}; m[i[0] > 3] = -1; s[0] = byte(m[true])`,
		"pointer keys":         `a, b := new(P), new(P); m := map[*P]int{a: 1}; m[b] = 2; s[0] = byte(m[a] + m[b])`,
		"interface keys":       `m := map[any]int{}; m[1] = 1; m["x"] = 2; m[P{1, 2}] = 3; m[nil] = 4; m[Sq(2)] = 5; s[0] = byte(m[1] + m[P{1, 2}])`,
		"named interface keys": `m := map[Shape]int{Sq(1): 1}; var x Shape = Sq(2); m[x]++; delete(m, Sq(1)); s[0] = byte(m[x])`,
		"interface in struct":  `m := map[T]bool{}; m[T{V: 1}] = true; s[0] = byte(len(m))`,
		"interface values":     `m := map[string]any{"a": 1, "b": "x"}; if v, ok := m["a"].(int); ok { s[0] = byte(v) }; var e any; e, _ = m["c"]; _ = e`,
		"map values":           `m := map[string]map[string]int{}; m["a"] = map[string]int{}; m["a"]["b"] = 1; m["a"]["b"]++; s[0] = byte(m["a"]["b"])`,
		"slice values":         `m := map[int][]byte{}; m[1] = append(m[1], 1, 2); m[1][0] = 3; s[0] = m[1][0] + byte(len(m[1]))`,
		"struct values":        `m := map[int]P{1: {1, 2}}; p := m[1]; p.X = 5; m[1] = p; s[0] = byte(m[1].X)`,
		"array values":         `m := map[int][4]int8{}; m[1] = [4]int8{1, -2}; a := m[1]; s[0] = byte(a[1] + m[1][0])`,
		"pointer values":       `m := map[int]*P{1: {1, 2}}; m[1].X = 4; s[0] = byte(m[1].X)`,
		"empty struct values":  `m := map[string]struct{}{"a": {}}; m["b"] = struct{}{}; _, ok := m["a"]; if ok { s[0] = byte(len(m)) }; v := m["c"]; _ = v`,
		"empty struct keys":    `m := map[struct{}]int{}; m[struct{}{}] = 1; m[struct{}{}]++; delete(m, struct{}{}); s[0] = byte(len(m))`,
		"named map":            `x := Set{}; x.Add("a"); if x.Has("a") { s[0] = 1 }; y := Set(nil); s[1] = byte(len(y))`,
		"map conversion":       `x := map[string]struct{}(Set{"a": {}}); y := Set(x); z := map[string]struct{}(nil); s[0] = byte(len(y) + len(z))`,
		"struct fields":        `n := Node{Kids: map[uint8]*Node{}}; n.Kids[1] = &Node{}; n.Kids[1].Tags = map[string]uint64{"a": 2}; s[0] = byte(n.Kids[1].Tags["a"])`,
		"slices of maps":       `ms := make([]map[int]int, 2); ms[0] = map[int]int{}; ms[0][1] = 2; ms = append(ms, nil); s[0] = byte(ms[0][1] + len(ms))`,
		"arrays of maps":       `var a [2]map[int]int; a[0] = map[int]int{1: 1}; b := a; b[0][1] = 5; s[0] = byte(a[0][1])`,
		"pointers to maps":     `m := map[int]int{}; p := &m; (*p)[1] = 2; q := new(map[int]int); *q = m; s[0] = byte((*q)[1])`,
		"maps in interfaces":   `var a any = map[string]int{"a": 1}; if m, ok := a.(map[string]int); ok { m["b"] = 2; s[0] = byte(len(m)) }; switch a.(type) { case map[string]int: s[1] = 1 }`,
		"named map interface":  `var x I = Set{"a": {}}; if x.Has("a") { s[0] = 1 }`,
		"generics":             `c := &Counter[string]{}; c.Inc("a"); m := map[int8]P{-1: {1, 1}}; v, ok := Get(m, -1); if ok { s[0] = byte(v.X) }; s[1] = byte(Size(Set{}) + Size(m))`,
		"generic counters":     `c := Counter[K]{}; c.Inc(K{S: "x"}); d := Counter[any]{}; d.Inc(1); s[0] = byte(c.Inc(K{S: "x"}))`,
		"function results":     `m := build(); m["b"] = 2; s[0] = byte(m["a"] + build()["a"])`,
		"signed values":        `m := map[int64]int64{-1: -1}; m[-1]--; m[-2] = m[-1] * 3; s[0] = byte(m[-2])`,
		"len of field":         `n := Node{}; s[0] = byte(len(n.Tags) + len(n.Kids))`,
		"compare nil":          `var m map[int]int; if m != nil || nil == m { s[0] = 1 }`,
		"escaping key":         `x := P{1, 2}; m := map[*P]bool{&x: true}; y := P{}; m[&y] = true; s[0] = byte(len(m))`,
		"escaping value":       `m := map[int]*P{}; for j := 0; j < 2; j++ { v := P{X: int16(j)}; m[j] = &v }; s[0] = byte(m[1].X)`,
		"uint16 keys":          `m := make(map[uint16]uint16); _ = m; s[0] = 1`,
		"recursive map type":   `t := Tree{"a": {"b": nil}}; t["a"]["c"] = Tree{}; s[0] = byte(len(t["a"]) + len(t["a"]["b"]))`,
	} {
		t.Run(name, func(t *testing.T) {
			source := mapPrefix + body + "\nreturn 0 }\n"
			if got := mapMessage(t, source); got != "" {
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

func TestMapRejections(t *testing.T) {
	const noRange = rangeMapRule
	cases := map[string][2]string{
		"range key value":    {`m := map[int]int{}; for k, v := range m { _, _ = k, v }`, noRange},
		"range key":          {`m := map[int]int{}; for k := range m { _ = k }`, noRange},
		"range bare":         {`m := map[int]int{}; for range m {}`, noRange},
		"range assign":       {`m := map[int]int{}; var k, v int; for k, v = range m {}; _, _ = k, v`, noRange},
		"range blank":        {`for _, v := range map[string]int{} { _ = v }`, noRange},
		"range named map":    {`for k := range (Set{}) { _ = k }`, noRange},
		"range field":        {`var n Node; for k := range n.Tags { _ = k }`, noRange},
		"range call":         {`for k := range build() { _ = k }`, noRange},
		"address of element": {`m := map[int]int{}; p := &m[1]; _ = p`, "invalid operation: cannot take address of m[1] (map index expression of type int)"},
		"field of element":   {`m := map[int]P{}; m[1].X = 2`, "cannot assign to struct field m[1].X in map"},
		"compare maps":       {`a, b := map[int]int{}, map[int]int{}; _ = a == b`, "invalid operation: a == b (map can only be compared to nil)"},
		"map key":            {`var m map[map[int]int]int; _ = m`, "invalid map key type map[int]int"},
		"slice key":          {`var m map[[]byte]int; _ = m`, "invalid map key type []byte"},
		"clear slice":        {`clear(s)`, "clear supports only maps"},
		"compound add":       {`m := map[int]int{}; m[1] += 2`, "compound assignment unsupported"},
		"float values":       {`var m map[int]float64; _ = m`, "unsupported type float64"},
		"function values":    {`var m map[int]func(); _ = m`, "unsupported type func()"},
		"empty struct field": {"var m map[int]Z; _ = m", "map keys and values must not contain zero-length arrays or empty structs"},
		"zero array key":     {"var m map[[0]byte]int; m[[0]byte{}] = 1; var n map[int][0]byte; _ = n; s[0] = byte(len(m))", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			source := mapPrefix + c[0] + "\nreturn 0 }\n"
			if got := mapMessage(t, source); got != c[1] {
				t.Fatalf("got  %q\nwant %q", got, c[1])
			}
			if _, err := Compile("alloc.go", []byte(source)); (err == nil) != (c[1] == "") || err != nil && !strings.HasSuffix(err.Error(), c[1]) {
				t.Fatalf("CompileProgram disagrees with Check: %v", err)
			}
		})
	}
	// Range over a map is rejected inside generic code instantiated with a
	// map type, naming the instantiation.
	for _, c := range [][2]string{
		{"Keys(map[string]int{})", "instantiation Keys[map[string]int, string, int]: alloc.go:2:73: "},
		{"Keys(Named{})", "instantiation Keys[Named, uint8, bool]: alloc.go:2:73: "},
		{"Sum(map[uint16]uint64{1: 2})", "instantiation Sum[uint16]: alloc.go:3:83: "},
	} {
		generic := "package p\nfunc Keys[M ~map[K]V, K comparable, V any](m M) int { n := 0; for range m { n++ }; return n }\n" +
			"func Sum[K comparable](m map[K]uint64) uint64 { t := uint64(0); for _, v := range m { t = t + v }; return t }\n" +
			"type Named map[uint8]bool\nfunc Process(s, i []byte) uint64 { return uint64(" + c[0] + ") }\n"
		if got := mapMessage(t, generic); got != c[1]+noRange {
			t.Errorf("generic range %s: %q", c[0], got)
		}
	}
}

// mapHeapModel replays the documented heap accounting (docs/GO_MAPS.md) of
// examples/map-values mode 6: an 8-byte allocator counter, then allocations
// rounded up to 8 bytes; a map header is 24 bytes; make with a positive hint
// h allocates slots for gosvm_map_cap(h); a new key that would take the
// table past 3/4 full doubles it (8 slots when there is none).
type mapHeapModel struct {
	used, budget uint64
}

func (h *mapHeapModel) alloc(n uint64) bool {
	n = (n + 7) &^ 7
	if n > h.budget-h.used {
		return false
	}
	h.used += n
	return true
}

type mapModel struct {
	count, cap, slot uint64
	keys             map[string]bool
}

func mapCapFor(n uint64) uint64 {
	c := uint64(8)
	for n*4 > c*3 {
		c <<= 1
	}
	return c
}

func (h *mapHeapModel) makeMap(hint int64, slot uint64) (*mapModel, bool) {
	m := &mapModel{slot: slot, keys: map[string]bool{}}
	if !h.alloc(mapHeaderBytes) {
		return nil, false
	}
	if hint > 0 {
		m.cap = mapCapFor(uint64(hint))
		if !h.alloc(m.cap * slot) {
			return nil, false
		}
	}
	return m, true
}

func (h *mapHeapModel) insert(m *mapModel, key string) bool {
	if m.keys[key] {
		return true
	}
	if (m.count+1)*4 > m.cap*3 {
		next := m.cap * 2
		if next == 0 {
			next = 8
		}
		if !h.alloc(next * m.slot) {
			return false
		}
		m.cap = next
	}
	m.keys[key] = true
	m.count++
	return true
}

func (m *mapModel) remove(key string) {
	if m.keys[key] {
		delete(m.keys, key)
		m.count--
	}
}

// mapHeapRun reports whether mode 6 with n insertions of kind fits budget.
func mapHeapRun(kind byte, n, hint int, budget uint64) bool {
	h := &mapHeapModel{used: 8, budget: budget}
	switch kind {
	case 0, 1:
		if kind == 0 {
			hint = 0
		}
		m, ok := h.makeMap(int64(hint), 24)
		if !ok {
			return false
		}
		for j := 0; j < n; j++ {
			if !h.insert(m, fmt.Sprint(j)) {
				return false
			}
		}
	case 2:
		m, ok := h.makeMap(0, 24) // map[string]struct{}: 8 + 16 + 0
		if !ok {
			return false
		}
		const alphabet = "abcabcdeffedcbaxyz"
		for j := 0; j < n; j++ {
			lo, l := j%7, (j/7)%9
			a, b := alphabet[lo:lo+l], alphabet[0:(j/63)%16]
			if a != "" && b != "" && !h.alloc(uint64(len(a)+len(b))) {
				return false // the concatenated key, before the insertion
			}
			if !h.insert(m, a+b) {
				return false
			}
		}
	case 3:
		m, ok := h.makeMap(0, 24) // map[uint32]uint8: 8 + 8 + 8
		if !ok {
			return false
		}
		for j := 0; j < n; j++ {
			key := fmt.Sprint(j % 5)
			if !h.insert(m, key) {
				return false
			}
			m.remove(key)
		}
	default:
		// make(map[uint8]uint8) headers appended to a []map[uint8]uint8
		// (8-byte pointer-holding elements, Go 1.22 growth).
		length, capacity := uint64(0), uint64(0)
		for j := 0; j < n; j++ {
			if !h.alloc(mapHeaderBytes) {
				return false
			}
			if length == capacity {
				capacity = modelCap(length, capacity, 1, 8, true)
				if !h.alloc(capacity * 8) {
					return false
				}
			}
			length++
		}
	}
	return true
}

// mapNoLimit is mapHeapLimit's result when 20,000 insertions fit.
const mapNoLimit = 20000

// mapHeapLimit is the largest n that fits (-1 when make itself runs out of
// memory), or mapNoLimit.
func mapHeapLimit(kind byte, hint int, budget uint64) int {
	for n := 0; n < mapNoLimit; n++ {
		if !mapHeapRun(kind, n, hint, budget) {
			return n - 1
		}
	}
	return mapNoLimit
}

func mapHeapInput(kind byte, n, hint int) []byte {
	in := make([]byte, 128)
	in[0], in[1], in[2], in[3], in[4], in[5], in[6] = 6, byte(n), byte(n>>8), byte(n>>16), kind, byte(hint), byte(hint>>8)
	return in
}

// mapHeapKinds are the heap boundary cases: kind and make hint.
var mapHeapKinds = [][2]int{{0, 0}, {1, 1}, {1, 7}, {1, 384}, {1, 385}, {1, 768}, {1, 769}, {2, 0}, {3, 0}, {4, 0}}

// TestMapHeap checks the exact out-of-memory point of map growth with the
// default 32 KiB heap against the documented accounting.
func TestMapHeap(t *testing.T) {
	bin := hostCompile(t, compileMaps(t, 0), DefaultHeapBytes, allocDriver, false)
	oom := fmt.Sprintf("P:%x\n", make([]byte, 1024))
	checked := 0
	for _, kh := range mapHeapKinds {
		kind, hint := byte(kh[0]), kh[1]
		limit := mapHeapLimit(kind, hint, DefaultHeapBytes)
		if limit == mapNoLimit {
			in := mapHeapInput(kind, limit, hint)
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", in)), nativeMapCall(in); got != want {
				t.Errorf("kind %d hint %d: %d: %.16s, want %.16s", kind, hint, limit, got, want)
			}
			t.Logf("kind %d hint %d: %d insertions fit", kind, hint, limit)
			checked++
			continue
		}
		if limit >= 0 {
			fits := mapHeapInput(kind, limit, hint)
			if got, want := runHost(t, bin, fmt.Sprintf("%x\n", fits)), nativeMapCall(fits); got != want {
				t.Errorf("kind %d hint %d: %d keys: %.16s, want %.16s", kind, hint, limit, got, want)
			}
		}
		over := mapHeapInput(kind, limit+1, hint)
		if got := runHost(t, bin, fmt.Sprintf("%x\n", over)); got != oom {
			t.Errorf("kind %d hint %d: %d keys: %.16s, want out of memory", kind, hint, limit+1, got)
		}
		if got := nativeMapCall(over); strings.HasPrefix(got, "P:") {
			t.Errorf("kind %d: native Go panicked", kind)
		}
		t.Logf("kind %d hint %d: %d insertions fit, %d run out of memory", kind, hint, limit, limit+1)
		checked += 2
	}
	// The documented examples: 384 uint64 keys fit (24,416 bytes in use),
	// the 385th needs 1,024 slots of 24 bytes; make with a hint of 385 to
	// 768 allocates those 1,024 slots at once, so 768 keys fit; a hint of
	// 769 needs 2,048 slots and make itself runs out of memory.
	for _, c := range [][3]int{{0, 0, 384}, {1, 385, 768}, {1, 768, 768}, {1, 769, -1}} {
		if got := mapHeapLimit(byte(c[0]), c[1], DefaultHeapBytes); got != c[2] {
			t.Errorf("kind %d hint %d: limit %d, want %d", c[0], c[1], got, c[2])
		}
	}
	t.Logf("%d heap boundaries match the documented accounting", checked)
}

// TestCheckedProgramMaps: the backend view lists every map type with its
// sizes, slot size and whether hashing a key can abort.
func TestCheckedProgramMaps(t *testing.T) {
	source := `package p
type K struct {
	A uint8
	S string
}
type Set map[string]struct{}
type IK struct{ V any }
func Process(s, i []byte) uint64 {
	a := map[uint64]uint64{}
	b := Set{}
	c := map[K][3]uint16{}
	d := map[any]bool{}
	e := map[IK]map[byte]bool{}
	f := map[[2]any]uint8{}
	g := map[uint8]map[uint8]bool{}
	return uint64(len(a) + len(b) + len(c) + len(d) + len(e) + len(f) + len(g))
}
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "a.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i, mt := range checked.Maps {
		if mt.Index != i {
			t.Errorf("%s: index %d at %d", mt.Key, mt.Index, i)
		}
		got = append(got, fmt.Sprintf("%s key=%d elem=%d slot=%d iface=%v", mt.Key, mt.KeySize, mt.ElemSize, mt.SlotSize, mt.KeyInterfaces))
	}
	want := []string{
		"map[[2]interface{}]uint8 key=32 elem=1 slot=48 iface=true",
		"map[interface{}]bool key=16 elem=1 slot=32 iface=true",
		"map[program.IK]map[uint8]bool key=16 elem=8 slot=32 iface=true",
		"map[program.K][3]uint16 key=24 elem=6 slot=40 iface=false",
		"map[string]struct{} key=16 elem=0 slot=24 iface=false",
		"map[uint64]uint64 key=8 elem=8 slot=24 iface=false",
		"map[uint8]bool key=1 elem=1 slot=24 iface=false",
		"map[uint8]map[uint8]bool key=1 elem=8 slot=24 iface=false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("maps:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// A program without maps has none.
	plain, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "a.go", Data: []byte("package p\nfunc Process(s, i []byte) uint64 { return 0 }\n")}}}}})
	if err != nil || len(plain.Maps) != 0 {
		t.Fatalf("plain program: %v %d", err, len(plain.Maps))
	}
}

// TestMapRuntimeOnlyWhenUsed: programs without maps carry no map runtime;
// the C of a map program is deterministic.
func TestMapRuntimeOnlyWhenUsed(t *testing.T) {
	without, err := Compile("a.go", []byte("package p\nfunc Process(s, i []byte) uint64 { b := make([]byte, 2); return uint64(len(b)) }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "gosvm_map") || strings.Contains(string(without), "gosvm_empty") {
		t.Fatal("a program without maps got the map runtime")
	}
	with, err := Compile("a.go", []byte("package p\nfunc Process(s, i []byte) uint64 { m := map[uint8]uint8{1: 2}; return uint64(m[i[0]]) }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), "gosvm_map0_find") || !strings.Contains(string(with), "gosvm_alloc") || strings.Contains(string(with), "gosvm_hstr") {
		t.Fatal("a map program lacks the map or heap runtime, or has the string hash")
	}
	source := mapSource(t)
	first := compileMaps(t, 0)
	for k := 0; k < 3; k++ {
		again, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "program.go", Data: source}}}}})
		if err != nil || string(again) != string(first) {
			t.Fatalf("map program C is not deterministic: %v", err)
		}
	}
}

// TestMapDocumentationExample compiles and runs the example in
// docs/GO_MAPS.md.
func TestMapDocumentationExample(t *testing.T) {
	doc := string(mustRead(t, "../../docs/GO_MAPS.md"))
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

// TestMapHeapMoves: a reference stored into a map (as a key or an element,
// by assignment or in a literal) moves its local to the heap; lookups and
// deletes do not; elements read from a map are heap storage.
func TestMapHeapMoves(t *testing.T) {
	source := `package p
type P struct{ X uint64 }
func get(m map[int]*uint64) *uint64 { v, _ := m[1]; return v }
func first(m map[int][]byte) []byte { return m[1] }
func Process(s, i []byte) uint64 {
	a := P{1}
	m := map[*P]int{}
	m[&a] = 1
	b := P{2}
	n := map[*P]bool{&b: true}
	c := uint64(3)
	o := map[int]*uint64{1: &c}
	d := uint64(4)
	o[2] = &d
	e := P{5}
	_ = m[&e]
	f := P{6}
	delete(n, &f)
	var g [4]byte
	q := map[int][]byte{1: g[:]}
	return uint64(len(m)+len(n)+len(q)) + *get(o) + uint64(len(first(q)))
}
`
	checked, err := Check(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "a.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, fn := range checked.Functions {
		var vars []string
		for _, v := range fn.HeapVars {
			vars = append(vars, v.Name())
		}
		got[fn.Object.Name()] = fmt.Sprintf("vars=%v results=%+v", vars, fn.Borrows.Results)
	}
	want := map[string]string{
		"get":     "vars=[] results=[{Parameters:[] SDK:false Heap:true}]",
		"first":   "vars=[] results=[{Parameters:[] SDK:false Heap:true}]",
		"Process": "vars=[a b c d g] results=[{Parameters:[] SDK:false Heap:false}]",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: got %s, want %s", name, got[name], w)
		}
	}
	c, err := CompileProgram(&Program{Entry: "program", Packages: []PackageSources{{"program", []Source{{Name: "a.go", Data: []byte(source)}}}}})
	if err != nil {
		t.Fatal(err)
	}
	hostCompile(t, c, DefaultHeapBytes, "", true)
}
