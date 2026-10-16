package compiler

import (
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/types"
)

// Maps and the export of the interpreter's state (initeval.go).

// plainKey reports whether the image can hold maps keyed by t: integers,
// booleans, strings, and arrays and structs of them. Their hash is fixed by
// the key's value alone (pointers and interfaces hash by address or type
// ID), so the backend can lay out the table at compile time.
func plainKey(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsInteger|types.IsBoolean|types.IsString) != 0
	case *types.Array:
		return plainKey(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if !plainKey(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

func (ev *evaluator) mapType(n ast.Node, t types.Type) *MapType {
	if ev.p.maps != nil {
		if mt := ev.p.maps.byKey[typeKey(mapOf(t))]; mt != nil {
			return mt
		}
	}
	ev.stopf(n, "internal error: map type %s", t)
	return nil
}

// keyString is the canonical encoding of a map key (blank fields ignored,
// as Go's map key equality does).
func keyString(v value, t types.Type, b []byte) []byte {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch x := v.(type) {
		case evString:
			b = binary.LittleEndian.AppendUint64(b, uint64(len(x.s)))
			return append(b, x.s...)
		case bool:
			if x {
				return append(b, 1)
			}
			return append(b, 0)
		}
		return binary.LittleEndian.AppendUint64(b, asInt(v))
	case *types.Array:
		for _, e := range v.(evArray) {
			b = keyString(e, u.Elem(), b)
		}
		return b
	case *types.Struct:
		s := v.(evStruct)
		for i := 0; i < u.NumFields(); i++ {
			if u.Field(i).Name() != "_" {
				b = keyString(s[i], u.Field(i).Type(), b)
			}
		}
		return b
	}
	panic("unsupported map key")
}

// mapCap is gosvm_map_cap: the smallest power of two of at least 8 that
// holds n keys at load factor 3/4.
func mapCap(n uint64) uint64 {
	c := uint64(8)
	for n*4 > c*3 {
		c <<= 1
	}
	return c
}

// mapMake is gosvm_map_make: the header, and slots for hint keys when the
// hint is positive and not oversized.
func (ev *evaluator) mapMake(n ast.Node, mt *MapType, hint int64) *evMap {
	if !plainKey(mt.Type.Key()) {
		ev.stopf(n, "maps keyed by %s are not evaluated at compile time", mt.Type.Key())
	}
	m := &evMap{mt: mt, index: map[string]*evEntry{}}
	m.hdr = ev.alloc(n, mapHeaderBytes)
	m.hdr.kind, m.hdr.m = InitObjectMap, m
	if hint > 0 && uint64(hint) <= (1<<48)/uint64(mt.SlotSize) {
		m.cap = int64(mapCap(uint64(hint)))
		m.slots = ev.alloc(n, m.cap*mt.SlotSize)
		m.slots.kind, m.slots.m = InitObjectMapSlots, m
	}
	return m
}

// mapRead is m[k] for a map of type mt: a copy of the element, or the zero
// value when m is nil or k absent.
func (ev *evaluator) mapRead(m *evMap, mt *MapType, k value) (value, bool) {
	if m != nil {
		if e := m.index[string(keyString(k, mt.Type.Key(), nil))]; e != nil {
			return copyVal(e.elem), true
		}
	}
	return ev.zero(mt.Type.Elem()), false
}

// mapSet is gosvm_mapN_set: the entry of key k, inserted with the zero
// element when absent (growing the table first when it would pass 3/4
// full); a nil map stops (Go panics).
func (ev *evaluator) mapSet(n ast.Node, m *evMap, k value) *evEntry {
	if m == nil {
		ev.stopf(n, "assignment to entry in nil map")
	}
	key := string(keyString(k, m.mt.Type.Key(), nil))
	if e := m.index[key]; e != nil {
		return e
	}
	slot := m.mt.SlotSize
	if (int64(len(m.entries))+1)*4 > m.cap*3 {
		c := int64(8)
		if m.cap != 0 {
			c = m.cap * 2
		}
		if c > ev.budget/slot {
			ev.stopf(n, "map growth exceeds the heap budget")
		}
		m.slots = ev.alloc(n, c*slot)
		m.slots.kind, m.slots.m = InitObjectMapSlots, m
		m.cap = c
	}
	e := &evEntry{key: copyVal(k), elem: ev.zero(m.mt.Type.Elem())}
	m.entries = append(m.entries, e)
	m.index[key] = e
	return e
}

func (ev *evaluator) mapDelete(m *evMap, k value) {
	if m == nil {
		return
	}
	key := string(keyString(k, m.mt.Type.Key(), nil))
	e := m.index[key]
	if e == nil {
		return
	}
	delete(m.index, key)
	for i, x := range m.entries {
		if x == e {
			m.entries = append(m.entries[:i:i], m.entries[i+1:]...)
			break
		}
	}
}

// mapLiteral is make with the entry count as the hint, then the entries in
// gc's insertion order (MapLiteralOrder), each evaluated as an owner.
func (ev *evaluator) mapLiteral(e *ast.CompositeLit, t types.Type) *evMap {
	mt := ev.mapType(e, t)
	m := ev.mapMake(e, mt, int64(len(e.Elts)))
	var entries []int
	if ev.p.order != nil {
		entries = ev.p.order.mapOrder[e]
	}
	if entries == nil {
		for i := range e.Elts {
			entries = append(entries, i)
		}
	}
	for _, i := range entries {
		kv := e.Elts[i].(*ast.KeyValueExpr)
		release := ev.hoist(kv)
		k := ev.evalAs(kv.Key, mt.Type.Key())
		v := ev.evalAs(kv.Value, mt.Type.Elem())
		ev.mapSet(e, m, k).elem = copyVal(v)
		release()
	}
	return m
}

// Export.

// evLoc is where a storage location lives: in a heap object, or in a
// package-level variable (the globals block or read-only data), at path.
type evLoc struct {
	obj    *evObj
	global *Global
	base   bool
	path   []int
}

type exporter struct {
	ev      *evaluator
	index   map[*value]evLoc
	objects map[*evObj]*InitObject
	maps    map[*evMap]*InitMap
	list    []*InitObject
}

type exportError struct{ reason string }

func (ev *evaluator) export() (image *InitImage, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(exportError); ok {
				image, err = nil, fmt.Errorf("%s", e.reason)
				return
			}
			panic(r)
		}
	}()
	x := &exporter{ev: ev, index: map[*value]evLoc{}, objects: map[*evObj]*InitObject{}, maps: map[*evMap]*InitMap{}}
	x.index[ev.baseLoc] = evLoc{base: true}
	for _, g := range ev.p.Globals {
		if loc := ev.globals[g.Var]; loc != nil {
			x.walk(loc, evLoc{global: g})
		}
	}
	for v, loc := range ev.ro {
		x.walk(loc, evLoc{global: ev.byVar[v]})
	}
	for _, o := range ev.objs {
		if o.root != nil {
			x.walk(o.root, evLoc{obj: o})
		}
	}
	image = &InitImage{HeapUsed: ev.used, Values: make([]*InitValue, len(ev.p.Globals)), Work: ev.work}
	for i, g := range ev.p.Globals {
		if !g.ReadOnly {
			image.Values[i] = x.value(*ev.globals[g.Var], g.Var.Type())
		}
	}
	sortedObjects(x.list)
	image.Objects = x.list
	return image, nil
}

func (x *exporter) fail(format string, args ...any) {
	panic(exportError{fmt.Sprintf(format, args...)})
}

// walk records loc and every field and element inside it.
func (x *exporter) walk(loc *value, at evLoc) {
	x.index[loc] = at
	var elems []value
	switch v := (*loc).(type) {
	case evStruct:
		elems = v
	case evArray:
		elems = v
	default:
		return
	}
	for i := range elems {
		next := at
		next.path = append(append([]int(nil), at.path...), i)
		x.walk(&elems[i], next)
	}
}

func (x *exporter) ref(loc *value) *InitRef {
	at, ok := x.index[loc]
	if !ok {
		x.fail("a reference to storage that does not outlive initialization")
	}
	switch {
	case at.base:
		return &InitRef{HeapBase: true}
	case at.obj != nil:
		if at.obj.block {
			x.fail("internal error: block object")
		}
		return &InitRef{Object: x.object(at.obj), Path: at.path}
	}
	return &InitRef{Global: at.global, Path: at.path}
}

func (x *exporter) object(o *evObj) *InitObject {
	if out := x.objects[o]; out != nil {
		return out
	}
	out := &InitObject{Offset: o.off, Size: o.size, Kind: o.kind}
	x.objects[o] = out
	x.list = append(x.list, out)
	switch o.kind {
	case InitObjectValue:
		out.Type = o.typ
		out.Value = x.value(*o.root, o.typ)
	case InitObjectArray:
		out.Type = o.typ
		out.Value = x.value(*o.root, types.NewArray(o.typ, int64(len((*o.root).(evArray)))))
	case InitObjectBytes:
		out.Bytes = o.bytes
	case InitObjectMap, InitObjectMapSlots:
		out.Map = x.mapOf(o.m)
	case InitObjectFunc:
		env := o.env
		f := &InitFunc{Target: env.target}
		for _, c := range env.captures {
			f.Captures = append(f.Captures, x.ref(c))
		}
		if env.target.Kind == TargetMethodValue {
			f.Receiver = x.value(env.recv, env.target.Func.Type().(*types.Signature).Recv().Type())
		}
		out.Func = f
	default:
		x.fail("internal error: object kind %d", o.kind)
	}
	return out
}

func (x *exporter) mapOf(m *evMap) *InitMap {
	if out := x.maps[m]; out != nil {
		return out
	}
	out := &InitMap{Type: m.mt, Cap: m.cap}
	x.maps[m] = out
	x.object(m.hdr)
	if m.slots != nil {
		out.Slots = x.object(m.slots)
	}
	for _, e := range m.entries {
		out.Entries = append(out.Entries, InitMapEntry{Key: x.value(e.key, m.mt.Type.Key()), Elem: x.value(e.elem, m.mt.Type.Elem())})
	}
	return out
}

func (x *exporter) value(v value, t types.Type) *InitValue {
	out := &InitValue{Type: t}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch v := v.(type) {
		case bool:
			out.Kind = InitBool
			if v {
				out.Bits = 1
			}
		case evString:
			out.Kind, out.Bytes = InitString, []byte(v.s)
			if v.obj != nil {
				out.Ref, out.Low = &InitRef{Object: x.object(v.obj)}, v.off
			}
		case uint64:
			out.Kind, out.Bits = InitInt, v
		default:
			x.fail("internal error: basic value %T", v)
		}
	case *types.Pointer:
		out.Kind = InitPointer
		if p := v.(evPtr); p.loc != nil {
			out.Ref = x.ref(p.loc)
		}
	case *types.Slice:
		out.Kind = InitSlice
		s := v.(evSlice)
		if s.arr != nil {
			out.Ref, out.Low, out.Len, out.Cap = x.ref(s.arr), s.lo, s.n, s.c
		}
	case *types.Map:
		out.Kind = InitMapValue
		if m := v.(*evMap); m != nil {
			x.mapOf(m)
			out.Ref = &InitRef{Object: x.object(m.hdr)}
		}
	case *types.Signature:
		out.Kind = InitFuncValue
		f := v.(evFunc)
		out.Target = f.t
		if f.env != nil {
			out.Ref = &InitRef{Object: x.object(f.env)}
		}
	case *types.Interface:
		out.Kind = InitInterface
		i := v.(evIface)
		out.Dynamic = i.dyn
		if d := i.dyn; d != nil {
			switch {
			case d.Direct:
				out.Elem = x.value(i.data, d.Type)
			case d.Size == 0:
			default:
				ref := x.ref(i.data.(evPtr).loc)
				if ref.Object == nil || len(ref.Path) != 0 {
					x.fail("internal error: interface box")
				}
				out.Ref = ref
			}
		}
	case *types.Struct:
		out.Kind = InitStruct
		s := v.(evStruct)
		for i := 0; i < u.NumFields(); i++ {
			out.Elems = append(out.Elems, x.value(s[i], u.Field(i).Type()))
		}
	case *types.Array:
		out.Kind = InitArray
		for _, e := range v.(evArray) {
			out.Elems = append(out.Elems, x.value(e, u.Elem()))
		}
	default:
		x.fail("internal error: value of type %s", t)
	}
	return out
}
