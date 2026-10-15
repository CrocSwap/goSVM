package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

// Maps. A map value is a pointer to a header on the invocation heap (nil for
// a nil map), so copies share the table, as in Go. The table is open
// addressing with linear probing over a power-of-two slot array, a fixed-seed
// hash of the key's canonical value and backward-shift deletion (no
// tombstones), so heap use depends only on the sequence of live-key counts.
// Iteration is not supported: Go randomizes its order. See docs/GO_MAPS.md.

// MapType is one map type the program uses, keyed by its unnamed type.
type MapType struct {
	// Type is the map type itself (the underlying type of a named map type).
	Type *types.Map
	// Key is typeKey(Type): identical map types share it.
	Key string
	// Index is the position in CheckedProgram.Maps (sorted by Key); the C
	// backend names its helpers gosvm_map<Index>_*.
	Index int
	// KeySize and ElemSize are the Go sizes of the key and element types.
	KeySize, ElemSize int64
	// SlotSize is the bytes of one table slot: an 8-byte hash word, then the
	// key and the element, each rounded up to 8 bytes (a zero-size key or
	// element takes no space). Heap accounting is in these units.
	SlotSize int64
	// KeyInterfaces reports that the key holds interface values (directly or
	// in fields or elements): hashing aborts for an uncomparable dynamic
	// type, as Go panics, so every operation hashes the key, even on a nil
	// or empty map.
	KeyInterfaces bool
}

// isEmptyStruct reports whether t is the unnamed empty struct type struct{}.
func isEmptyStruct(t types.Type) bool {
	st, ok := t.(*types.Struct)
	return ok && st.NumFields() == 0
}

// emptyStructRuntime is the C type of struct{} (C has no empty structs).
const emptyStructRuntime = `
typedef struct { u8 unused; } gosvm_empty;
`

// mapHeaderBytes is the heap size of a map header (count, capacity, slots).
const mapHeaderBytes = 24

func roundUp8(n int64) int64 { return (n + 7) &^ 7 }

// isMap reports whether t is a map type (or a named one).
func isMap(t types.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(*types.TypeParam); ok {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// containsIface reports whether values of t hold interface values by value.
func containsIface(t types.Type) bool {
	if isIface(t) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Array:
		return containsIface(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if containsIface(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// mapRule admits map[K]V for storable K and V. go/types has already required
// K to be comparable. Keys and elements must not hold zero-length arrays or
// empty structs unless they have size zero (their Go and C sizes could
// differ otherwise).
func (f *frontend) mapRule(n ast.Node, m *types.Map) {
	key := typeKey(m)
	if f.maps == nil {
		f.maps = map[string]*MapType{}
	}
	if f.maps[key] != nil {
		return
	}
	sizes := &types.StdSizes{WordSize: 8, MaxAlign: 8}
	mt := &MapType{Type: m, Key: key, KeySize: sizes.Sizeof(m.Key()), ElemSize: sizes.Sizeof(m.Elem()), KeyInterfaces: containsIface(m.Key())}
	mt.SlotSize = 8 + roundUp8(mt.KeySize) + roundUp8(mt.ElemSize)
	f.maps[key] = mt // before the element rules: map types may be recursive
	for _, t := range []types.Type{m.Key(), m.Elem()} {
		if isContext(t) {
			f.fail(n, "maps of Context unsupported")
		}
		if _, ok := t.(*types.Tuple); ok {
			f.fail(n, "unsupported type %s", m)
		}
		f.typeArgRule(n, t)
		if sizes.Sizeof(t) != 0 && zeroSizePart(t) {
			f.fail(n, "map keys and values must not contain zero-length arrays or empty structs")
		}
	}
}

// mapProgram is the program's map types.
type mapProgram struct {
	types []*MapType
	byKey map[string]*MapType
}

// sortedMaps orders the recorded map types by key and numbers them.
func sortedMaps(recorded map[string]*MapType) *mapProgram {
	p := &mapProgram{byKey: recorded}
	for _, mt := range recorded {
		p.types = append(p.types, mt)
	}
	sort.Slice(p.types, func(i, j int) bool { return p.types[i].Key < p.types[j].Key })
	for i, mt := range p.types {
		mt.Index = i
	}
	return p
}

// isMapIndex reports whether e is m[k] on a map.
func (f *frontend) isMapIndex(e ast.Expr) (*ast.IndexExpr, bool) {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	return ix, ok && isMap(f.info.Types[ix.X].Type)
}

// mapOf is the map type of a map-typed expression's type.
func mapOf(t types.Type) *types.Map { return t.Underlying().(*types.Map) }

// Checker rules.

// rangeMapRule is the rejection of every range over a map.
const rangeMapRule = "range over maps unsupported: Go randomizes map iteration order, so it cannot be deterministic on-chain; keep the keys in a slice and range over that"

// mapIndex checks m[k] and v, ok := m[k]: the map, then the key.
func (c *checker) mapIndex(e *ast.IndexExpr, tv types.TypeAndValue) {
	c.expr(e.X)
	c.expr(e.Index)
	if tuple, ok := tv.Type.(*types.Tuple); ok {
		for i := 0; i < tuple.Len(); i++ {
			c.typeRule(e, tuple.At(i).Type())
		}
		return
	}
	c.typeRule(e, tv.Type)
}

// mapLiteral checks map[K]V{k: v, ...}: keys and values in source order.
func (c *checker) mapLiteral(e *ast.CompositeLit, t types.Type) {
	c.typeRule(e, t)
	for _, elt := range e.Elts {
		kv := elt.(*ast.KeyValueExpr)
		c.expr(kv.Key)
		c.expr(kv.Value)
	}
}

// mapBuiltin checks delete(m, k), clear(m) and len(m): the operands in
// order. clear of a slice remains unsupported.
func (c *checker) mapBuiltin(e *ast.CallExpr, name string, tv types.TypeAndValue) {
	if !isMap(c.info.Types[e.Args[0]].Type) {
		c.fail(e, "clear supports only maps")
	}
	for _, arg := range e.Args {
		c.expr(arg)
	}
	if name == "len" {
		c.typeRule(e, tv.Type)
	}
}

// mapConversion checks M(x) between map types with identical underlying
// types, or M(nil).
func (c *checker) mapConversion(e *ast.CallExpr, tv types.TypeAndValue) {
	if len(e.Args) != 1 {
		c.fail(e, "invalid map conversion")
	}
	if !c.info.Types[e.Args[0]].IsNil() {
		c.expr(e.Args[0])
	}
	c.typeRule(e, tv.Type)
}

// Emitter.

// mapRuntime is emitted for programs with map values, after the heap
// runtime: the header, hashing primitives and the type-independent table
// operations. Heap accounting is contracted (docs/GO_MAPS.md).
const mapRuntime = `
/* Go map: a heap header (nil for a nil map) holding the live-key count and a
   power-of-two array of slots (cap 0: none yet). A slot is a hash word (0:
   empty; occupied slots have bit 63 set), the key and the element, each
   rounded up to 8 bytes. Linear probing; deletion shifts later entries back,
   so there are no tombstones. Iteration is not supported. */
typedef struct { i64 count; u64 cap; u8 *slots; } gosvm_map;
#define GOSVM_MAP_USED (1ULL << 63)
static inline u64 gosvm_hmix(u64 h, u64 w) { h = (h ^ w) * 0x9E3779B97F4A7C15ULL; return h ^ (h >> 32); }
static inline u64 gosvm_hfin(u64 h) { h ^= h >> 31; h *= 0xBF58476D1CE4E5B9ULL; h ^= h >> 29; return h | GOSVM_MAP_USED; }
/* The smallest power of two >= 8 that holds n keys at load factor 3/4. */
static u64 gosvm_map_cap(u64 n) { u64 c = 8; while (n * 4 > c * 3) c <<= 1; return c; }
/* make(map[K]V, hint): the header, and slots for hint keys when hint > 0
   (Go treats a negative or oversized hint as 0). */
static gosvm_map *gosvm_map_make(i64 hint, u64 slot) {
    gosvm_map *m = (gosvm_map *)gosvm_alloc(sizeof(gosvm_map));
    if (hint > 0 && (u64)hint <= (1ULL << 48) / slot) {
        m->cap = gosvm_map_cap((u64)hint);
        m->slots = gosvm_alloc(m->cap * slot);
    }
    return m;
}
static inline i64 gosvm_map_len(gosvm_map *m) { return m ? m->count : 0; }
/* clear(m): every slot empty; the slot array is kept. */
static void gosvm_map_clear(gosvm_map *m, u64 slot) {
    if (!m || m->count == 0) return;
    for (u64 i = 0; i < m->cap; i++) *(u64 *)(m->slots + i * slot) = 0;
    m->count = 0;
}
static inline void gosvm_map_copy(u8 *dst, const u8 *src, u64 slot) {
    for (u64 w = 0; w < slot / 8; w++) ((u64 *)dst)[w] = ((const u64 *)src)[w];
}
/* Double the slot array (8 slots when there is none) and reinsert the live
   entries by their stored hashes. The old array is not freed. */
static void gosvm_map_grow(gosvm_map *m, u64 slot) {
    u64 cap = m->cap ? m->cap * 2 : 8, mask = cap - 1;
    if (cap > GOSVM_HEAP_BYTES / slot) abort();
    u8 *slots = gosvm_alloc(cap * slot);
    for (u64 i = 0; i < m->cap; i++) {
        const u8 *s = m->slots + i * slot;
        u64 h = *(const u64 *)s;
        if (!h) continue;
        u64 j = h & mask;
        while (*(u64 *)(slots + j * slot)) j = (j + 1) & mask;
        gosvm_map_copy(slots + j * slot, s, slot);
    }
    m->cap = cap;
    m->slots = slots;
}
/* Remove the entry in slot i: later entries of the probe run move back into
   the hole unless their home slot lies cyclically in (i, j]. */
static void gosvm_map_remove(gosvm_map *m, u64 i, u64 slot) {
    u64 mask = m->cap - 1, j = i;
    m->count--;
    for (;;) {
        j = (j + 1) & mask;
        u8 *s = m->slots + j * slot;
        u64 h = *(u64 *)s;
        if (!h) break;
        u64 k = h & mask;
        if (i <= j ? (i < k && k <= j) : (i < k || k <= j)) continue;
        gosvm_map_copy(m->slots + i * slot, s, slot);
        i = j;
    }
    *(u64 *)(m->slots + i * slot) = 0;
}
`

// stringHashRuntime hashes a string's length and bytes.
const stringHashRuntime = `
static u64 gosvm_hstr(u64 h, gosvm_string s) {
    h = gosvm_hmix(h, (u64)s.len);
    u64 w = 0, n = 0;
    for (u64 i = 0; i < (u64)s.len; i++) {
        w |= (u64)s.ptr[i] << (8 * n);
        if (++n == 8) { h = gosvm_hmix(h, w); w = 0; n = 0; }
    }
    return n ? gosvm_hmix(h, w) : h;
}
`

// mapFor is the program's entry for a map-typed value's type.
func (g *generator) mapFor(n ast.Node, t types.Type) *MapType {
	if g.maps == nil || !isMap(t) {
		g.unchecked(n)
	}
	mt := g.maps.byKey[typeKey(mapOf(t))]
	if mt == nil {
		g.unchecked(n)
	}
	return mt
}

// hashCode is C statements that mix the canonical value of x (a C lvalue of
// type t) into h: integers and bools by value, pointers by address, strings
// by length and bytes, arrays by element and structs by field (no padding),
// interfaces by dynamic type ID and value.
func (g *generator) hashCode(n ast.Node, t types.Type, x string, depth int) string {
	if isIface(t) {
		return fmt.Sprintf("h = gosvm_iface_hash(h,%s);", x)
	}
	if isString(t) {
		return fmt.Sprintf("h = gosvm_hstr(h,%s);", x)
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		return fmt.Sprintf("h = gosvm_hmix(h,(u64)(__UINTPTR_TYPE__)%s);", x)
	case *types.Basic:
		return fmt.Sprintf("h = gosvm_hmix(h,(u64)%s);", x)
	case *types.Array:
		if u.Len() == 0 {
			return ""
		}
		i := fmt.Sprintf("i%d", depth)
		return fmt.Sprintf("for (u64 %s=0;%s<%dULL;%s++) { %s }", i, i, u.Len(), i, g.hashCode(n, u.Elem(), x+".items["+i+"]", depth+1))
	case *types.Struct:
		var parts []string
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if part := g.hashCode(n, f.Type(), x+"."+g.name(f), depth); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, " ")
	}
	g.unchecked(n)
	return ""
}

// emitMaps defines each map type's hash, lookup, insertion and deletion, and
// the interface hash when a key holds interfaces.
func (g *generator) emitMaps() {
	node := posNode(0)
	ifaceKeys := false
	for _, mt := range g.maps.types {
		ifaceKeys = ifaceKeys || mt.KeyInterfaces
	}
	if ifaceKeys {
		g.line("static u64 gosvm_iface_hash(u64 h,gosvm_iface x);")
		for _, d := range g.ifaces.dynamic {
			if d.Direct || d.Size == 0 || !d.Comparable {
				continue
			}
			ct := g.ctype(node, d.Type)
			g.line("static u64 gosvm_hashd%d(u64 h,const u8 *p) { /* %s */", d.ID, d.Key)
			g.line("%s\nreturn h;\n}", g.hashCode(node, d.Type, "(*("+ct+" *)p)", 0))
		}
		// Go hashes an interface by its dynamic type and value; an
		// uncomparable dynamic type panics (hash of unhashable type).
		g.line("static u64 gosvm_iface_hash(u64 h,gosvm_iface x) {")
		g.line("h = gosvm_hmix(h,x.type);")
		g.line("if (x.type == 0) return h;")
		for _, d := range g.ifaces.dynamic {
			switch {
			case !d.Comparable:
				g.line("if (x.type == %dULL) abort(); /* unhashable %s */", d.ID, d.Key)
			case d.Direct:
				g.line("if (x.type == %dULL) return gosvm_hmix(h,(u64)(__UINTPTR_TYPE__)x.data);", d.ID)
			case d.Size == 0:
				g.line("if (x.type == %dULL) return h;", d.ID)
			default:
				g.line("if (x.type == %dULL) return gosvm_hashd%d(h,x.data);", d.ID, d.ID)
			}
		}
		g.line("abort();\n}")
	}
	for _, mt := range g.maps.types {
		g.emitMapType(node, mt)
	}
}

func (g *generator) emitMapType(node ast.Node, mt *MapType) {
	n := mt.Index
	kc, kw, vw, slot := g.variable(node, mt.Type.Key()), roundUp8(mt.KeySize), roundUp8(mt.ElemSize), mt.SlotSize
	equal := "1" // a zero-size key: every key is equal
	if mt.KeySize > 0 {
		equal = g.valueEqual(node, mt.Type.Key(), "(*("+kc+" *)(s+8))", "(*k)")
	}
	g.line("/* %s: slot %d bytes */", mt.Key, slot)
	g.line("static u64 gosvm_map%d_hash(%s *k) {", n, kc)
	g.line("u64 h = 0x243F6A8885A308D3ULL;")
	if code := g.hashCode(node, mt.Type.Key(), "(*k)", 0); code != "" {
		g.line("%s", code)
	}
	g.line("return gosvm_hfin(h);\n}")
	// Lookup: the element's storage, or 0 when the key is absent.
	g.line("static u8 *gosvm_map%d_find(gosvm_map *m,%s *k) {", n, kc)
	if mt.KeyInterfaces {
		g.line("u64 h = gosvm_map%d_hash(k);", n)
		g.line("if (!m || m->count == 0) return 0;")
	} else {
		g.line("if (!m || m->count == 0) return 0;")
		g.line("u64 h = gosvm_map%d_hash(k);", n)
	}
	g.line("u64 mask = m->cap - 1;")
	g.line("for (u64 i = h & mask;; i = (i + 1) & mask) {")
	g.line("u8 *s = m->slots + i * %dULL;", slot)
	g.line("u64 t = *(u64 *)s;")
	g.line("if (!t) return 0;")
	g.line("if (t == h && %s) return s + %dULL;", equal, 8+kw)
	g.line("}\n}")
	// Assignment: the element's storage, inserting a zero element for a new
	// key (growing first when the table would pass 3/4 full). A nil map
	// aborts, as Go panics.
	g.line("static u8 *gosvm_map%d_set(gosvm_map *m,%s *k) {", n, kc)
	g.line("if (!m) abort();")
	g.line("u64 h = gosvm_map%d_hash(k), i = 0;", n)
	g.line("if (m->cap) {")
	g.line("u64 mask = m->cap - 1;")
	g.line("for (i = h & mask;; i = (i + 1) & mask) {")
	g.line("u8 *s = m->slots + i * %dULL;", slot)
	g.line("u64 t = *(u64 *)s;")
	g.line("if (!t) break;")
	g.line("if (t == h && %s) return s + %dULL;", equal, 8+kw)
	g.line("}\n}")
	g.line("if ((u64)(m->count + 1) * 4 > m->cap * 3) {")
	g.line("gosvm_map_grow(m,%dULL);", slot)
	g.line("u64 mask = m->cap - 1;")
	g.line("for (i = h & mask; *(u64 *)(m->slots + i * %dULL); i = (i + 1) & mask) {}", slot)
	g.line("}")
	g.line("u8 *s = m->slots + i * %dULL;", slot)
	g.line("*(u64 *)s = h;")
	if kw > 0 {
		g.line("*(%s *)(s + 8) = *k;", kc)
	}
	for w := int64(0); w < vw/8; w++ {
		g.line("((u64 *)(s + %dULL))[%d] = 0;", 8+kw, w)
	}
	g.line("m->count++;")
	g.line("return s + %dULL;\n}", 8+kw)
	// delete: a nil or empty map, or an absent key, is a no-op.
	g.line("static void gosvm_map%d_delete(gosvm_map *m,%s *k) {", n, kc)
	if mt.KeyInterfaces {
		g.line("u64 h = gosvm_map%d_hash(k);", n)
		g.line("if (!m || m->count == 0) return;")
	} else {
		g.line("if (!m || m->count == 0) return;")
		g.line("u64 h = gosvm_map%d_hash(k);", n)
	}
	g.line("u64 mask = m->cap - 1;")
	g.line("for (u64 i = h & mask;; i = (i + 1) & mask) {")
	g.line("u8 *s = m->slots + i * %dULL;", slot)
	g.line("u64 t = *(u64 *)s;")
	g.line("if (!t) return;")
	g.line("if (t == h && %s) { gosvm_map_remove(m,i,%dULL); return; }", equal, slot)
	g.line("}\n}")
}

// mapKeyAs evaluates a key operand into a temporary of the map's key type.
func (g *generator) mapKeyAs(e ast.Expr, mt *MapType) string {
	return g.temp(e, mt.Type.Key(), g.exprAs(e, mt.Type.Key()))
}

// mapRead is the element of key k (a key temporary) in map m: a copy, or
// the zero value when absent. ok receives whether it was present.
func (g *generator) mapRead(n ast.Node, mt *MapType, m, k string) (value, ok string) {
	elem := mt.Type.Elem()
	ec := g.variable(n, elem)
	p := g.temp(n, types.NewPointer(types.Typ[types.Uint8]), fmt.Sprintf("gosvm_map%d_find(%s,&%s)", mt.Index, m, k))
	value = g.temp(n, elem, "{0}")
	if mt.ElemSize > 0 {
		g.line("if (%s) %s = *(%s *)%s;", p, value, ec, p)
	}
	return value, "(" + p + " != 0)"
}

// mapIndex lowers m[k] (not the comma-ok form): the map, then the key, are
// evaluated, then the element is copied (the zero value when absent).
func (g *generator) mapIndex(e *ast.IndexExpr) string {
	mt := g.mapFor(e, g.info.Types[e.X].Type)
	m := g.expr(e.X)
	k := g.mapKeyAs(e.Index, mt)
	v, _ := g.mapRead(e, mt, m, k)
	return v
}

// mapIndexOK lowers v, ok := m[k].
func (g *generator) mapIndexOK(e *ast.IndexExpr) (string, string) {
	mt := g.mapFor(e, g.info.Types[e.X].Type)
	m := g.expr(e.X)
	k := g.mapKeyAs(e.Index, mt)
	v, ok := g.mapRead(e, mt, m, k)
	return v, g.temp(e, types.Typ[types.Bool], ok)
}

// mapTarget evaluates the map and key of an assignment target m[k] and
// returns a C lvalue that inserts the key when the store runs (so stores
// happen in Go's left-to-right assignment order, after every operand). A
// zero-size element is stored into a local sink after the insertion.
func (g *generator) mapTarget(e *ast.IndexExpr) string {
	mt := g.mapFor(e, g.info.Types[e.X].Type)
	m := g.temp(e.X, g.info.Types[e.X].Type, g.expr(e.X))
	k := g.mapKeyAs(e.Index, mt)
	ec := g.variable(e, mt.Type.Elem())
	set := fmt.Sprintf("gosvm_map%d_set(%s,&%s)", mt.Index, m, k)
	if mt.ElemSize == 0 {
		sink := g.temp(e, mt.Type.Elem(), "{0}")
		return fmt.Sprintf("(*(%s, &%s))", set, sink)
	}
	return fmt.Sprintf("(*(%s *)%s)", ec, set)
}

// mapIncDec lowers m[k]++ and m[k]--: the element is inserted when absent
// and updated in place.
func (g *generator) mapIncDec(e *ast.IndexExpr, tok string) {
	mt := g.mapFor(e, g.info.Types[e.X].Type)
	m := g.expr(e.X)
	k := g.mapKeyAs(e.Index, mt)
	t := mt.Type.Elem()
	p := g.temp(e, types.NewPointer(t), fmt.Sprintf("(%s *)gosvm_map%d_set(%s,&%s)", g.variable(e, t), mt.Index, m, k))
	if isSigned(t) {
		g.incDec(e, t, "(*"+p+")", tok)
		return
	}
	g.line("(*%s)%s;", p, tok)
}

// mapLiteral lowers map[K]V{...}: make with the entry count as the hint,
// then each key and value are evaluated and inserted in source order.
func (g *generator) mapLiteral(e *ast.CompositeLit, t types.Type) string {
	mt := g.mapFor(e, t)
	r := g.temp(e, t, fmt.Sprintf("gosvm_map_make(%d,%dULL)", len(e.Elts), mt.SlotSize))
	ec := g.variable(e, mt.Type.Elem())
	for _, elt := range e.Elts {
		kv := elt.(*ast.KeyValueExpr)
		k := g.mapKeyAs(kv.Key, mt)
		v := g.exprAs(kv.Value, mt.Type.Elem())
		set := fmt.Sprintf("gosvm_map%d_set(%s,&%s)", mt.Index, r, k)
		if mt.ElemSize == 0 {
			g.line("(void)%s;", set)
			continue
		}
		g.line("*(%s *)%s = %s;", ec, set, v)
	}
	return r
}

// mapMake lowers make(map[K]V) and make(map[K]V, hint).
func (g *generator) mapMake(e *ast.CallExpr, t types.Type) string {
	mt := g.mapFor(e, t)
	hint := "0"
	if len(e.Args) > 1 {
		hint = "(i64)" + g.expr(e.Args[1])
	}
	return g.temp(e, t, fmt.Sprintf("gosvm_map_make(%s,%dULL)", hint, mt.SlotSize))
}

// mapBuiltin lowers len(m), delete(m, k) and clear(m).
func (g *generator) mapBuiltin(e *ast.CallExpr, name string) string {
	mt := g.mapFor(e, g.info.Types[e.Args[0]].Type)
	m := g.expr(e.Args[0])
	switch name {
	case "len":
		return g.temp(e, g.info.Types[e].Type, fmt.Sprintf("gosvm_map_len(%s)", m))
	case "delete":
		k := g.mapKeyAs(e.Args[1], mt)
		g.line("gosvm_map%d_delete(%s,&%s);", mt.Index, m, k)
		return ""
	case "clear":
		g.line("gosvm_map_clear(%s,%dULL);", m, mt.SlotSize)
		return ""
	}
	g.unchecked(e)
	return ""
}

// mapBuiltinCall reports whether call is len, delete or clear of a map.
func (f *frontend) mapBuiltinCall(call *ast.CallExpr) (string, bool) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	b, ok := f.info.Uses[id].(*types.Builtin)
	if !ok {
		return "", false
	}
	switch b.Name() {
	case "delete", "clear":
		return b.Name(), true
	case "len":
		return "len", isMap(f.info.Types[call.Args[0]].Type)
	}
	return "", false
}
