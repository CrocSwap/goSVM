package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// The initialization interpreter (initeval.go). It evaluates checked Go the
// way the C backend's code does: the same evaluation order (left to right,
// with CheckedProgram.Hoisted applied at the same owners), the same heap
// allocations of the same contracted sizes (docs/ERROR_CONTRACT.md#contracted-runtime-behaviour)
// and the same values. Anything that would trap, log, call the SDK or a
// native library, or that it does not model, stops the current step
// (evStop); the step then runs at run time instead.

// value is an interpreter value:
//
//	integers      uint64 (two's-complement bits, zero-extended from the width)
//	bool          bool
//	string        evString
//	pointer       evPtr
//	slice         evSlice
//	struct        evStruct (fields)
//	array         evArray (elements)
//	interface     evIface
//	map           *evMap (nil: a nil map)
//	function      evFunc
//	tuple         evTuple (multiple results)
//
// Storage is a *value. Aggregates are stored in place (store), so pointers
// to fields and elements stay valid when their variable is assigned.
type value = any

type (
	evStruct []value
	evArray  []value
	evTuple  []value
	// evString is a string; obj is the heap bytes object holding it (nil
	// for literal and other read-only data) and off its offset there.
	evString struct {
		s   string
		obj *evObj
		off int64
	}
	// evPtr points at storage; loc is nil for a nil pointer.
	evPtr struct{ loc *value }
	// evSlice is a slice of the evArray stored at arr (nil: a nil slice),
	// from element lo, with length n and capacity c.
	evSlice struct {
		arr      *value
		lo, n, c int64
	}
	// evIface is an interface value: dyn nil for a nil interface; data is
	// the pointer or map value of a Direct type, an evPtr to the box of any
	// other non-zero-size value, or nil.
	evIface struct {
		dyn  *DynamicType
		data value
	}
	// evFunc is a function value: t nil for nil; env the closure
	// environment or method value object, if any.
	evFunc struct {
		t   *FuncTarget
		env *evObj
	}
	// evNil is an untyped nil whose type the context supplies.
	evNil struct{}
)

// evMap is a map; hdr is its 24-byte header object.
type evMap struct {
	mt      *MapType
	hdr     *evObj
	slots   *evObj
	cap     int64
	entries []*evEntry
	index   map[string]*evEntry
}

type evEntry struct{ key, elem value }

// evEnv is a closure environment (captured variables' storage) or a method
// value (the bound receiver).
type evEnv struct {
	target   *FuncTarget
	captures []*value
	recv     value
}

// evObj is one heap allocation (or the globals block).
type evObj struct {
	off, size int64
	kind      InitObjectKind
	typ       types.Type
	root      *value
	bytes     []byte
	m         *evMap
	env       *evEnv
	block     bool
}

// evStop stops the current step.
type evStop struct {
	pos    token.Pos
	reason string
}

// evFrame is one function activation.
type evFrame struct {
	vars    map[types.Object]*value
	results *types.Tuple
	named   []*types.Var
	ret     []value
	defers  []func()
}

type evaluator struct {
	p         *CheckedProgram
	info      *types.Info
	budget    int64
	used      int64
	work      int
	funcs     map[*types.Func]*Function
	moved     map[types.Object]bool
	movedLits map[*ast.CompositeLit]bool
	globals   map[*types.Var]*value // writable variables (the block)
	ro        map[*types.Var]*value // read-only variables, as read
	byVar     map[*types.Var]*Global
	block     *evObj
	objs      []*evObj
	baseLoc   *value            // the heap start (non-nil empty slices)
	arrays    map[*value]*evObj // heap array objects by their storage
	frame     *evFrame
	hv, hb    map[ast.Expr]value // hoisted values (unconverted, converted)
}

func newEvaluator(p *CheckedProgram) *evaluator {
	ev := &evaluator{p: p, info: p.Info, budget: int64(p.Program.HeapBytes), funcs: map[*types.Func]*Function{},
		moved: map[types.Object]bool{}, movedLits: map[*ast.CompositeLit]bool{}, globals: map[*types.Var]*value{},
		ro: map[*types.Var]*value{}, byVar: map[*types.Var]*Global{}, hv: map[ast.Expr]value{}, hb: map[ast.Expr]value{}, arrays: map[*value]*evObj{}}
	if ev.budget == 0 {
		ev.budget = DefaultHeapBytes
	}
	empty := value(evArray{})
	ev.baseLoc = &empty
	for _, fn := range p.Functions {
		ev.funcs[fn.Object] = fn
		for _, v := range fn.HeapVars {
			ev.moved[v] = true
		}
		for _, lit := range fn.HeapLiterals {
			ev.movedLits[lit] = true
		}
	}
	for _, g := range p.Globals {
		ev.byVar[g.Var] = g
	}
	return ev
}

// allocBlock allocates the globals block, the first allocation, and zeroes
// the writable variables.
func (ev *evaluator) allocBlock() {
	if ev.p.GlobalBytes > 0 {
		ev.block = ev.alloc(nil, ev.p.GlobalBytes)
		ev.block.block = true
	}
	for _, g := range ev.p.Globals {
		if !g.ReadOnly {
			loc := new(value)
			*loc = ev.zero(g.Var.Type())
			ev.globals[g.Var] = loc
		}
	}
}

func (ev *evaluator) stopf(n ast.Node, format string, args ...any) {
	pos := token.NoPos
	if n != nil {
		pos = n.Pos()
	}
	panic(&evStop{pos: pos, reason: fmt.Sprintf(format, args...)})
}

// tick counts one unit of work.
func (ev *evaluator) tick(n ast.Node) {
	ev.work++
	if ev.work > InitEvalStepLimit {
		ev.stopf(n, "exceeds the %d-step compile-time evaluation budget", InitEvalStepLimit)
	}
}

// alloc is gosvm_alloc: bump allocation of n bytes rounded up to 8,
// aborting (here: stopping) when the budget is exhausted.
func (ev *evaluator) alloc(n ast.Node, bytes int64) *evObj {
	offset := ev.used
	if offset == 0 {
		offset = 8
	}
	if bytes < 0 || uint64(bytes) > uint64(ev.budget-offset) {
		ev.stopf(n, "allocation exceeds the heap budget")
	}
	ev.used = offset + roundUp8(bytes)
	o := &evObj{off: offset, size: roundUp8(bytes)}
	ev.objs = append(ev.objs, o)
	return o
}

// allocValue allocates a value object of type t holding v.
func (ev *evaluator) allocValue(n ast.Node, t types.Type, v value) *value {
	o := ev.alloc(n, cSizeof(t))
	o.kind, o.typ = InitObjectValue, t
	loc := new(value)
	*loc = copyVal(v)
	o.root = loc
	return loc
}

// allocArray allocates the backing array of n elements of type elem.
func (ev *evaluator) allocArray(node ast.Node, elem types.Type, n int64) *value {
	o := ev.alloc(node, n*cSizeof(elem))
	o.kind, o.typ = InitObjectArray, elem
	arr := make(evArray, n)
	for i := range arr {
		arr[i] = ev.zero(elem)
	}
	loc := value(arr)
	o.root = &loc
	ev.arrays[o.root] = o
	return o.root
}

// allocBytes allocates a string bytes object of size bytes holding s.
func (ev *evaluator) allocBytes(n ast.Node, size int64, s string) evString {
	o := ev.alloc(n, size)
	o.kind, o.bytes = InitObjectBytes, []byte(s)
	return evString{s: s, obj: o}
}

// Values.

func (ev *evaluator) zero(t types.Type) value {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return false
		case u.Info()&types.IsString != 0:
			return evString{}
		case u.Kind() == types.UntypedNil:
			return evNil{}
		}
		return uint64(0)
	case *types.Pointer:
		return evPtr{}
	case *types.Slice:
		return evSlice{}
	case *types.Map:
		return (*evMap)(nil)
	case *types.Signature:
		return evFunc{}
	case *types.Interface:
		return evIface{}
	case *types.Struct:
		s := make(evStruct, u.NumFields())
		for i := range s {
			s[i] = ev.zero(u.Field(i).Type())
		}
		return s
	case *types.Array:
		a := make(evArray, u.Len())
		for i := range a {
			a[i] = ev.zero(u.Elem())
		}
		return a
	}
	ev.stopf(nil, "values of type %s are not evaluated at compile time", t)
	return nil
}

// as gives an untyped nil its context's zero value.
func (ev *evaluator) as(v value, t types.Type) value {
	if _, ok := v.(evNil); ok && t != nil {
		return ev.zero(t)
	}
	return v
}

func copyVal(v value) value {
	switch v := v.(type) {
	case evStruct:
		c := make(evStruct, len(v))
		for i := range v {
			c[i] = copyVal(v[i])
		}
		return c
	case evArray:
		c := make(evArray, len(v))
		for i := range v {
			c[i] = copyVal(v[i])
		}
		return c
	}
	return v
}

// store writes v into storage loc, aggregates element by element so that
// pointers into loc stay valid.
func store(loc *value, v value) {
	switch d := (*loc).(type) {
	case evStruct:
		s := v.(evStruct)
		for i := range d {
			store(&d[i], s[i])
		}
		return
	case evArray:
		a := v.(evArray)
		for i := range d {
			store(&d[i], a[i])
		}
		return
	}
	*loc = copyVal(v)
}

// Integers.

// intInfo is the width and signedness of an integer type (untyped
// integers are 64-bit signed).
func intInfo(t types.Type) (uint, bool) {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return 64, false
	}
	switch b.Kind() {
	case types.Int8:
		return 8, true
	case types.Int16:
		return 16, true
	case types.Int32, types.UntypedRune:
		return 32, true
	case types.Int64, types.Int, types.UntypedInt:
		return 64, true
	case types.Uint8:
		return 8, false
	case types.Uint16:
		return 16, false
	case types.Uint32:
		return 32, false
	}
	return 64, false
}

func trunc(v uint64, t types.Type) uint64 {
	if bits, _ := intInfo(t); bits < 64 {
		return v & (1<<bits - 1)
	}
	return v
}

// signed is v's value as an int64 by t's signedness: sign-extended for a
// signed type, reinterpreted for an unsigned one.
func signed(v uint64, t types.Type) int64 {
	bits, s := intInfo(t)
	if s && bits < 64 {
		shift := 64 - bits
		return int64(v<<shift) >> shift
	}
	return int64(v)
}

// widen is v as a u64 the way C's (u64) cast of a value of type t gives it:
// sign-extended for a signed type.
func widen(v uint64, t types.Type) uint64 { return uint64(signed(v, t)) }

func asInt(v value) uint64 {
	switch v := v.(type) {
	case uint64:
		return v
	case bool:
		if v {
			return 1
		}
		return 0
	}
	panic(fmt.Sprintf("not an integer: %T", v))
}

// constValue is constant v as a value of type t.
func (ev *evaluator) constValue(n ast.Node, v constant.Value, t types.Type) value {
	switch v.Kind() {
	case constant.Bool:
		return constant.BoolVal(v)
	case constant.String:
		return evString{s: constant.StringVal(v)}
	case constant.Int, constant.Float:
		i := constant.ToInt(v)
		if x, ok := constant.Int64Val(i); ok {
			return trunc(uint64(x), t)
		}
		if x, ok := constant.Uint64Val(i); ok {
			return trunc(x, t)
		}
	}
	ev.stopf(n, "constant %s is not evaluated at compile time", v)
	return nil
}

// Frames.

func (ev *evaluator) newFrame(fn *Function, results *types.Tuple) *evFrame {
	f := &evFrame{vars: map[types.Object]*value{}, results: results}
	if results != nil {
		f.named = namedResults(results)
	}
	_ = fn
	return f
}

// declare makes a new variable o holding v in frame f: on the heap when
// Check moved it there (each execution allocates), else local storage.
func (ev *evaluator) declare(n ast.Node, f *evFrame, o types.Object, v value) {
	if o == nil || o.Name() == "_" {
		return
	}
	v = ev.as(v, o.Type())
	if ev.moved[o] {
		f.vars[o] = ev.allocValue(n, o.Type(), v)
		return
	}
	loc := new(value)
	*loc = copyVal(v)
	f.vars[o] = loc
}

// varLoc is the storage of a variable: a local of the current frame or a
// package-level variable.
func (ev *evaluator) varLoc(n ast.Node, o types.Object) *value {
	if ev.frame != nil {
		if loc := ev.frame.vars[o]; loc != nil {
			return loc
		}
	}
	v, ok := o.(*types.Var)
	if ok && isPackageVar(v) {
		if loc := ev.globals[v]; loc != nil {
			return loc
		}
		if g := ev.byVar[v]; g != nil && g.ReadOnly {
			return ev.readOnly(n, g)
		}
	}
	ev.stopf(n, "internal error: no storage for %s", o.Name())
	return nil
}

// readOnly is a read-only variable's storage: its constant value, built
// without allocating (it is read-only data, not heap).
func (ev *evaluator) readOnly(n ast.Node, g *Global) *value {
	if loc := ev.ro[g.Var]; loc != nil {
		return loc
	}
	loc := new(value)
	*loc = ev.constInit(n, g.Init, g.Var.Type())
	ev.ro[g.Var] = loc
	return loc
}

// constInit is the value of a read-only variable's initializer: absent
// (zero), a constant, or a composite literal of constants.
func (ev *evaluator) constInit(n ast.Node, e ast.Expr, t types.Type) value {
	if e == nil || ev.info.Types[e].IsNil() {
		return ev.zero(t)
	}
	if tv := ev.info.Types[e]; tv.Value != nil {
		return ev.constValue(e, tv.Value, t)
	}
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	if !ok {
		ev.stopf(e, "internal error: read-only initializer")
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		s := ev.zero(t).(evStruct)
		for i, elt := range lit.Elts {
			field, value := i, elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				value, field = kv.Value, fieldByName(u, kv.Key.(*ast.Ident).Name)
			}
			s[field] = ev.constInit(n, value, u.Field(field).Type())
		}
		return s
	case *types.Array:
		a := ev.zero(t).(evArray)
		ev.constElements(n, lit, u.Elem(), a)
		return a
	case *types.Slice:
		arr := make(evArray, literalLength(ev.info, lit))
		for i := range arr {
			arr[i] = ev.zero(u.Elem())
		}
		ev.constElements(n, lit, u.Elem(), arr)
		loc := value(arr)
		return evSlice{arr: &loc, n: int64(len(arr)), c: int64(len(arr))}
	}
	ev.stopf(e, "internal error: read-only initializer")
	return nil
}

func (ev *evaluator) constElements(n ast.Node, lit *ast.CompositeLit, elem types.Type, out evArray) {
	index := int64(0)
	for _, elt := range lit.Elts {
		value := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			index, _ = constant.Int64Val(ev.info.Types[kv.Key].Value)
			value = kv.Value
		}
		out[index] = ev.constInit(n, value, elem)
		index++
	}
}

func fieldByName(st *types.Struct, name string) int {
	for j := 0; j < st.NumFields(); j++ {
		if st.Field(j).Name() == name {
			return j
		}
	}
	return -1
}

// fieldIndex is the index of the field a selector names.
func (ev *evaluator) fieldIndex(e *ast.SelectorExpr) int {
	if s := ev.info.Selections[e]; s != nil && s.Kind() == types.FieldVal && len(s.Index()) == 1 {
		return s.Index()[0]
	}
	t := ev.info.Types[e.X].Type
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		ev.stopf(e, "internal error: field of %s", t)
	}
	i := fieldByName(st, e.Sel.Name)
	if i < 0 {
		ev.stopf(e, "internal error: field %s", e.Sel.Name)
	}
	return i
}

// Hoisting (CheckedProgram.Hoisted), as the C emitter applies it.

func (ev *evaluator) hoist(n ast.Node) func() {
	order := ev.p.order
	if order == nil {
		return func() {}
	}
	list := order.hoisted[n]
	if list == nil {
		return func() {}
	}
	for _, e := range list {
		if order.converted[e] {
			ev.hb[e] = ev.eval(e)
		} else {
			ev.hv[e] = ev.evalValue(e)
		}
	}
	return func() {
		for _, e := range list {
			delete(ev.hv, e)
			delete(ev.hb, e)
		}
	}
}

func (ev *evaluator) hoisted(e ast.Expr, boxed bool) (value, bool) {
	if ev.p.order == nil {
		return nil, false
	}
	values := ev.hv
	if boxed {
		values = ev.hb
	}
	if src := ev.p.order.shared[e]; src != nil {
		v, ok := values[src]
		return v, ok
	}
	v, ok := values[e]
	return v, ok
}

// Statements.

type ctrl int

const (
	ctrlNext ctrl = iota
	ctrlBreak
	ctrlContinue
	ctrlReturn
)

func (ev *evaluator) execList(list []ast.Stmt) ctrl {
	for _, s := range list {
		if c := ev.exec(s); c != ctrlNext {
			return c
		}
	}
	return ctrlNext
}

func (ev *evaluator) exec(s ast.Stmt) (c ctrl) {
	ev.tick(s)
	defer ev.hoist(s)()
	switch s := s.(type) {
	case *ast.BlockStmt:
		return ev.execList(s.List)
	case *ast.ExprStmt:
		ev.eval(s.X)
	case *ast.ReturnStmt:
		f := ev.frame
		if len(s.Results) > 0 {
			values := ev.values(s.Results, tupleTypes(f.results))
			if f.named != nil {
				for i, v := range f.named {
					store(f.vars[v], ev.as(values[i], v.Type()))
				}
			} else {
				f.ret = values
			}
		}
		return ctrlReturn
	case *ast.AssignStmt:
		ev.assign(s)
	case *ast.IncDecStmt:
		ev.incDec(s)
	case *ast.DeclStmt:
		d := s.Decl.(*ast.GenDecl)
		if d.Tok == token.VAR {
			for _, sp := range d.Specs {
				v := sp.(*ast.ValueSpec)
				release := ev.hoist(v)
				ev.valueSpec(v)
				release()
			}
		}
	case *ast.IfStmt:
		if s.Init != nil {
			if c := ev.exec(s.Init); c != ctrlNext {
				return c
			}
		}
		if ev.eval(s.Cond).(bool) {
			return ev.exec(s.Body)
		} else if s.Else != nil {
			return ev.exec(s.Else)
		}
	case *ast.ForStmt:
		return ev.forStmt(s)
	case *ast.RangeStmt:
		return ev.rangeStmt(s)
	case *ast.SwitchStmt:
		return ev.switchStmt(s)
	case *ast.TypeSwitchStmt:
		return ev.typeSwitch(s)
	case *ast.BranchStmt:
		if s.Label != nil {
			ev.stopf(s, "internal error: labeled branch")
		}
		switch s.Tok {
		case token.BREAK:
			return ctrlBreak
		case token.CONTINUE:
			return ctrlContinue
		}
		ev.stopf(s, "internal error: %s", s.Tok)
	case *ast.DeferStmt:
		ev.deferStmt(s)
	case *ast.EmptyStmt:
	default:
		ev.stopf(s, "internal error: statement %T", s)
	}
	return ctrlNext
}

func (ev *evaluator) valueSpec(v *ast.ValueSpec) {
	f := ev.frame
	if len(v.Names) != 1 || len(v.Values) > 1 {
		if len(v.Values) > 0 {
			lhs := make([]ast.Expr, len(v.Names))
			for i, id := range v.Names {
				lhs[i] = id
			}
			ev.assignMany(lhs, v.Values, token.DEFINE)
			return
		}
		for _, id := range v.Names {
			if o := ev.info.Defs[id]; o != nil {
				ev.declare(v, f, o, ev.zero(o.Type()))
			}
		}
		return
	}
	o := ev.info.Defs[v.Names[0]]
	var val value
	if len(v.Values) == 1 {
		val = ev.evalAs(v.Values[0], o.Type())
	} else {
		val = ev.zero(o.Type())
	}
	ev.declare(v, f, o, val)
}

// place is assignable storage: a variable, field, element or pointer
// target (loc), a map element (set when stored), or the blank identifier.
type place struct {
	loc   *value
	m     *evMap
	mapOK bool
	mt    *MapType
	key   value
	t     types.Type
	blank bool
}

func (ev *evaluator) read(n ast.Node, p place) value {
	if p.mapOK {
		v, _ := ev.mapRead(p.m, p.mt, p.key)
		return v
	}
	return copyVal(*p.loc)
}

func (ev *evaluator) write(n ast.Node, p place, v value) {
	switch {
	case p.blank:
	case p.mapOK:
		e := ev.mapSet(n, p.m, p.key)
		e.elem = copyVal(ev.as(v, p.t))
	default:
		store(p.loc, ev.as(v, p.t))
	}
}

// target evaluates an assignment target's operands (captureTarget).
func (ev *evaluator) target(e ast.Expr) place {
	t := ev.info.Types[e].Type
	if id, ok := e.(*ast.Ident); ok {
		if id.Name == "_" {
			return place{blank: true}
		}
		o := ev.info.Uses[id]
		return place{loc: ev.varLoc(id, o), t: o.Type()}
	}
	if ix, ok := ev.isMapIndex(e); ok {
		m := ev.eval(ix.X).(*evMap)
		mt := ev.mapType(ix, ev.info.Types[ix.X].Type)
		k := ev.evalAs(ix.Index, mt.Type.Key())
		return place{m: m, mapOK: true, mt: mt, key: k, t: mt.Type.Elem()}
	}
	return place{loc: ev.addr(e), t: t}
}

func (ev *evaluator) isMapIndex(e ast.Expr) (*ast.IndexExpr, bool) {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	if !ok || !isMap(ev.info.Types[ix.X].Type) {
		return nil, false
	}
	return ix, true
}

func (ev *evaluator) assign(s *ast.AssignStmt) {
	if s.Tok == token.ADD_ASSIGN && len(s.Lhs) == 1 && isString(ev.info.Types[s.Lhs[0]].Type) {
		// s += t: the target's operands, the operands, then the target is
		// read and the concatenation stored.
		p := ev.target(s.Lhs[0])
		var values []evString
		for _, operand := range ev.concatOperands(s.Rhs[0], nil) {
			values = append(values, ev.eval(operand).(evString))
		}
		current := ev.read(s, p).(evString)
		ev.write(s, p, ev.concat(s, append([]evString{current}, values...)))
		return
	}
	if op, ok := compoundOps[s.Tok]; ok && len(s.Lhs) == 1 {
		lhs := ast.Unparen(s.Lhs[0])
		t := ev.info.Types[lhs].Type
		p := ev.target(lhs)
		y := ev.eval(s.Rhs[0])
		var x value
		if p.mapOK {
			e := ev.mapSet(s, p.m, p.key)
			x = e.elem
			e.elem = ev.binaryInt(s, op, t, ev.info.Types[s.Rhs[0]].Type, asInt(x), asInt(y))
			return
		}
		x = ev.read(s, p)
		ev.write(s, p, ev.binaryInt(s, op, t, ev.info.Types[s.Rhs[0]].Type, asInt(x), asInt(y)))
		return
	}
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		ev.assignMany(s.Lhs, s.Rhs, s.Tok)
		return
	}
	lhs := s.Lhs[0]
	if id, ok := lhs.(*ast.Ident); ok {
		if id.Name == "_" {
			ev.eval(s.Rhs[0])
			return
		}
		if s.Tok == token.DEFINE {
			o := ev.info.Defs[id]
			if o != nil {
				ev.declare(s, ev.frame, o, ev.evalAs(s.Rhs[0], o.Type()))
				return
			}
		}
		o := ev.info.Uses[id]
		loc := ev.varLoc(id, o)
		store(loc, ev.evalAs(s.Rhs[0], o.Type()))
		return
	}
	p := ev.target(lhs)
	v := ev.evalAs(s.Rhs[0], ev.info.Types[lhs].Type)
	ev.write(s, p, v)
}

// assignMany is a tuple assignment or definition: targets, then values,
// then stores left to right.
func (ev *evaluator) assignMany(lhs, rhs []ast.Expr, tok token.Token) {
	targets := make([]place, len(lhs))
	defs := make([]types.Object, len(lhs))
	for i, left := range lhs {
		if id, ok := left.(*ast.Ident); ok && tok == token.DEFINE {
			defs[i] = ev.info.Defs[id]
			if id.Name == "_" {
				targets[i] = place{blank: true}
				continue
			}
			if defs[i] != nil {
				continue
			}
		}
		targets[i] = ev.target(left)
	}
	expected := make([]types.Type, len(lhs))
	for i, left := range lhs {
		expected[i] = ev.info.Types[left].Type
		if defs[i] != nil {
			expected[i] = defs[i].Type()
		} else if id, ok := left.(*ast.Ident); ok && ev.info.Uses[id] != nil {
			expected[i] = ev.info.Uses[id].Type()
		}
	}
	values := ev.values(rhs, expected)
	if len(values) != len(lhs) {
		ev.stopf(lhs[0], "internal error: assignment arity")
	}
	for i, v := range values {
		if defs[i] != nil {
			ev.declare(lhs[i], ev.frame, defs[i], v)
			continue
		}
		ev.write(lhs[i], targets[i], ev.as(v, expected[i]))
	}
}

func (ev *evaluator) incDec(s *ast.IncDecStmt) {
	delta := uint64(1)
	if s.Tok == token.DEC {
		delta = ^uint64(0)
	}
	t := ev.info.Types[s.X].Type
	if ix, ok := ev.isMapIndex(s.X); ok {
		m := ev.eval(ix.X).(*evMap)
		mt := ev.mapType(ix, ev.info.Types[ix.X].Type)
		k := ev.evalAs(ix.Index, mt.Type.Key())
		e := ev.mapSet(s, m, k)
		e.elem = trunc(asInt(e.elem)+delta, t)
		return
	}
	p := ev.target(s.X)
	ev.write(s, p, trunc(asInt(ev.read(s, p))+delta, t))
}

func (ev *evaluator) forStmt(s *ast.ForStmt) ctrl {
	if s.Init != nil {
		if c := ev.exec(s.Init); c != ctrlNext {
			return c
		}
	}
	// Go 1.22: each iteration has its own copy of the heap-moved variables
	// the init statement declares, made before the post statement.
	var fresh []types.Object
	if init, ok := s.Init.(*ast.AssignStmt); ok && init.Tok == token.DEFINE {
		for _, lhs := range init.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				if o := ev.info.Defs[id]; o != nil && ev.moved[o] {
					fresh = append(fresh, o)
				}
			}
		}
	}
	for {
		ev.tick(s)
		if s.Cond != nil && !ev.eval(s.Cond).(bool) {
			return ctrlNext
		}
		switch ev.exec(s.Body) {
		case ctrlBreak:
			return ctrlNext
		case ctrlReturn:
			return ctrlReturn
		}
		for _, o := range fresh {
			old := ev.frame.vars[o]
			ev.frame.vars[o] = ev.allocValue(s, o.Type(), *old)
		}
		if s.Post != nil {
			ev.exec(s.Post)
		}
	}
}

func (ev *evaluator) rangeStmt(s *ast.RangeStmt) ctrl {
	xt := ev.info.Types[s.X].Type
	key, val := rangeTarget(s.Key), rangeTarget(s.Value)
	object := func(id *ast.Ident) types.Object {
		if s.Tok == token.DEFINE {
			return ev.info.Defs[id]
		}
		return ev.info.Uses[id]
	}
	assign := func(id *ast.Ident, v value) {
		o := object(id)
		if conv := ev.conversion(id); conv != nil && conv.rng {
			v = ev.box(id, conv, v)
		}
		if s.Tok == token.DEFINE {
			ev.declare(id, ev.frame, o, v)
		} else {
			store(ev.varLoc(id, o), v)
		}
	}
	elemType := func(id *ast.Ident) types.Type {
		if conv := ev.conversion(id); conv != nil && conv.rng {
			return conv.From
		}
		return object(id).Type()
	}
	body := func() (ctrl, bool) {
		switch ev.exec(s.Body) {
		case ctrlBreak:
			return ctrlNext, true
		case ctrlReturn:
			return ctrlReturn, true
		}
		return ctrlNext, false
	}
	if isString(xt) {
		str := ev.eval(s.X).(evString).s
		for i := 0; i < len(str); {
			ev.tick(s)
			r, width := decodeRune(str[i:])
			if key != nil {
				assign(key, trunc(uint64(i), elemType(key)))
			}
			if val != nil {
				assign(val, trunc(uint64(int64(r)), elemType(val)))
			}
			i += width
			if c, done := body(); done {
				return c
			}
		}
		return ctrlNext
	}
	var bound uint64
	var element func(i uint64) value
	if a, ok := arrayValue(xt); ok {
		bound = uint64(a.Len())
		if val == nil {
			ev.rangeOperandCalls(s.X)
		} else if _, isPointer := pointer(xt); isPointer {
			p := ev.eval(s.X).(evPtr)
			element = func(i uint64) value {
				if p.loc == nil {
					ev.stopf(s.X, "nil pointer dereference")
				}
				return copyVal((*p.loc).(evArray)[i])
			}
		} else {
			x := ev.eval(s.X).(evArray)
			element = func(i uint64) value { return copyVal(x[i]) }
		}
	} else if _, ok := xt.Underlying().(*types.Slice); ok {
		x := ev.eval(s.X).(evSlice)
		bound = uint64(x.n)
		element = func(i uint64) value { return copyVal((*x.arr).(evArray)[x.lo+int64(i)]) }
	} else if _, ok := xt.Underlying().(*types.Basic); ok {
		n := asInt(ev.eval(s.X))
		if isSigned(xt) && signed(n, xt) < 0 {
			n = 0
		}
		bound = n
	} else {
		ev.stopf(s, "internal error: range over %s", xt)
	}
	for i := uint64(0); i < bound; i++ {
		ev.tick(s)
		var read value
		if val != nil {
			read = element(i)
		}
		if key != nil {
			assign(key, trunc(i, elemType(key)))
		}
		if val != nil {
			assign(val, read)
		}
		if c, done := body(); done {
			return c
		}
	}
	return ctrlNext
}

// rangeOperandCalls runs the calls of an index-only array range operand.
func (ev *evaluator) rangeOperandCalls(e ast.Expr) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		ev.rangeOperandCalls(e.X)
	case *ast.SelectorExpr:
		ev.rangeOperandCalls(e.X)
	case *ast.StarExpr:
		ev.rangeOperandCalls(e.X)
	case *ast.CallExpr:
		ev.eval(e)
	}
}

func (ev *evaluator) switchStmt(s *ast.SwitchStmt) ctrl {
	if s.Init != nil {
		if c := ev.exec(s.Init); c != ctrlNext {
			return c
		}
	}
	var tag value = true
	var tagType types.Type
	if s.Tag != nil {
		tag = ev.eval(s.Tag)
		tagType = ev.info.Types[s.Tag].Type
	}
	run := func(body []ast.Stmt) ctrl {
		c := ev.execList(body)
		if c == ctrlBreak {
			return ctrlNext
		}
		return c
	}
	var fallback *ast.CaseClause
	for _, st := range s.Body.List {
		clause := st.(*ast.CaseClause)
		if clause.List == nil {
			fallback = clause
			continue
		}
		for _, test := range clause.List {
			v := ev.eval(test)
			matched := false
			if s.Tag == nil {
				matched = v.(bool)
			} else {
				matched = ev.equalOperands(test, s.Tag, tag, tagType, test, v, ev.info.Types[test].Type)
			}
			if matched {
				return run(clause.Body)
			}
		}
	}
	if fallback != nil {
		return run(fallback.Body)
	}
	return ctrlNext
}

func (ev *evaluator) typeSwitch(s *ast.TypeSwitchStmt) ctrl {
	if s.Init != nil {
		if c := ev.exec(s.Init); c != ctrlNext {
			return c
		}
	}
	x := ev.eval(typeSwitchGuard(s).X).(evIface)
	bind := func(clause *ast.CaseClause) {
		obj := ev.info.Implicits[clause]
		if obj == nil {
			return
		}
		var v value = x
		if len(clause.List) == 1 && !isIface(obj.Type()) {
			v = ev.unbox(clause, obj.Type(), x)
		}
		ev.declare(clause, ev.frame, obj, v)
	}
	run := func(clause *ast.CaseClause) ctrl {
		bind(clause)
		c := ev.execList(clause.Body)
		if c == ctrlBreak {
			return ctrlNext
		}
		return c
	}
	var fallback *ast.CaseClause
	for _, st := range s.Body.List {
		clause := st.(*ast.CaseClause)
		if clause.List == nil {
			fallback = clause
			continue
		}
		matched := false
		for _, e := range clause.List {
			if tv := ev.info.Types[e]; tv.IsNil() {
				matched = matched || x.dyn == nil
			} else {
				matched = matched || ev.typeTest(e, tv.Type, x)
			}
		}
		if matched {
			return run(clause)
		}
	}
	if fallback != nil {
		return run(fallback)
	}
	return ctrlNext
}

// deferStmt evaluates a deferred call's function, receiver and arguments
// now and queues the call; a record inside a loop is heap-allocated.
func (ev *evaluator) deferStmt(s *ast.DeferStmt) {
	site := ev.p.funcs.byDefer[s]
	if site == nil {
		ev.stopf(s, "internal error: defer site")
	}
	call := s.Call
	var run func()
	switch site.Kind {
	case DeferFunction:
		fn := ev.function(call, ev.calleeObject(call))
		args := ev.callArgs(call)
		run = func() { ev.call(fn, nil, args) }
	case DeferMethod:
		selector := call.Fun.(*ast.SelectorExpr)
		fn, recv, args := ev.methodArgs(call, selector)
		run = func() { ev.call(fn, recv, args) }
	case DeferDynamic:
		selector := call.Fun.(*ast.SelectorExpr)
		x := ev.eval(selector.X).(evIface)
		args := ev.callArgs(call)
		if x.dyn == nil {
			ev.stopf(s, "nil interface in defer")
		}
		run = func() { ev.dynamicInvoke(call, x, args) }
	case DeferIndirect, DeferLiteral:
		f := ev.eval(call.Fun).(evFunc)
		args := ev.callArgs(call)
		run = func() { ev.callFunc(call, f, args) }
	case DeferCopy:
		t := ev.info.Types[call.Args[0]].Type
		dst, src := ev.evalAs(call.Args[0], t), ev.evalAs(call.Args[1], t)
		run = func() { ev.copySlice(dst, src) }
	default:
		ev.stopf(s, "internal error: defer kind")
	}
	if site.InLoop {
		o := ev.alloc(s, site.RecordBytes)
		o.kind = InitObjectValue // a dead record: nothing reaches it after the call
	}
	ev.frame.defers = append(ev.frame.defers, run)
}

// concatOperands are the leaves of a maximal tree of non-constant string
// additions (the C emitter's concatOperands).
func (ev *evaluator) concatOperands(e ast.Expr, out []ast.Expr) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD && ev.info.Types[b].Value == nil && isString(ev.info.Types[b].Type) {
		return ev.concatOperands(b.Y, ev.concatOperands(b.X, out))
	}
	return append(out, e)
}

// concat is gosvm_concat: "" without non-empty operands, the operand itself
// when only one is non-empty, else one allocation of the total length.
func (ev *evaluator) concat(n ast.Node, values []evString) evString {
	total, count, idx := 0, 0, 0
	for i, v := range values {
		if len(v.s) == 0 {
			continue
		}
		total += len(v.s)
		count++
		idx = i
	}
	switch count {
	case 0:
		return evString{}
	case 1:
		return values[idx]
	}
	var b strings.Builder
	for _, v := range values {
		b.WriteString(v.s)
	}
	return ev.allocBytes(n, int64(total), b.String())
}
