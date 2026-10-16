package compiler

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math/bits"
	"unicode/utf8"
)

// Expressions of the initialization interpreter (initeval_interp.go).

// conversion is the interface conversion Check recorded for e, if any.
func (ev *evaluator) conversion(e ast.Expr) *Conversion {
	if ev.p.ifaces == nil {
		return nil
	}
	return ev.p.ifaces.byExpr[e]
}

// eval evaluates e, converted to an interface where Check recorded it.
func (ev *evaluator) eval(e ast.Expr) value {
	if v, ok := ev.hoisted(e, true); ok {
		return v
	}
	if conv := ev.conversion(e); conv != nil && !conv.rng {
		if conv.From == types.Typ[types.UntypedNil] {
			return evIface{}
		}
		return ev.box(e, conv, ev.evalValue(e))
	}
	return ev.evalValue(e)
}

// evalAs is eval with an untyped nil given type t.
func (ev *evaluator) evalAs(e ast.Expr, t types.Type) value {
	if ev.info.Types[e].IsNil() {
		if conv := ev.conversion(e); conv != nil {
			return evIface{}
		}
		return ev.zero(t)
	}
	return ev.as(ev.eval(e), t)
}

// values evaluates expressions for targets of the expected types,
// expanding a multiple-value call (expressionValuesAs).
func (ev *evaluator) values(exprs []ast.Expr, expected []types.Type) []value {
	var out []value
	for i, e := range exprs {
		var want types.Type
		if i < len(expected) {
			want = expected[i]
		}
		if t, ok := ev.info.Types[e].Type.(*types.Tuple); ok && t.Len() == 2 {
			if assert, ok := ast.Unparen(e).(*ast.TypeAssertExpr); ok {
				v, ok := ev.typeAssertOK(assert)
				out = append(out, ev.convertAt(e, 0, v), ok)
				continue
			}
			if ix, ok := ev.isMapIndex(e); ok {
				m := ev.eval(ix.X).(*evMap)
				mt := ev.mapType(ix, ev.info.Types[ix.X].Type)
				k := ev.evalAs(ix.Index, mt.Type.Key())
				v, ok := ev.mapRead(m, mt, k)
				out = append(out, ev.convertAt(e, 0, v), ok)
				continue
			}
		}
		v := ev.evalAs(e, want)
		if t, ok := ev.info.Types[e].Type.(*types.Tuple); ok && t.Len() > 1 {
			tuple := v.(evTuple)
			for j := range tuple {
				out = append(out, ev.convertAt(e, j, tuple[j]))
			}
			continue
		}
		out = append(out, v)
	}
	for i := range out {
		if i < len(expected) {
			out[i] = ev.as(out[i], expected[i])
		}
	}
	return out
}

// convertAt converts element i of a multiple-value expression when Check
// recorded a conversion for it.
func (ev *evaluator) convertAt(e ast.Expr, i int, v value) value {
	if ev.p.ifaces == nil {
		return v
	}
	for _, conv := range ev.p.ifaces.byTuple[e] {
		if conv.Index == i {
			if conv.From == types.Typ[types.UntypedNil] {
				return evIface{}
			}
			return ev.box(e, conv, v)
		}
	}
	return v
}

func (ev *evaluator) evalValue(e ast.Expr) value {
	if v, ok := ev.hoisted(e, false); ok {
		return v
	}
	defer ev.hoist(e)()
	tv := ev.info.Types[e]
	if tv.IsNil() {
		if tv.Type != nil && tv.Type != types.Typ[types.UntypedNil] {
			return ev.zero(tv.Type)
		}
		return evNil{}
	}
	if tv.Value != nil {
		return ev.constValue(e, tv.Value, tv.Type)
	}
	if ev.p.funcs != nil {
		if t := ev.p.funcs.byExpr[e]; t != nil {
			return ev.funcValue(e, t)
		}
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return ev.eval(e.X)
	case *ast.Ident:
		o, ok := ev.info.Uses[e].(*types.Var)
		if !ok {
			ev.stopf(e, "internal error: identifier %s", e.Name)
		}
		return copyVal(*ev.varLoc(e, o))
	case *ast.SliceExpr:
		if isString(ev.info.Types[e.X].Type) {
			x := ev.eval(e.X).(evString)
			lo, hi := uint64(0), uint64(len(x.s))
			if e.Low != nil {
				lo = ev.index(e.Low)
			}
			if e.High != nil {
				hi = ev.index(e.High)
			}
			if lo > hi || hi > uint64(len(x.s)) {
				ev.stopf(e, "slice bounds out of range")
			}
			if x.obj == nil {
				return evString{s: x.s[lo:hi]}
			}
			return evString{s: x.s[lo:hi], obj: x.obj, off: x.off + int64(lo)}
		}
		return ev.sliceExpr(e)
	case *ast.StarExpr:
		p := ev.eval(e.X).(evPtr)
		if p.loc == nil {
			ev.stopf(e, "nil pointer dereference")
		}
		return copyVal(*p.loc)
	case *ast.CompositeLit:
		return ev.compositeLit(e, tv.Type)
	case *ast.SelectorExpr:
		if v := ev.p.frontendPackageVar(e); v != nil {
			return copyVal(*ev.varLoc(e, v))
		}
		i := ev.fieldIndex(e)
		xt := ev.info.Types[e.X].Type
		if _, isPointer := pointer(xt); isPointer {
			p := ev.eval(e.X).(evPtr)
			if p.loc == nil {
				ev.stopf(e, "nil pointer dereference")
			}
			return copyVal((*p.loc).(evStruct)[i])
		}
		if ev.info.Types[e.X].Addressable() {
			return copyVal((*ev.addr(e.X)).(evStruct)[i])
		}
		return ev.eval(e.X).(evStruct)[i]
	case *ast.IndexExpr:
		xt := ev.info.Types[e.X].Type
		if isMap(xt) {
			m := ev.eval(e.X).(*evMap)
			mt := ev.mapType(e, xt)
			k := ev.evalAs(e.Index, mt.Type.Key())
			v, _ := ev.mapRead(m, mt, k)
			return v
		}
		if _, isPointer := pointer(xt); isPointer {
			p := ev.eval(e.X).(evPtr)
			i := ev.index(e.Index)
			if p.loc == nil {
				ev.stopf(e, "nil pointer dereference")
			}
			return copyVal(ev.element(e, (*p.loc).(evArray), i))
		}
		if _, ok := xt.Underlying().(*types.Array); ok {
			if ev.info.Types[e.X].Addressable() {
				loc := ev.addr(e.X)
				i := ev.index(e.Index)
				return copyVal(ev.element(e, (*loc).(evArray), i))
			}
			x := ev.eval(e.X).(evArray)
			return ev.element(e, x, ev.index(e.Index))
		}
		x := ev.eval(e.X)
		i := ev.index(e.Index)
		if isString(xt) {
			s := x.(evString).s
			if i >= uint64(len(s)) {
				ev.stopf(e, "index out of range")
			}
			return uint64(s[i])
		}
		s := x.(evSlice)
		if i >= uint64(s.n) {
			ev.stopf(e, "index out of range")
		}
		return copyVal((*s.arr).(evArray)[s.lo+int64(i)])
	case *ast.TypeAssertExpr:
		x := ev.eval(e.X).(evIface)
		t := ev.info.Types[e.Type].Type
		if !ev.typeTest(e, t, x) {
			ev.stopf(e, "failed type assertion")
		}
		if isIface(t) {
			return x
		}
		return ev.unbox(e, t, x)
	case *ast.BinaryExpr:
		return ev.binary(e, tv.Type)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return evPtr{loc: ev.address(e.X)}
		}
		x := ev.eval(e.X)
		switch e.Op {
		case token.NOT:
			return !x.(bool)
		case token.ADD:
			return x
		case token.SUB:
			return trunc(-asInt(x), tv.Type)
		case token.XOR:
			return trunc(^asInt(x), tv.Type)
		}
		ev.stopf(e, "internal error: unary %s", e.Op)
	case *ast.CallExpr:
		return ev.callExpr(e, tv.Type)
	case *ast.FuncLit:
		ev.stopf(e, "internal error: literal without target")
	}
	ev.stopf(e, "internal error: expression %T", e)
	return nil
}

// frontendPackageVar is frontend.packageVar for a checked program: the
// package-level variable e names (V or pkg.V), or nil.
func (p *CheckedProgram) frontendPackageVar(e ast.Expr) *types.Var {
	if p.globals == nil {
		return nil
	}
	var id *ast.Ident
	switch e := e.(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		if !ok {
			return nil
		}
		if _, ok := p.Info.Uses[x].(*types.PkgName); !ok {
			return nil
		}
		id = e.Sel
	default:
		return nil
	}
	v, ok := p.Info.Uses[id].(*types.Var)
	if !ok || !isPackageVar(v) {
		return nil
	}
	return v
}

// index evaluates an index or bound as a u64 (a negative signed value is
// out of range).
func (ev *evaluator) index(e ast.Expr) uint64 {
	v := ev.eval(e)
	return widen(asInt(v), ev.info.Types[e].Type)
}

func (ev *evaluator) element(n ast.Node, a evArray, i uint64) value {
	if i >= uint64(len(a)) {
		ev.stopf(n, "index out of range")
	}
	return a[i]
}

// addr is the storage of an addressable expression (fieldTarget).
func (ev *evaluator) addr(e ast.Expr) *value {
	switch e := e.(type) {
	case *ast.Ident:
		return ev.varLoc(e, ev.info.Uses[e])
	case *ast.ParenExpr:
		return ev.addr(e.X)
	case *ast.StarExpr:
		p := ev.eval(e.X).(evPtr)
		if p.loc == nil {
			ev.stopf(e, "nil pointer dereference")
		}
		return p.loc
	case *ast.SelectorExpr:
		if v := ev.p.frontendPackageVar(e); v != nil {
			return ev.varLoc(e, v)
		}
		i := ev.fieldIndex(e)
		if _, isPointer := pointer(ev.info.Types[e.X].Type); isPointer {
			p := ev.eval(e.X).(evPtr)
			if p.loc == nil {
				ev.stopf(e, "nil pointer dereference")
			}
			return &(*p.loc).(evStruct)[i]
		}
		return &(*ev.addr(e.X)).(evStruct)[i]
	case *ast.IndexExpr:
		t := ev.info.Types[e.X].Type
		if _, ok := arrayValue(t); ok {
			base := ev.arrayPointer(e.X)
			i := ev.index(e.Index)
			a := (*base).(evArray)
			if i >= uint64(len(a)) {
				ev.stopf(e, "index out of range")
			}
			return &a[i]
		}
		s := ev.eval(e.X).(evSlice)
		i := ev.index(e.Index)
		if i >= uint64(s.n) {
			ev.stopf(e, "index out of range")
		}
		return &(*s.arr).(evArray)[s.lo+int64(i)]
	case *ast.CompositeLit:
		loc := new(value)
		*loc = ev.evalValue(e)
		return loc
	}
	ev.stopf(e, "internal error: address of %T", e)
	return nil
}

// arrayPointer is the storage of an array operand: through a pointer, the
// operand's own storage, or a temporary.
func (ev *evaluator) arrayPointer(e ast.Expr) *value {
	if _, ok := pointer(ev.info.Types[e].Type); ok {
		p := ev.eval(e).(evPtr)
		if p.loc == nil {
			ev.stopf(e, "nil pointer dereference")
		}
		return p.loc
	}
	if ev.info.Types[e].Addressable() {
		return ev.addr(e)
	}
	loc := new(value)
	*loc = ev.eval(e)
	return loc
}

// address is &e.
func (ev *evaluator) address(e ast.Expr) *value {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return ev.address(x.X)
	case *ast.CompositeLit:
		v := ev.evalValue(x)
		if ev.movedLits[x] {
			return ev.allocValue(x, ev.info.Types[x].Type, v)
		}
		loc := new(value)
		*loc = v
		return loc
	case *ast.StarExpr:
		p := ev.eval(x.X).(evPtr)
		if p.loc == nil {
			ev.stopf(x, "nil pointer dereference")
		}
		return p.loc
	}
	return ev.addr(e)
}

func (ev *evaluator) sliceExpr(e *ast.SliceExpr) value {
	var base evSlice
	if a, ok := arrayValue(ev.info.Types[e.X].Type); ok {
		loc := ev.arrayPointer(e.X)
		base = evSlice{arr: loc, n: a.Len(), c: a.Len()}
	} else {
		base = ev.eval(e.X).(evSlice)
	}
	lo, hi, max := uint64(0), uint64(base.n), uint64(base.c)
	if e.Low != nil {
		lo = ev.index(e.Low)
	}
	if e.High != nil {
		hi = ev.index(e.High)
	}
	if e.Max != nil {
		max = ev.index(e.Max)
	}
	if lo > hi || hi > max || max > uint64(base.c) {
		ev.stopf(e, "slice bounds out of range")
	}
	if base.arr == nil {
		return evSlice{}
	}
	return evSlice{arr: base.arr, lo: base.lo + int64(lo), n: int64(hi - lo), c: int64(max - lo)}
}

func (ev *evaluator) compositeLit(e *ast.CompositeLit, t types.Type) value {
	if a, ok := t.Underlying().(*types.Array); ok {
		arr := ev.zero(t).(evArray)
		index := int64(0)
		for _, elt := range e.Elts {
			value := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				index, _ = constant.Int64Val(ev.info.Types[kv.Key].Value)
				value = kv.Value
			}
			arr[index] = ev.evalAs(value, a.Elem())
			index++
		}
		return arr
	}
	if s, ok := t.(*types.Slice); ok {
		n := literalLength(ev.info, e)
		ev.checkMake(e, s.Elem(), uint64(n), uint64(n))
		loc := ev.allocArray(e, s.Elem(), n)
		index := int64(0)
		for _, elt := range e.Elts {
			value := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				index, _ = constant.Int64Val(ev.info.Types[kv.Key].Value)
				value = kv.Value
			}
			v := ev.evalAs(value, s.Elem())
			store(&(*loc).(evArray)[index], v)
			index++
		}
		return evSlice{arr: loc, n: n, c: n}
	}
	if isMap(t) {
		return ev.mapLiteral(e, t)
	}
	literal := t
	if p, ok := pointer(literal); ok {
		literal = p.Elem() // an elided &T{...} element, boxed below
	}
	st, ok := literal.Underlying().(*types.Struct)
	if !ok {
		ev.stopf(e, "internal error: literal of %s", t)
	}
	r := ev.zero(literal).(evStruct)
	for i, elt := range e.Elts {
		field, value := i, elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			value, field = kv.Value, fieldByName(st, kv.Key.(*ast.Ident).Name)
		}
		r[field] = ev.evalAs(value, st.Field(field).Type())
	}
	if literal != t {
		return evPtr{loc: ev.allocValue(e, literal, r)}
	}
	return r
}

// Interfaces.

func (ev *evaluator) dynamicType(n ast.Node, t types.Type) *DynamicType {
	if ev.p.ifaces != nil {
		if d := ev.p.ifaces.dynamicType(t); d != nil {
			return d
		}
	}
	ev.stopf(n, "internal error: dynamic type %s", t)
	return nil
}

// box converts a value of conv.From to an interface value: pointers and
// maps directly, zero-size values without allocating, every other value
// copied to a new heap allocation of its C size.
func (ev *evaluator) box(n ast.Node, conv *Conversion, v value) evIface {
	d := ev.dynamicType(n, conv.From)
	v = ev.as(v, conv.From)
	switch {
	case d.Direct:
		return evIface{dyn: d, data: v}
	case d.Size == 0:
		return evIface{dyn: d}
	}
	return evIface{dyn: d, data: evPtr{loc: ev.allocValue(n, conv.From, v)}}
}

func (ev *evaluator) unbox(n ast.Node, t types.Type, x evIface) value {
	d := ev.dynamicType(n, t)
	switch {
	case d.Direct:
		return x.data
	case d.Size == 0:
		return ev.zero(t)
	}
	return copyVal(*x.data.(evPtr).loc)
}

// typeTest reports whether x holds a value of type t (or one implementing
// interface t).
func (ev *evaluator) typeTest(n ast.Node, t types.Type, x evIface) bool {
	if isIface(t) {
		it := t.Underlying().(*types.Interface)
		if it.Empty() {
			return x.dyn != nil
		}
		return x.dyn != nil && x.dyn.Boxed && types.Implements(x.dyn.Type, it)
	}
	return x.dyn != nil && x.dyn == ev.dynamicType(n, t)
}

func (ev *evaluator) typeAssertOK(e *ast.TypeAssertExpr) (value, bool) {
	x := ev.eval(e.X).(evIface)
	t := ev.info.Types[e.Type].Type
	if !ev.typeTest(e, t, x) {
		return ev.zero(t), false
	}
	if isIface(t) {
		return x, true
	}
	return ev.unbox(e, t, x), true
}

// ifaceEqual is gosvm_iface_eq.
func (ev *evaluator) ifaceEqual(n ast.Node, a, b evIface) bool {
	if a.dyn != b.dyn {
		return false
	}
	d := a.dyn
	switch {
	case d == nil:
		return true
	case d.Direct && d.Comparable:
		return ev.equal(n, d.Type, a.data, b.data)
	case d.Size == 0:
		return true
	case d.Comparable:
		return ev.equal(n, d.Type, copyVal(*a.data.(evPtr).loc), copyVal(*b.data.(evPtr).loc))
	}
	ev.stopf(n, "comparing uncomparable type %s", d.Key)
	return false
}

// equal compares two values of comparable type t as Go does.
func (ev *evaluator) equal(n ast.Node, t types.Type, a, b value) bool {
	if isIface(t) {
		return ev.ifaceEqual(n, a.(evIface), b.(evIface))
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch a := a.(type) {
		case evString:
			return a.s == b.(evString).s
		case bool:
			return a == b.(bool)
		}
		return asInt(a) == asInt(b)
	case *types.Pointer:
		return a.(evPtr).loc == b.(evPtr).loc
	case *types.Map:
		return a.(*evMap) == b.(*evMap)
	case *types.Struct:
		x, y := a.(evStruct), b.(evStruct)
		for i := 0; i < u.NumFields(); i++ {
			if u.Field(i).Name() == "_" {
				continue
			}
			if !ev.equal(n, u.Field(i).Type(), x[i], y[i]) {
				return false
			}
		}
		return true
	case *types.Array:
		x, y := a.(evArray), b.(evArray)
		for i := range x {
			if !ev.equal(n, u.Elem(), x[i], y[i]) {
				return false
			}
		}
		return true
	}
	ev.stopf(n, "internal error: equality of %s", t)
	return false
}

// isNilValue reports whether v is the nil value of its kind.
func isNilValue(v value) bool {
	switch v := v.(type) {
	case evNil:
		return true
	case evPtr:
		return v.loc == nil
	case evSlice:
		return v.arr == nil
	case *evMap:
		return v == nil
	case evFunc:
		return v.t == nil
	case evIface:
		return v.dyn == nil
	}
	return false
}

// equalOperands is x == y for operand expressions xe and ye of types xt and
// yt (one may be nil, or an interface compared with a concrete value).
func (ev *evaluator) equalOperands(n ast.Node, xe ast.Expr, x value, xt types.Type, ye ast.Expr, y value, yt types.Type) bool {
	switch {
	case ev.info.Types[xe].IsNil():
		return isNilValue(y)
	case ev.info.Types[ye].IsNil():
		return isNilValue(x)
	}
	xi, yi := isIface(xt), isIface(yt)
	switch {
	case xi && yi:
		return ev.ifaceEqual(n, x.(evIface), y.(evIface))
	case xi:
		return ev.concreteEqual(n, x.(evIface), yt, y)
	case yi:
		return ev.concreteEqual(n, y.(evIface), xt, x)
	}
	switch xt.Underlying().(type) {
	case *types.Slice, *types.Signature:
		return isNilValue(x) == isNilValue(y) && isNilValue(x)
	}
	return ev.equal(n, xt, x, y)
}

func (ev *evaluator) concreteEqual(n ast.Node, x evIface, t types.Type, v value) bool {
	d := ev.dynamicType(n, t)
	if x.dyn != d {
		return false
	}
	switch {
	case d.Direct:
		return ev.equal(n, t, x.data, v)
	case d.Size == 0:
		return true
	}
	return ev.equal(n, t, copyVal(*x.data.(evPtr).loc), v)
}

// Operators.

func (ev *evaluator) binary(e *ast.BinaryExpr, t types.Type) value {
	xt, yt := ev.info.Types[e.X].Type, ev.info.Types[e.Y].Type
	switch e.Op {
	case token.LAND:
		if !ev.eval(e.X).(bool) {
			return false
		}
		return ev.eval(e.Y).(bool)
	case token.LOR:
		if ev.eval(e.X).(bool) {
			return true
		}
		return ev.eval(e.Y).(bool)
	case token.EQL, token.NEQ:
		x := ev.eval(e.X)
		y := ev.eval(e.Y)
		eq := ev.equalOperands(e, e.X, x, xt, e.Y, y, yt)
		if e.Op == token.NEQ {
			return !eq
		}
		return eq
	}
	if isString(xt) {
		if e.Op == token.ADD {
			var values []evString
			for _, operand := range ev.concatOperands(e, nil) {
				values = append(values, ev.eval(operand).(evString))
			}
			return ev.concat(e, values)
		}
		x, y := ev.eval(e.X).(evString).s, ev.eval(e.Y).(evString).s
		switch e.Op {
		case token.LSS:
			return x < y
		case token.LEQ:
			return x <= y
		case token.GTR:
			return x > y
		case token.GEQ:
			return x >= y
		}
		ev.stopf(e, "internal error: string %s", e.Op)
	}
	x := asInt(ev.eval(e.X))
	y := asInt(ev.eval(e.Y))
	switch e.Op {
	case token.LSS, token.LEQ, token.GTR, token.GEQ:
		var less, equal bool
		if isSigned(xt) {
			a, b := signed(x, xt), signed(y, yt)
			less, equal = a < b, a == b
		} else {
			less, equal = x < y, x == y
		}
		switch e.Op {
		case token.LSS:
			return less
		case token.LEQ:
			return less || equal
		case token.GTR:
			return !less && !equal
		}
		return !less
	}
	return ev.binaryInt(e, e.Op, t, yt, x, y)
}

// binaryInt is x op y on integers of type t (y of type yt for shifts),
// with Go's wrapping, truncating division and shift semantics; division by
// zero and negative shift counts stop.
func (ev *evaluator) binaryInt(n ast.Node, op token.Token, t, yt types.Type, x, y uint64) value {
	bits, isSigned := intInfo(t)
	switch op {
	case token.ADD:
		return trunc(x+y, t)
	case token.SUB:
		return trunc(x-y, t)
	case token.MUL:
		return trunc(x*y, t)
	case token.AND:
		return x & y
	case token.OR:
		return x | y
	case token.XOR:
		return x ^ y
	case token.AND_NOT:
		return x &^ y
	case token.QUO, token.REM:
		if y == 0 {
			ev.stopf(n, "integer divide by zero")
		}
		if isSigned {
			a, b := signed(x, t), signed(y, t)
			var r int64
			switch {
			case b == -1 && op == token.QUO:
				r = -a // wraps for the minimum value, as Go does
			case b == -1:
				r = 0
			case op == token.QUO:
				r = a / b
			default:
				r = a % b
			}
			return trunc(uint64(r), t)
		}
		if op == token.QUO {
			return x / y
		}
		return x % y
	case token.SHL, token.SHR:
		count := y
		if yt != nil && isSignedType(yt) {
			if signed(y, yt) < 0 {
				ev.stopf(n, "negative shift amount")
			}
		}
		if op == token.SHL {
			if count >= uint64(bits) {
				return uint64(0)
			}
			return trunc(x<<count, t)
		}
		if isSigned {
			if count >= 63 {
				count = 63
			}
			return trunc(uint64(signed(x, t)>>count), t)
		}
		if count >= uint64(bits) {
			return uint64(0)
		}
		return x >> count
	}
	ev.stopf(n, "internal error: operator %s", op)
	return nil
}

func isSignedType(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUnsigned == 0
}

// Conversions.

func (ev *evaluator) conversionCall(e *ast.CallExpr, t types.Type) value {
	arg := e.Args[0]
	if kind := classifyConversion(ev.info, e); kind != notStringConversion {
		return ev.stringConversion(e, kind)
	}
	if isIface(t) {
		return ev.eval(arg) // converted, or an interface value
	}
	if ev.info.Types[arg].IsNil() {
		return ev.zero(t)
	}
	x := ev.eval(arg)
	switch t.Underlying().(type) {
	case *types.Basic:
		if v, ok := x.(uint64); ok {
			from := ev.info.Types[arg].Type
			return trunc(widen(v, from), t)
		}
		return x
	}
	return x
}

func (ev *evaluator) stringConversion(e *ast.CallExpr, kind stringConversion) value {
	arg := e.Args[0]
	x := ev.eval(arg)
	switch kind {
	case stringFromString:
		return x
	case stringFromBytes:
		s := x.(evSlice)
		if s.n == 0 {
			return evString{}
		}
		return ev.allocBytes(e, s.n, string(ev.sliceBytes(s)))
	case stringFromRunes:
		s := x.(evSlice)
		var b []byte
		for i := int64(0); i < s.n; i++ {
			b = utf8.AppendRune(b, rune(int32(uint32(asInt((*s.arr).(evArray)[s.lo+i])))))
		}
		if len(b) == 0 {
			return evString{}
		}
		return ev.allocBytes(e, int64(len(b)), string(b))
	case stringFromInteger:
		v := signed(asInt(x), ev.info.Types[arg].Type)
		r := rune(0xFFFD)
		if v >= 0 && v <= 0x10FFFF {
			r = rune(v)
		}
		return ev.allocBytes(e, 8, string(utf8.AppendRune(nil, r)))
	case bytesFromString:
		s := x.(evString).s
		if len(s) == 0 {
			return evSlice{arr: ev.baseLoc}
		}
		capacity := int64(len(s))
		if ev.info.Types[arg].Value == nil {
			if int64(len(s)) > ev.budget {
				ev.stopf(e, "allocation exceeds the heap budget")
			}
			capacity = int64(roundUpSize(uint64(len(s)), false))
		}
		loc := ev.allocArray(e, types.Typ[types.Uint8], capacity)
		for i := 0; i < len(s); i++ {
			(*loc).(evArray)[i] = uint64(s[i])
		}
		return evSlice{arr: loc, n: int64(len(s)), c: capacity}
	case runesFromString:
		s := x.(evString).s
		var runes []rune
		for i := 0; i < len(s); {
			r, w := decodeRune(s[i:])
			runes = append(runes, r)
			i += w
		}
		if len(runes) == 0 {
			return evSlice{arr: ev.baseLoc}
		}
		if int64(len(runes)) > ev.budget/4 {
			ev.stopf(e, "allocation exceeds the heap budget")
		}
		mem := int64(roundUpSize(uint64(len(runes))*4, false))
		loc := ev.allocArray(e, types.Typ[types.Int32], mem/4)
		for i, r := range runes {
			(*loc).(evArray)[i] = uint64(uint32(r))
		}
		return evSlice{arr: loc, n: int64(len(runes)), c: mem / 4}
	}
	ev.stopf(e, "internal error: string conversion")
	return nil
}

// decodeRune is Go's runtime decoderune (and utf8.DecodeRuneInString).
func decodeRune(s string) (rune, int) {
	r, w := utf8.DecodeRuneInString(s)
	return r, w
}

func (ev *evaluator) sliceBytes(s evSlice) []byte {
	b := make([]byte, s.n)
	for i := range b {
		b[i] = byte(asInt((*s.arr).(evArray)[s.lo+int64(i)]))
	}
	return b
}

// Function values.

func (ev *evaluator) funcValue(e ast.Expr, t *FuncTarget) value {
	switch t.Kind {
	case TargetLiteral:
		cl := ev.p.funcs.byLit[e.(*ast.FuncLit)]
		if cl == nil {
			ev.stopf(e, "internal error: literal")
		}
		if len(cl.Captures) == 0 {
			return evFunc{t: t}
		}
		o := ev.alloc(e, cl.EnvBytes)
		env := &evEnv{target: t}
		for _, v := range cl.Captures {
			if !ev.moved[v] {
				ev.stopf(e, "internal error: captured variable not moved")
			}
			env.captures = append(env.captures, ev.varLoc(e, v))
		}
		o.kind, o.env = InitObjectFunc, env
		return evFunc{t: t, env: o}
	case TargetMethodValue:
		sel := e.(*ast.SelectorExpr)
		x := ev.info.Types[sel.X].Type
		receiver := t.Func.Type().(*types.Signature).Recv().Type()
		env := &evEnv{target: t}
		var size int64
		if t.PointerReceiver {
			if _, isPointer := pointer(x); isPointer {
				env.recv = ev.eval(sel.X)
			} else {
				env.recv = evPtr{loc: ev.addr(sel.X)}
			}
			size = 16
		} else {
			if _, isPointer := pointer(x); isPointer {
				p := ev.eval(sel.X).(evPtr)
				if p.loc == nil {
					ev.stopf(e, "nil pointer dereference")
				}
				env.recv = copyVal(*p.loc)
			} else {
				env.recv = ev.eval(sel.X)
			}
			size = 8 + roundUp8((&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(receiver))
		}
		o := ev.alloc(e, size)
		o.kind, o.env = InitObjectFunc, env
		return evFunc{t: t, env: o}
	}
	return evFunc{t: t}
}

// callFunc calls function value f.
func (ev *evaluator) callFunc(n ast.Node, f evFunc, args []value) []value {
	if f.t == nil {
		ev.stopf(n, "call of nil function value")
	}
	t := f.t
	switch t.Kind {
	case TargetLiteral:
		cl := ev.p.funcs.byLit[t.Lit]
		var captures []*value
		if f.env != nil {
			captures = f.env.env.captures
		}
		return ev.callLiteral(cl, captures, args)
	case TargetMethodValue:
		return ev.call(ev.function(n, t.Func), copyVal(f.env.env.recv), args)
	case TargetMethodExpr:
		recv := args[0]
		if t.Deref {
			p := recv.(evPtr)
			if p.loc == nil {
				ev.stopf(n, "nil pointer dereference")
			}
			recv = copyVal(*p.loc)
		}
		return ev.call(ev.function(n, t.Func), recv, args[1:])
	}
	return ev.call(ev.function(n, t.Func), nil, args)
}

// Calls.

func (ev *evaluator) function(n ast.Node, o types.Object) *Function {
	fn, _ := o.(*types.Func)
	if fn != nil {
		if f := ev.funcs[fn]; f != nil {
			return f
		}
		if fn.Pkg() != nil {
			ev.stopf(n, "calls %s.%s", fn.Pkg().Path(), fn.Name())
		}
	}
	ev.stopf(n, "internal error: callee")
	return nil
}

// calleeObject is the object a direct call names (an instance's own object
// for a generic callee).
func (ev *evaluator) calleeObject(call *ast.CallExpr) types.Object {
	id := callIdent(ev.callFun(call.Fun))
	if id == nil {
		return nil
	}
	if inst := ev.p.Instantiated[id]; inst != nil {
		return inst.Object
	}
	return ev.info.Uses[id]
}

// callFun strips explicit instantiation from a callee expression.
func (ev *evaluator) callFun(e ast.Expr) ast.Expr {
	var x ast.Expr
	switch e := e.(type) {
	case *ast.IndexExpr:
		x = e.X
	case *ast.IndexListExpr:
		x = e.X
	default:
		return e
	}
	id := callIdent(x)
	if id == nil {
		return e
	}
	switch o := ev.info.Uses[id].(type) {
	case *types.Func:
		if genericFunc(o) {
			return x
		}
	case *types.TypeName:
		if named, ok := o.Type().(*types.Named); ok && named.TypeParams().Len() > 0 && named.TypeArgs().Len() == 0 {
			return x
		}
	}
	return e
}

// callArgs evaluates a call's arguments for its parameters.
func (ev *evaluator) callArgs(call *ast.CallExpr) []value {
	var expected []types.Type
	if sig, ok := ev.info.Types[call.Fun].Type.Underlying().(*types.Signature); ok {
		for i := 0; i < sig.Params().Len(); i++ {
			expected = append(expected, sig.Params().At(i).Type())
		}
	}
	return ev.values(call.Args, expected)
}

func (ev *evaluator) result(call *ast.CallExpr, results []value) value {
	switch len(results) {
	case 0:
		return nil
	case 1:
		return results[0]
	}
	return evTuple(results)
}

func (ev *evaluator) callExpr(e *ast.CallExpr, t types.Type) value {
	ev.tick(e)
	if selector, ok := e.Fun.(*ast.SelectorExpr); ok {
		if selection := ev.info.Selections[selector]; selection != nil && selection.Kind() != types.FieldVal {
			if isIface(ev.info.Types[selector.X].Type) {
				x := ev.eval(selector.X).(evIface)
				args := ev.callArgs(e)
				return ev.result(e, ev.dynamicInvoke(e, x, args))
			}
			fn, recv, args := ev.methodArgs(e, selector)
			return ev.result(e, ev.call(fn, recv, args))
		}
	}
	if ev.info.Types[e.Fun].IsType() {
		return ev.conversionCall(e, t)
	}
	var o types.Object
	switch callee := ev.callFun(e.Fun).(type) {
	case *ast.SelectorExpr:
		id, ok := callee.X.(*ast.Ident)
		if !ok {
			return ev.indirect(e)
		}
		imported, ok := ev.info.Uses[id].(*types.PkgName)
		if !ok {
			return ev.indirect(e)
		}
		switch imported.Imported().Path() {
		case "gosvm/solana":
			ev.stopf(e, "calls gosvm/solana.%s", callee.Sel.Name)
		case bitsPath:
			return ev.bitsCall(e)
		}
		o = ev.calleeObject(e)
		if _, ok := o.(*types.Var); ok {
			return ev.indirect(e)
		}
	case *ast.Ident:
		o = ev.calleeObject(e)
		if _, ok := o.(*types.Var); ok {
			return ev.indirect(e)
		}
	default:
		return ev.indirect(e)
	}
	if b, ok := o.(*types.Builtin); ok {
		return ev.builtin(e, b.Name(), t)
	}
	if _, ok := o.(*types.TypeName); ok {
		return ev.conversionCall(e, t)
	}
	fn := ev.function(e, o)
	args := ev.callArgs(e)
	return ev.result(e, ev.call(fn, nil, args))
}

// indirect is a call through a function value (a literal called where it
// is written runs that literal).
func (ev *evaluator) indirect(e *ast.CallExpr) value {
	f := ev.eval(e.Fun).(evFunc)
	args := ev.callArgs(e)
	return ev.result(e, ev.callFunc(e, f, args))
}

// methodArgs evaluates a static method call's receiver and arguments in
// the C backend's order (methodArgs) and returns the method, the receiver
// value and the arguments.
func (ev *evaluator) methodArgs(call *ast.CallExpr, selector *ast.SelectorExpr) (*Function, value, []value) {
	o := ev.calleeObject(&ast.CallExpr{Fun: selector})
	if inst := ev.p.Instantiated[selector.Sel]; inst != nil {
		o = inst.Object
	}
	fn := ev.function(call, o)
	sig := fn.Object.Type().(*types.Signature)
	receiver := sig.Recv().Type()
	selection := ev.info.Selections[selector]
	if selection.Kind() == types.MethodExpr {
		args := ev.callArgs(call)
		actual := ev.info.Types[selector].Type.(*types.Signature).Params().At(0).Type()
		if _, isPointer := pointer(actual); isPointer {
			if _, want := pointer(receiver); !want {
				p := args[0].(evPtr)
				if p.loc == nil {
					ev.stopf(call, "nil pointer dereference")
				}
				args[0] = copyVal(*p.loc)
			}
		}
		return fn, args[0], args[1:]
	}
	actual := ev.info.Types[selector.X].Type
	if _, want := pointer(receiver); want {
		var recv value
		if _, isPointer := pointer(actual); isPointer {
			recv = ev.eval(selector.X)
		} else {
			recv = evPtr{loc: ev.addr(selector.X)}
		}
		return fn, recv, ev.callArgs(call)
	}
	if _, isPointer := pointer(actual); isPointer {
		p := ev.eval(selector.X).(evPtr)
		args := ev.callArgs(call)
		if p.loc == nil {
			ev.stopf(call, "nil pointer dereference")
		}
		return fn, copyVal(*p.loc), args
	}
	recv := ev.eval(selector.X)
	return fn, recv, ev.callArgs(call)
}

// dynamicInvoke calls the method of slot call on interface value x.
func (ev *evaluator) dynamicInvoke(call *ast.CallExpr, x evIface, args []value) []value {
	slot := ev.p.ifaces.calls[call]
	if slot == nil {
		ev.stopf(call, "internal error: dynamic call")
	}
	if x.dyn == nil {
		ev.stopf(call, "method call on nil interface")
	}
	for _, m := range x.dyn.Methods {
		if m.Slot != slot {
			continue
		}
		var recv value
		switch {
		case m.Deref:
			p := x.data.(evPtr)
			if p.loc == nil {
				ev.stopf(call, "nil pointer dereference")
			}
			recv = copyVal(*p.loc)
		case x.dyn.Direct:
			recv = x.data
		case x.dyn.Size == 0:
			recv = ev.zero(x.dyn.Type)
		default:
			recv = copyVal(*x.data.(evPtr).loc)
		}
		return ev.call(ev.function(call, m.Func), recv, args)
	}
	ev.stopf(call, "dynamic call of %s on %s is not dispatched", slot.Name, x.dyn.Key)
	return nil
}

// call runs a declared function, method or instance.
func (ev *evaluator) call(fn *Function, recv value, args []value) []value {
	ev.tick(fn.Decl)
	if fn.Library != nil {
		ev.stopf(fn.Decl, "calls native library function %s", fn.Object.Name())
	}
	sig := fn.Object.Type().(*types.Signature)
	switch fn.Intrinsic {
	case "":
	case intrinsicComparable:
		x := args[0].(evIface)
		return []value{x.dyn == nil || x.dyn.Comparable}
	case intrinsicNoescape:
		return []value{args[0]}
	case intrinsicUnsafeString:
		// A string sharing the slice's bytes (the overlay never writes
		// them afterwards, as Go's unsafe.String requires).
		s := args[0].(evSlice)
		if s.n == 0 {
			return []value{evString{}}
		}
		o := ev.arrays[s.arr]
		if o == nil {
			ev.stopf(fn.Decl, "unsafeString of storage outside the heap")
		}
		return []value{evString{s: string(ev.sliceBytes(s)), obj: o, off: s.lo}}
	default:
		ev.stopf(fn.Decl, "calls the %s intrinsic", fn.Intrinsic)
	}
	f := ev.newFrame(fn, sig.Results())
	if r := sig.Recv(); r != nil {
		ev.declare(fn.Decl, f, r, recv)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		ev.declare(fn.Decl, f, sig.Params().At(i), args[i])
	}
	return ev.runBody(fn.Decl, f, fn.Decl.Body)
}

// callLiteral runs a function literal with its captured variables'
// storage.
func (ev *evaluator) callLiteral(cl *Closure, captures []*value, args []value) []value {
	ev.tick(cl.Lit)
	sig := cl.Target.Signature
	f := ev.newFrame(nil, sig.Results())
	if cl.Results != nil {
		f.named = cl.Results
	} else {
		f.named = nil
	}
	for i, v := range cl.Captures {
		f.vars[v] = captures[i]
	}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		if i < len(cl.Params) && cl.Params[i] != nil {
			p = cl.Params[i]
		}
		ev.declare(cl.Lit, f, p, args[i])
	}
	return ev.runBody(cl.Lit, f, cl.Lit.Body)
}

func (ev *evaluator) runBody(n ast.Node, f *evFrame, body *ast.BlockStmt) []value {
	for _, v := range f.named {
		if v.Name() == "_" {
			// A blank named result still holds the returned value.
			loc := new(value)
			*loc = ev.zero(v.Type())
			f.vars[v] = loc
			continue
		}
		ev.declare(n, f, v, ev.zero(v.Type()))
	}
	saved := ev.frame
	ev.frame = f
	ev.execList(body.List)
	for i := len(f.defers) - 1; i >= 0; i-- {
		f.defers[i]()
		ev.frame = f
	}
	ev.frame = saved
	if f.named != nil {
		out := make([]value, len(f.named))
		for i, v := range f.named {
			out[i] = copyVal(*f.vars[v])
		}
		return out
	}
	if f.results != nil && f.results.Len() > 0 && f.ret == nil {
		ev.stopf(n, "internal error: missing results")
	}
	return f.ret
}

// Builtins.

func (ev *evaluator) checkMake(n ast.Node, elem types.Type, length, capacity uint64) {
	if length > capacity || capacity > (1<<48)/uint64(cSizeof(elem)) {
		ev.stopf(n, "makeslice: len or cap out of range")
	}
}

func (ev *evaluator) builtin(e *ast.CallExpr, name string, t types.Type) value {
	switch name {
	case "panic":
		ev.stopf(e, "panic")
	case "new":
		elem := t.(*types.Pointer).Elem()
		return evPtr{loc: ev.allocValue(e, elem, ev.zero(elem))}
	case "make":
		if isMap(t) {
			mt := ev.mapType(e, t)
			hint := int64(0)
			if len(e.Args) > 1 {
				h := ev.eval(e.Args[1])
				hint = signed(asInt(h), ev.info.Types[e.Args[1]].Type)
			}
			return ev.mapMake(e, mt, hint)
		}
		elem := t.(*types.Slice).Elem()
		length := ev.makeLength(e.Args[1])
		capacity := length
		if len(e.Args) > 2 {
			capacity = ev.makeLength(e.Args[2])
		}
		ev.checkMake(e, elem, length, capacity)
		loc := ev.allocArray(e, elem, int64(capacity))
		return evSlice{arr: loc, n: int64(length), c: int64(capacity)}
	case "append":
		s := t.(*types.Slice)
		base := ev.evalAs(e.Args[0], s).(evSlice)
		var values []value
		if e.Ellipsis.IsValid() {
			if isString(ev.info.Types[e.Args[1]].Type) {
				for _, b := range []byte(ev.eval(e.Args[1]).(evString).s) {
					values = append(values, uint64(b))
				}
			} else {
				extra := ev.evalAs(e.Args[1], s).(evSlice)
				for i := int64(0); i < extra.n; i++ {
					values = append(values, copyVal((*extra.arr).(evArray)[extra.lo+i]))
				}
			}
		} else {
			for _, v := range e.Args[1:] {
				values = append(values, ev.evalAs(v, s.Elem()))
			}
			if len(values) == 0 {
				return base
			}
		}
		r := ev.grow(e, base, int64(len(values)), s.Elem())
		for j, v := range values {
			store(&(*r.arr).(evArray)[r.lo+r.n-int64(len(values))+int64(j)], v)
		}
		return r
	case "copy":
		dt := ev.info.Types[e.Args[0]].Type
		dst := ev.evalAs(e.Args[0], dt)
		src := ev.evalAs(e.Args[1], dt)
		return ev.copySlice(dst, src)
	case "len", "cap":
		x := ev.eval(e.Args[0])
		switch x := x.(type) {
		case evString:
			return uint64(len(x.s))
		case evSlice:
			if name == "len" {
				return uint64(x.n)
			}
			return uint64(x.c)
		case *evMap:
			if x == nil {
				return uint64(0)
			}
			return uint64(len(x.entries))
		case evArray:
			return uint64(len(x))
		case evPtr:
			if a, ok := arrayValue(ev.info.Types[e.Args[0]].Type); ok {
				return uint64(a.Len())
			}
		}
	case "delete":
		m := ev.eval(e.Args[0]).(*evMap)
		mt := ev.mapType(e, ev.info.Types[e.Args[0]].Type)
		k := ev.evalAs(e.Args[1], mt.Type.Key())
		ev.mapDelete(m, k)
		return nil
	case "clear":
		m, ok := ev.eval(e.Args[0]).(*evMap)
		if !ok {
			ev.stopf(e, "internal error: clear")
		}
		if m != nil {
			m.entries, m.index = nil, map[string]*evEntry{}
		}
		return nil
	}
	ev.stopf(e, "builtin %s is not evaluated at compile time", name)
	return nil
}

// makeLength is a make operand as a u64; a negative signed value stops.
func (ev *evaluator) makeLength(e ast.Expr) uint64 {
	v := asInt(ev.eval(e))
	t := ev.info.Types[e].Type
	if isSignedType(t) && signed(v, t) < 0 {
		ev.stopf(e, "makeslice: len out of range")
	}
	return widen(v, t)
}

// copySlice is copy(dst, src): src a slice of the same type or a string.
func (ev *evaluator) copySlice(dst, src value) value {
	d := dst.(evSlice)
	var elems []value
	switch s := src.(type) {
	case evString:
		for i := 0; i < len(s.s); i++ {
			elems = append(elems, uint64(s.s[i]))
		}
	case evSlice:
		for i := int64(0); i < s.n; i++ {
			elems = append(elems, copyVal((*s.arr).(evArray)[s.lo+i]))
		}
	}
	n := d.n
	if int64(len(elems)) < n {
		n = int64(len(elems))
	}
	for i := int64(0); i < n; i++ {
		store(&(*d.arr).(evArray)[d.lo+i], elems[i])
	}
	return uint64(n)
}

// grow is gosvm_grow (Go 1.22 growslice).
func (ev *evaluator) grow(n ast.Node, s evSlice, num int64, elem types.Type) evSlice {
	newlen := s.n + num
	if newlen <= s.c {
		return evSlice{arr: s.arr, lo: s.lo, n: newlen, c: s.c}
	}
	size := uint64(cSizeof(elem))
	newcap := nextCap(uint64(newlen), uint64(s.c))
	if newcap > uint64(ev.budget)/size {
		ev.stopf(n, "allocation exceeds the heap budget")
	}
	newcap = roundUpSize(newcap*size, containsReferences(elem)) / size
	loc := ev.allocArray(n, elem, int64(newcap))
	for i := int64(0); i < s.n; i++ {
		store(&(*loc).(evArray)[i], (*s.arr).(evArray)[s.lo+i])
	}
	return evSlice{arr: loc, n: newlen, c: int64(newcap)}
}

// nextCap is Go 1.22 runtime.nextslicecap.
func nextCap(newlen, oldcap uint64) uint64 {
	newcap, doublecap := oldcap, oldcap+oldcap
	if newlen > doublecap {
		return newlen
	}
	if oldcap < 256 {
		return doublecap
	}
	for newcap < newlen {
		newcap += (newcap + 768) >> 2
	}
	return newcap
}

var mallocSizeClasses = [68]uint64{0, 8, 16, 24, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224, 240, 256, 288, 320, 352, 384, 416, 448, 480, 512, 576, 640, 704, 768, 896, 1024, 1152, 1280, 1408, 1536, 1792, 2048, 2304, 2688, 3072, 3200, 3456, 4096, 4864, 5376, 6144, 6528, 6784, 6912, 8192, 9472, 9728, 10240, 10880, 12288, 13568, 14336, 16384, 18432, 19072, 20480, 21760, 24576, 27264, 28672, 32768}

// roundUpSize is gosvm_roundupsize (Go 1.22 roundupsize with malloc
// headers).
func roundUpSize(size uint64, scan bool) uint64 {
	req := size
	if req <= 32768-8 {
		if scan && req > 512 {
			req += 8
		}
		for _, c := range mallocSizeClasses {
			if c >= req {
				return c - (req - size)
			}
		}
	}
	req += 8191
	if req < size {
		return size
	}
	return req &^ 8191
}

// math/bits.

func (ev *evaluator) bitsCall(call *ast.CallExpr) value {
	fn, ok := isBitsFunc(ev.info, call)
	if !ok {
		ev.stopf(call, "internal error: math/bits")
	}
	name := fn.Name()
	switch name {
	case "Add", "Sub", "Mul", "Div", "Rem", "LeadingZeros", "TrailingZeros", "OnesCount", "Len", "Reverse", "ReverseBytes", "RotateLeft":
		name += "64"
	}
	args := ev.callArgs(call)
	a := func(i int) uint64 { return asInt(args[i]) }
	pair := func(x, y uint64) value { return evTuple{x, y} }
	switch name {
	case "Add64":
		c := uint64(0)
		if a(2) != 0 {
			c = 1
		}
		s, carry := bits.Add64(a(0), a(1), c)
		return pair(s, carry)
	case "Sub64":
		b := uint64(0)
		if a(2) != 0 {
			b = 1
		}
		d, borrow := bits.Sub64(a(0), a(1), b)
		return pair(d, borrow)
	case "Add32":
		sum := a(0) + a(1) + a(2)
		return pair(sum&0xffffffff, sum>>32)
	case "Sub32":
		d, borrow := bits.Sub32(uint32(a(0)), uint32(a(1)), uint32(a(2)))
		return pair(uint64(d), uint64(borrow))
	case "Mul64":
		hi, lo := bits.Mul64(a(0), a(1))
		return pair(hi, lo)
	case "Mul32":
		hi, lo := bits.Mul32(uint32(a(0)), uint32(a(1)))
		return pair(uint64(hi), uint64(lo))
	case "Div64":
		if a(2) == 0 || a(2) <= a(0) {
			ev.stopf(call, "math/bits.Div64 traps")
		}
		q, r := bits.Div64(a(0), a(1), a(2))
		return pair(q, r)
	case "Rem64":
		if a(2) == 0 {
			ev.stopf(call, "math/bits.Rem64 traps")
		}
		return bits.Rem64(a(0), a(1), a(2))
	case "Div32":
		if a(2) == 0 || a(2) <= a(0) {
			ev.stopf(call, "math/bits.Div32 traps")
		}
		q, r := bits.Div32(uint32(a(0)), uint32(a(1)), uint32(a(2)))
		return pair(uint64(q), uint64(r))
	case "Rem32":
		if a(2) == 0 {
			ev.stopf(call, "math/bits.Rem32 traps")
		}
		return uint64(bits.Rem32(uint32(a(0)), uint32(a(1)), uint32(a(2))))
	case "LeadingZeros64":
		return uint64(bits.LeadingZeros64(a(0)))
	case "LeadingZeros32":
		return uint64(bits.LeadingZeros32(uint32(a(0))))
	case "LeadingZeros16":
		return uint64(bits.LeadingZeros16(uint16(a(0))))
	case "LeadingZeros8":
		return uint64(bits.LeadingZeros8(uint8(a(0))))
	case "TrailingZeros64":
		return uint64(bits.TrailingZeros64(a(0)))
	case "TrailingZeros32":
		return uint64(bits.TrailingZeros32(uint32(a(0))))
	case "TrailingZeros16":
		return uint64(bits.TrailingZeros16(uint16(a(0))))
	case "TrailingZeros8":
		return uint64(bits.TrailingZeros8(uint8(a(0))))
	case "OnesCount64":
		return uint64(bits.OnesCount64(a(0)))
	case "OnesCount32":
		return uint64(bits.OnesCount32(uint32(a(0))))
	case "OnesCount16":
		return uint64(bits.OnesCount16(uint16(a(0))))
	case "OnesCount8":
		return uint64(bits.OnesCount8(uint8(a(0))))
	case "Len64":
		return uint64(bits.Len64(a(0)))
	case "Len32":
		return uint64(bits.Len32(uint32(a(0))))
	case "Len16":
		return uint64(bits.Len16(uint16(a(0))))
	case "Len8":
		return uint64(bits.Len8(uint8(a(0))))
	case "Reverse64":
		return bits.Reverse64(a(0))
	case "Reverse32":
		return uint64(bits.Reverse32(uint32(a(0))))
	case "Reverse16":
		return uint64(bits.Reverse16(uint16(a(0))))
	case "Reverse8":
		return uint64(bits.Reverse8(uint8(a(0))))
	case "ReverseBytes64":
		return bits.ReverseBytes64(a(0))
	case "ReverseBytes32":
		return uint64(bits.ReverseBytes32(uint32(a(0))))
	case "ReverseBytes16":
		return uint64(bits.ReverseBytes16(uint16(a(0))))
	case "RotateLeft64", "RotateLeft32", "RotateLeft16", "RotateLeft8":
		k := int(signed(a(1), ev.info.Types[call.Args[1]].Type))
		switch name {
		case "RotateLeft64":
			return bits.RotateLeft64(a(0), k)
		case "RotateLeft32":
			return uint64(bits.RotateLeft32(uint32(a(0)), k))
		case "RotateLeft16":
			return uint64(bits.RotateLeft16(uint16(a(0)), k))
		}
		return uint64(bits.RotateLeft8(uint8(a(0)), k))
	}
	ev.stopf(call, "math/bits.%s is not evaluated at compile time", fn.Name())
	return nil
}
