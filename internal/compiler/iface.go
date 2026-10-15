package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// Interface values. A value of interface type is two words: a dynamic type
// ID (0 for a nil interface) and a data word. The data word holds a pointer
// value itself, or points at a heap copy (box) of any other value; values of
// zero size are not copied. Dynamic dispatch, assertions, type switches and
// comparisons test the ID against the program's static list of dynamic
// types. See docs/GO_INTERFACES.md.

// Conversion is one conversion of a non-interface value to an interface type,
// implicit (assignment, argument, result, composite-literal element, append
// value, range assignment) or explicit (I(x)).
type Conversion struct {
	// Expr is the converted expression as written. For a range statement
	// that assigns elements (or keys) to interface variables, it is the key
	// or value identifier, and the element is converted when assigned.
	Expr ast.Expr
	// Index is -1 when Expr's own value is converted, or the element of
	// Expr's multiple-value result (a call, or a comma-ok type assertion).
	Index int
	// From is the concrete type; types.Typ[types.UntypedNil] for nil, which
	// converts to the nil interface value without allocating.
	From types.Type
	// To is the interface type.
	To types.Type

	rng bool // a range statement's key or value
}

// MethodSlot is one distinct interface method (name, package for unexported
// names, and signature) that the program calls dynamically or whose
// interface it asserts to. Slots are sorted by Key; Index is the position.
type MethodSlot struct {
	Index int
	// Key spells the method: Name(params) results, with the package path
	// before an unexported name.
	Key  string
	Name string
	// Pkg is the package of an unexported method name, else nil.
	Pkg *types.Package
	// Signature is an interface method's signature; the receiver is not
	// part of the slot.
	Signature *types.Signature
	// Called is true when a dynamic method call uses the slot.
	Called bool
	// Site is the first call or assertion that needs the slot.
	Site token.Pos
	// ExceptImplementers is set only on the slots of the dynamic calls in
	// standard-library overlay functions marked //gosvm:dispatch-except
	// (docs/GO_STDLIB.md#intrinsics): an interface type whose implementers
	// the slot never runs. They do not fill the slot, and its dispatcher
	// aborts for them as for a nil interface; Except lists their keys, in
	// dynamic type order. Such a slot is distinct from the unrestricted slot
	// of the same method: its Key ends with " except implementers of " and
	// the interface's key. Both are nil for every other slot.
	ExceptImplementers types.Type
	Except             []string
}

// sameExcept reports whether two slot exclusions are the same.
func sameExcept(a, b types.Type) bool {
	if a == nil || b == nil {
		return a == b
	}
	return types.Identical(a, b)
}

// DynamicType is a concrete type that an interface value may hold, or that
// code compares or asserts interface values against. IDs are 1-based in Key
// order; 0 is the nil interface.
type DynamicType struct {
	Type types.Type
	Key  string
	ID   uint64
	// Boxed is true when the program converts values of the type to an
	// interface; Methods are listed only then.
	Boxed bool
	// Direct is true for pointer and map types: the data word is the pointer
	// (for a map, its header pointer).
	Direct bool
	// Size is the Go size in bytes. A conversion allocates (Size+7)&^7 heap
	// bytes unless Direct or Size is 0 (then it allocates nothing).
	Size int64
	// Comparable reports whether == on two interface values holding this
	// type compares values (false: the comparison aborts, as Go panics).
	Comparable bool
	// Methods are the slots the type's method set implements, in slot
	// order.
	Methods []*DynamicMethod
}

// DynamicMethod is one method-table entry: the function a dynamic call of
// Slot runs for a dynamic type.
type DynamicMethod struct {
	Slot *MethodSlot
	// Func is the method to call: the declared method, or, for a method of
	// an instantiated generic type, the instance's Object.
	Func *types.Func
	// Deref is true when the dynamic type is *T and the method has a value
	// receiver: the call copies *data, and a nil pointer traps.
	Deref bool
}

// isIface reports whether t is an interface value type. Type parameters,
// whose underlying type is their constraint interface, are not.
func isIface(t types.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(*types.TypeParam); ok {
		return false
	}
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// interfaceRule admits an interface value type. A dynamic call passes and
// returns every method's parameters and results, so they must be storable
// values; variadic methods are rejected. The predeclared error is an
// ordinary interface: its Error() returns a string.
func (f *frontend) interfaceRule(n ast.Node, t types.Type) {
	it := t.Underlying().(*types.Interface)
	if f.ifaceChecked == nil {
		f.ifaceChecked = map[*types.Interface]bool{}
	}
	if f.ifaceChecked[it] {
		return
	}
	f.ifaceChecked[it] = true
	for i := 0; i < it.NumMethods(); i++ {
		m := it.Method(i)
		sig := m.Type().(*types.Signature)
		if sig.Variadic() {
			f.fail(n, "interface method %s: variadics are unsupported", m.Name())
		}
		for _, tuple := range []*types.Tuple{sig.Params(), sig.Results()} {
			for j := 0; j < tuple.Len(); j++ {
				f.methodTypeRule(n, m.Name(), tuple.At(j).Type())
			}
		}
	}
}

// methodTypeRule applies the storage rule to an interface method's
// parameter or result type, naming the method in a diagnostic.
func (f *frontend) methodTypeRule(n ast.Node, method string, t types.Type) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(compileError)
			if !ok {
				panic(r)
			}
			message := strings.TrimPrefix(string(e), "-: ")
			if message != string(e) {
				f.fail(n, "interface method %s: %s", method, message)
			}
			panic(r)
		}
	}()
	// Arrays (and pointers to them) are bounded here and declared where a
	// dispatcher or an implementation uses them.
	f.typeArgRule(posNode(token.NoPos), t)
}

// ifaceScan collects the interface operations of type-checked syntax.
type ifaceScan struct {
	info        *types.Info
	conversions []*Conversion
	calls       []*ast.CallExpr
	asserts     []*ast.TypeAssertExpr
	switches    []*ast.TypeSwitchStmt
	compared    []typeSite // concrete operands compared with interface values
}

type typeSite struct {
	t    types.Type
	site token.Pos
}

// ScanInterfaces reports the interface conversions, dynamic method calls and
// type assertions (excluding type switch guards) in files, in source order.
// It works on any type-checked syntax, including generic declarations, where
// a type parameter converted to an interface counts as a conversion.
func ScanInterfaces(info *types.Info, files []*ast.File) (conversions []*Conversion, calls []*ast.CallExpr, asserts []*ast.TypeAssertExpr) {
	s := scanInterfaces(info, files)
	return s.conversions, s.calls, s.asserts
}

func scanInterfaces(info *types.Info, files []*ast.File) *ifaceScan {
	s := &ifaceScan{info: info}
	for _, file := range files {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				var results *types.Tuple
				if o, ok := info.Defs[fn.Name].(*types.Func); ok {
					results = o.Type().(*types.Signature).Results()
				}
				s.walk(fn.Body, results)
			}
		}
	}
	return s
}

func (s *ifaceScan) record(e ast.Expr, index int, from, to types.Type) {
	s.conversions = append(s.conversions, &Conversion{Expr: e, Index: index, From: from, To: to})
}

// flow records the conversion, if any, of e's value to target.
func (s *ifaceScan) flow(e ast.Expr, target types.Type) {
	if !isIface(target) {
		return
	}
	tv, ok := s.info.Types[e]
	if !ok {
		return
	}
	if tv.IsNil() {
		s.record(e, -1, types.Typ[types.UntypedNil], target)
		return
	}
	if _, tuple := tv.Type.(*types.Tuple); tuple || tv.Type == nil || isIface(tv.Type) {
		return
	}
	s.record(e, -1, tv.Type, target)
}

// values records the conversions of values to targets: one value per target,
// or one multiple-value expression for all of them.
func (s *ifaceScan) values(values []ast.Expr, targets []types.Type) {
	if len(values) == len(targets) {
		for i, v := range values {
			s.flow(v, targets[i])
		}
		return
	}
	if len(values) != 1 {
		return
	}
	tuple, ok := s.info.Types[values[0]].Type.(*types.Tuple)
	if !ok || tuple.Len() != len(targets) {
		return
	}
	for i := 0; i < tuple.Len(); i++ {
		if isIface(targets[i]) && !isIface(tuple.At(i).Type()) {
			s.record(values[0], i, tuple.At(i).Type(), targets[i])
		}
	}
}

// target is the type a value assigned to e takes; nil when the assignment
// declares e (it takes the value's type) or discards the value.
func (s *ifaceScan) target(e ast.Expr, define bool) types.Type {
	if id, ok := e.(*ast.Ident); ok {
		if id.Name == "_" {
			return nil
		}
		if define && s.info.Defs[id] != nil {
			return nil
		}
		if o := s.info.Uses[id]; o != nil {
			return o.Type()
		}
	}
	return s.info.Types[e].Type
}

func tupleTypes(t *types.Tuple) []types.Type {
	out := make([]types.Type, t.Len())
	for i := range out {
		out[i] = t.At(i).Type()
	}
	return out
}

// callSignature is the signature a call's function expression has, with any
// explicit instantiation applied.
func (s *ifaceScan) callSignature(call *ast.CallExpr) *types.Signature {
	for e := call.Fun; e != nil; {
		if sig, ok := s.info.Types[e].Type.(*types.Signature); ok {
			return sig
		}
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return nil
		}
	}
	return nil
}

func (s *ifaceScan) walk(root ast.Node, results *types.Tuple) {
	ast.Inspect(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			var r *types.Tuple
			if sig, ok := s.info.Types[n].Type.(*types.Signature); ok {
				r = sig.Results()
			}
			s.walk(n.Body, r)
			return false
		case *ast.AssignStmt:
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				return true
			}
			targets := make([]types.Type, len(n.Lhs))
			for i, lhs := range n.Lhs {
				targets[i] = s.target(lhs, n.Tok == token.DEFINE)
			}
			s.values(n.Rhs, targets)
		case *ast.ValueSpec:
			if n.Type != nil && len(n.Values) > 0 {
				targets := make([]types.Type, len(n.Names))
				for i := range targets {
					targets[i] = s.info.Types[n.Type].Type
				}
				s.values(n.Values, targets)
			}
		case *ast.ReturnStmt:
			if results != nil && len(n.Results) > 0 {
				s.values(n.Results, tupleTypes(results))
			}
		case *ast.CallExpr:
			s.call(n)
		case *ast.CompositeLit:
			s.literal(n)
		case *ast.IndexExpr:
			// A map key converts to the map's key type.
			if t := s.info.Types[n.X].Type; isMap(t) {
				s.flow(n.Index, mapOf(t).Key())
			}
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				s.rangeAssign(n)
			}
		case *ast.TypeAssertExpr:
			if n.Type != nil {
				s.asserts = append(s.asserts, n)
			}
		case *ast.TypeSwitchStmt:
			s.switches = append(s.switches, n)
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				s.compare(n.X, n.Y)
			}
		case *ast.SwitchStmt:
			if n.Tag != nil {
				for _, stmt := range n.Body.List {
					for _, value := range stmt.(*ast.CaseClause).List {
						s.compare(n.Tag, value)
					}
				}
			}
		}
		return true
	})
}

// compare records the concrete operand of a comparison with an interface.
func (s *ifaceScan) compare(x, y ast.Expr) {
	tx, ty := s.info.Types[x], s.info.Types[y]
	switch {
	case isIface(tx.Type) && !isIface(ty.Type) && !ty.IsNil() && ty.Type != nil:
		s.compared = append(s.compared, typeSite{ty.Type, y.Pos()})
	case isIface(ty.Type) && !isIface(tx.Type) && !tx.IsNil() && tx.Type != nil:
		s.compared = append(s.compared, typeSite{tx.Type, x.Pos()})
	}
}

func (s *ifaceScan) call(n *ast.CallExpr) {
	fun := ast.Unparen(n.Fun)
	if tv := s.info.Types[fun]; tv.IsType() {
		if len(n.Args) == 1 {
			s.flow(n.Args[0], tv.Type)
		}
		return
	}
	if id, ok := fun.(*ast.Ident); ok {
		if b, ok := s.info.Uses[id].(*types.Builtin); ok {
			if b.Name() == "append" && !n.Ellipsis.IsValid() && len(n.Args) > 1 {
				if t := s.info.Types[n].Type; t != nil {
					if slice, ok := t.Underlying().(*types.Slice); ok {
						for _, v := range n.Args[1:] {
							s.flow(v, slice.Elem())
						}
					}
				}
			}
			if b.Name() == "delete" && len(n.Args) == 2 {
				if t := s.info.Types[n.Args[0]].Type; isMap(t) {
					s.flow(n.Args[1], mapOf(t).Key())
				}
			}
			return
		}
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if selection := s.info.Selections[sel]; selection != nil && selection.Kind() == types.MethodVal && isIface(s.info.Types[sel.X].Type) {
			s.calls = append(s.calls, n)
		}
	}
	sig := s.callSignature(n)
	if sig == nil {
		return
	}
	params := sig.Params()
	targets := make([]types.Type, len(n.Args))
	for i := range targets {
		switch {
		case sig.Variadic() && i >= params.Len()-1:
			last := params.At(params.Len() - 1).Type()
			if n.Ellipsis.IsValid() {
				targets[i] = last
			} else if slice, ok := last.Underlying().(*types.Slice); ok {
				targets[i] = slice.Elem()
			}
		case i < params.Len():
			targets[i] = params.At(i).Type()
		}
	}
	if len(n.Args) == 1 && params.Len() > 1 {
		targets = tupleTypes(params)
	}
	s.values(n.Args, targets)
}

func (s *ifaceScan) literal(n *ast.CompositeLit) {
	t := s.info.Types[n].Type
	if t == nil {
		return
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		for i, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok {
					for j := 0; j < u.NumFields(); j++ {
						if u.Field(j).Name() == key.Name {
							s.flow(kv.Value, u.Field(j).Type())
						}
					}
				}
				continue
			}
			if i < u.NumFields() {
				s.flow(elt, u.Field(i).Type())
			}
		}
	case *types.Array:
		s.elements(n, u.Elem())
	case *types.Slice:
		s.elements(n, u.Elem())
	case *types.Map:
		for _, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				s.flow(kv.Key, u.Key())
				s.flow(kv.Value, u.Elem())
			}
		}
	}
}

func (s *ifaceScan) elements(n *ast.CompositeLit, elem types.Type) {
	for _, elt := range n.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			elt = kv.Value
		}
		s.flow(elt, elem)
	}
}

// rangeAssign records range keys or elements assigned to interface
// variables.
func (s *ifaceScan) rangeAssign(n *ast.RangeStmt) {
	xt := s.info.Types[n.X].Type
	if xt == nil {
		return
	}
	var key, value types.Type
	u := xt.Underlying()
	if p, ok := u.(*types.Pointer); ok {
		u = p.Elem().Underlying()
	}
	switch u := u.(type) {
	case *types.Basic:
		key = xt
		if u.Info()&types.IsUntyped != 0 {
			key = types.Typ[types.Int]
		}
		if u.Info()&types.IsString != 0 {
			key, value = types.Typ[types.Int], runeType
		}
	case *types.Array:
		key, value = types.Typ[types.Int], u.Elem()
	case *types.Slice:
		key, value = types.Typ[types.Int], u.Elem()
	}
	for _, pair := range []struct {
		e ast.Expr
		t types.Type
	}{{n.Key, key}, {n.Value, value}} {
		if pair.e == nil || pair.t == nil {
			continue
		}
		if target := s.target(pair.e, false); isIface(target) && !isIface(pair.t) {
			s.conversions = append(s.conversions, &Conversion{Expr: pair.e, Index: -1, From: pair.t, To: target, rng: true})
		}
	}
}

// ifaceProgram is a checked program's interface information.
type ifaceProgram struct {
	used        bool
	conversions []*Conversion
	byExpr      map[ast.Expr]*Conversion
	byTuple     map[ast.Expr][]*Conversion
	dynamic     []*DynamicType
	byKey       map[string]*DynamicType
	slots       []*MethodSlot
	calls       map[*ast.CallExpr]*MethodSlot
	asserted    []types.Type // interface types asserted to or switched on, by key
	asserts     []*ast.TypeAssertExpr
	switches    []*ast.TypeSwitchStmt
}

// methodSlotKey spells an interface method for slot ordering.
func methodSlotKey(m *types.Func) string {
	var b strings.Builder
	if !m.Exported() && m.Pkg() != nil {
		b.WriteString(m.Pkg().Path() + ".")
	}
	b.WriteString(m.Name())
	writeSignature(&b, m.Type().(*types.Signature), func(p *types.Package) string { return p.Path() })
	return b.String()
}

// findSlot returns the slot of an interface method, adding it if new.
func findSlot(slots *[]*MethodSlot, m *types.Func, site token.Pos, except types.Type) *MethodSlot {
	sig := m.Type().(*types.Signature)
	for _, slot := range *slots {
		if slot.Name == m.Name() && (m.Exported() || slot.Pkg == m.Pkg()) && types.Identical(slot.Signature, sig) && sameExcept(slot.ExceptImplementers, except) {
			return slot
		}
	}
	slot := &MethodSlot{Key: methodSlotKey(m), Name: m.Name(), Signature: sig, Site: site, ExceptImplementers: except}
	if except != nil {
		slot.Key += " except implementers of " + typeKey(except)
	}
	if !m.Exported() {
		slot.Pkg = m.Pkg()
	}
	*slots = append(*slots, slot)
	return slot
}

// methodFunc is the function implementing a method-set entry: the declared
// method, or the instance of a generic type's method (nil when it has not
// been instantiated).
func methodFunc(m *types.Func, recv types.Type, instances map[string]*Instance) *types.Func {
	base := recvBase(recv)
	if base == nil || base.TypeArgs().Len() == 0 {
		return m
	}
	key, _ := instanceKey(m.Origin(), typeList(base.TypeArgs()))
	if inst := instances[key]; inst != nil {
		return inst.Object
	}
	return nil
}

// buildInterfaces derives the interface information of lowered files.
// instances resolves methods of instantiated generic types; excepts maps the
// dynamic calls of standard-library dispatch-except functions to the
// interface whose implementers their slots exclude (stdlibExcepts).
func buildInterfaces(info *types.Info, files []*ast.File, instances map[string]*Instance, excepts map[*ast.CallExpr]types.Type) *ifaceProgram {
	s := scanInterfaces(info, files)
	p := &ifaceProgram{byExpr: map[ast.Expr]*Conversion{}, byTuple: map[ast.Expr][]*Conversion{}, byKey: map[string]*DynamicType{},
		calls: map[*ast.CallExpr]*MethodSlot{}, conversions: s.conversions, asserts: s.asserts, switches: s.switches}
	for _, c := range s.conversions {
		if c.Index < 0 {
			p.byExpr[c.Expr] = c
		} else {
			p.byTuple[c.Expr] = append(p.byTuple[c.Expr], c)
		}
	}
	// Method slots: every method called dynamically, and every method of an
	// interface asserted to.
	var slots []*MethodSlot
	for _, call := range s.calls {
		sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		m, ok := info.Uses[sel.Sel].(*types.Func)
		if !ok {
			continue
		}
		slot := findSlot(&slots, m, call.Pos(), excepts[call])
		slot.Called = true
		p.calls[call] = slot
	}
	addAsserted := func(t types.Type, site token.Pos) {
		for _, existing := range p.asserted {
			if types.Identical(existing, t) {
				return
			}
		}
		p.asserted = append(p.asserted, t)
		it := t.Underlying().(*types.Interface)
		for i := 0; i < it.NumMethods(); i++ {
			findSlot(&slots, it.Method(i), site, nil)
		}
	}
	var concrete []typeSite
	addTarget := func(e ast.Expr) {
		tv := info.Types[e]
		switch {
		case tv.IsNil() || tv.Type == nil:
		case isIface(tv.Type):
			addAsserted(tv.Type, e.Pos())
		default:
			concrete = append(concrete, typeSite{tv.Type, e.Pos()})
		}
	}
	for _, a := range s.asserts {
		addTarget(a.Type)
	}
	for _, sw := range s.switches {
		for _, stmt := range sw.Body.List {
			for _, e := range stmt.(*ast.CaseClause).List {
				addTarget(e)
			}
		}
	}
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].Key < slots[j].Key })
	for i, slot := range slots {
		slot.Index = i
	}
	p.slots = slots
	sort.SliceStable(p.asserted, func(i, j int) bool { return typeKey(p.asserted[i]) < typeKey(p.asserted[j]) })
	// Dynamic types: converted types, assertion and case targets, and the
	// concrete operands of comparisons with interfaces.
	add := func(t types.Type, boxed bool) {
		key := typeKey(t)
		d := p.byKey[key]
		if d == nil {
			d = &DynamicType{Type: t, Key: key}
			p.byKey[key] = d
			p.dynamic = append(p.dynamic, d)
		}
		d.Boxed = d.Boxed || boxed
	}
	for _, c := range s.conversions {
		if c.From != types.Typ[types.UntypedNil] {
			add(c.From, true)
		}
	}
	for _, c := range append(concrete, s.compared...) {
		add(c.t, false)
	}
	sort.Slice(p.dynamic, func(i, j int) bool { return p.dynamic[i].Key < p.dynamic[j].Key })
	sizes := &types.StdSizes{WordSize: 8, MaxAlign: 8}
	for i, d := range p.dynamic {
		d.ID = uint64(i + 1)
		_, isPointer := d.Type.(*types.Pointer)
		// Pointers and maps (a pointer to the map header) are stored in
		// the data word.
		d.Direct = isPointer || isMap(d.Type)
		d.Size = sizes.Sizeof(d.Type)
		d.Comparable = types.Comparable(d.Type)
		if !d.Boxed {
			continue
		}
		set := types.NewMethodSet(d.Type)
		for _, slot := range slots {
			if slot.ExceptImplementers != nil && types.Implements(d.Type, slot.ExceptImplementers.Underlying().(*types.Interface)) {
				slot.Except = append(slot.Except, d.Key)
				continue
			}
			selection := set.Lookup(slot.Pkg, slot.Name)
			if selection == nil {
				continue
			}
			m, ok := selection.Obj().(*types.Func)
			if !ok || !types.Identical(m.Type(), slot.Signature) {
				continue
			}
			fn := methodFunc(m, d.Type, instances)
			if fn == nil {
				continue
			}
			_, pointerRecv := m.Type().(*types.Signature).Recv().Type().(*types.Pointer)
			d.Methods = append(d.Methods, &DynamicMethod{Slot: slot, Func: fn, Deref: isPointer && !pointerRecv})
		}
	}
	p.used = len(s.conversions) > 0 || len(s.asserts) > 0 || len(s.switches) > 0 || mentionsInterfaces(info, files)
	return p
}

// mentionsInterfaces reports whether any declared object or expression in
// files has a type that contains an interface value type.
func mentionsInterfaces(info *types.Info, files []*ast.File) bool {
	seen := map[types.Type]bool{}
	var mentions func(types.Type) bool
	mentions = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		if isIface(t) {
			return true
		}
		switch t := t.(type) {
		case *types.Named:
			if pkg := t.Obj().Pkg(); pkg != nil && pkg.Path() == "gosvm/solana" {
				return false
			}
			return mentions(t.Underlying())
		case *types.Pointer:
			return mentions(t.Elem())
		case *types.Array:
			return mentions(t.Elem())
		case *types.Slice:
			return mentions(t.Elem())
		case *types.Map:
			return mentions(t.Key()) || mentions(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				if mentions(t.Field(i).Type()) {
					return true
				}
			}
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				if mentions(t.At(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			return mentions(t.Params()) || mentions(t.Results()) || t.Recv() != nil && mentions(t.Recv().Type())
		}
		return false
	}
	found := false
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			if id, ok := n.(*ast.Ident); ok {
				if o := info.Defs[id]; o != nil && mentions(o.Type()) {
					found = true
				}
			}
			if e, ok := n.(ast.Expr); ok {
				if tv, ok := info.Types[e]; ok && mentions(tv.Type) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}

// dynamicType is the dynamic type entry for t (Check has listed it).
func (p *ifaceProgram) dynamicType(t types.Type) *DynamicType { return p.byKey[typeKey(t)] }

// assertedIndex is the position of an asserted interface type.
func (p *ifaceProgram) assertedIndex(t types.Type) int {
	for i, a := range p.asserted {
		if types.Identical(a, t) {
			return i
		}
	}
	return -1
}

// implementers lists the boxed dynamic types that implement interface t.
func (p *ifaceProgram) implementers(t types.Type) []*DynamicType {
	it := t.Underlying().(*types.Interface)
	var out []*DynamicType
	for _, d := range p.dynamic {
		if d.Boxed && types.Implements(d.Type, it) {
			out = append(out, d)
		}
	}
	return out
}

// targets lists, in dynamic type order, the functions a dynamic call of
// slot may run.
func (p *ifaceProgram) targets(slot *MethodSlot) []*types.Func {
	var out []*types.Func
	for _, d := range p.dynamic {
		for _, m := range d.Methods {
			if m.Slot == slot {
				out = append(out, m.Func)
			}
		}
	}
	return out
}

// Implementers lists the dynamic types converted to an interface somewhere
// in the program that implement interface type t: the values an interface of
// type t (or one asserted to t) can hold.
func (p *CheckedProgram) Implementers(t types.Type) []*DynamicType {
	return p.ifaces.implementers(t)
}

// runInterfaces instantiates the methods of generic types whose values are
// converted to interfaces, for every slot their method set implements, until
// no new instance appears: a dynamic call can reach them without a direct
// call naming them.
func (gp *genericProgram) runInterfaces(files []*ast.File) {
	for {
		s := scanInterfaces(gp.info, gp.lowered(files))
		var slots []*MethodSlot
		for _, call := range s.calls {
			if m, ok := gp.info.Uses[ast.Unparen(call.Fun).(*ast.SelectorExpr).Sel].(*types.Func); ok {
				findSlot(&slots, m, call.Pos(), nil)
			}
		}
		target := func(e ast.Expr) {
			if t := gp.info.Types[e].Type; isIface(t) {
				it := t.Underlying().(*types.Interface)
				for i := 0; i < it.NumMethods(); i++ {
					findSlot(&slots, it.Method(i), e.Pos(), nil)
				}
			}
		}
		for _, a := range s.asserts {
			target(a.Type)
		}
		for _, sw := range s.switches {
			for _, stmt := range sw.Body.List {
				for _, e := range stmt.(*ast.CaseClause).List {
					target(e)
				}
			}
		}
		requested := false
		for _, c := range s.conversions {
			base := recvBase(c.From)
			if base == nil || base.TypeArgs().Len() == 0 || containsTypeParam(base) {
				continue
			}
			set := types.NewMethodSet(c.From)
			for _, slot := range slots {
				selection := set.Lookup(slot.Pkg, slot.Name)
				if selection == nil {
					continue
				}
				m, ok := selection.Obj().(*types.Func)
				if !ok || !types.Identical(m.Type(), slot.Signature) {
					continue
				}
				args := typeList(base.TypeArgs())
				key, _ := instanceKey(m.Origin(), args)
				if gp.instances[key] == nil && gp.decls[m.Origin()] != nil {
					if gp.requestAt(m.Origin(), args, posNode(c.Expr.Pos()), nil) != nil {
						requested = true
					}
				}
			}
		}
		if !requested {
			return
		}
		gp.drain()
	}
}

// writeSignature writes a signature's parameter and result types.
func writeSignature(b *strings.Builder, sig *types.Signature, qualifier types.Qualifier) {
	write := func(t *types.Tuple) {
		b.WriteString("(")
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			if sig.Variadic() && t == sig.Params() && i == t.Len()-1 {
				b.WriteString("...")
			}
			writeType(b, t.At(i).Type(), qualifier)
		}
		b.WriteString(")")
	}
	write(sig.Params())
	if sig.Results().Len() > 0 {
		b.WriteString(" ")
		write(sig.Results())
	}
}

// writeInterface spells an interface type by its method set, so identical
// interfaces (any, interface{}, and spellings with byte or uint8) share a key.
func writeInterface(b *strings.Builder, t *types.Interface, qualifier types.Qualifier) {
	b.WriteString("interface{")
	for i := 0; i < t.NumMethods(); i++ {
		if i > 0 {
			b.WriteString("; ")
		}
		m := t.Method(i)
		if !m.Exported() && m.Pkg() != nil {
			if q := qualifier(m.Pkg()); q != "" {
				b.WriteString(q + ".")
			}
		}
		b.WriteString(m.Name())
		writeSignature(b, m.Type().(*types.Signature), qualifier)
	}
	b.WriteString("}")
}

type slotArray struct {
	typ  *types.Array
	site token.Pos
}

// slotArrays lists the array types that called method slots pass or return
// (by value or through a pointer), in slot order.
func slotArrays(slots []*MethodSlot) []slotArray {
	var out []slotArray
	for _, slot := range slots {
		if !slot.Called {
			continue
		}
		for _, tuple := range []*types.Tuple{slot.Signature.Params(), slot.Signature.Results()} {
			for i := 0; i < tuple.Len(); i++ {
				if a, ok := arrayValue(tuple.At(i).Type()); ok {
					out = append(out, slotArray{a, slot.Site})
				}
			}
		}
	}
	return out
}

// Checker rules.

// conversionRule admits converting a value of conv.From to an interface.
func (c *checker) conversionRule(n ast.Node, conv *Conversion) {
	if conv.From == types.Typ[types.UntypedNil] {
		c.typeRule(n, conv.To)
		return
	}
	if isContext(conv.From) {
		c.fail(n, "Context values cannot be converted to interfaces")
	}
	c.variableRule(n, conv.From)
	c.typeRule(n, conv.To)
	if (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(conv.From) != 0 && zeroSizePart(conv.From) {
		c.fail(n, "interface values must not hold %s: it contains zero-length arrays or empty structs", conv.From)
	}
}

// tupleConversions checks the conversions of a multiple-value expression's
// elements.
func (c *checker) tupleConversions(e ast.Expr) {
	for _, conv := range c.ifaces.byTuple[e] {
		c.conversionRule(e, conv)
	}
}

// typeAssert checks x.(T) and the comma-ok form.
func (c *checker) typeAssert(e *ast.TypeAssertExpr, tv types.TypeAndValue) {
	c.expr(e.X)
	c.typeRule(e.Type, c.info.Types[e.Type].Type)
	if tuple, ok := tv.Type.(*types.Tuple); ok {
		c.typeRule(e, tuple.At(1).Type())
		return
	}
	c.typeRule(e, tv.Type)
}

// typeSwitch checks a type switch: the guard, each case's types and bound
// variable, and the bodies; the default clause comes last, as emitted.
func (c *checker) typeSwitch(s *ast.TypeSwitchStmt) {
	if s.Init != nil {
		c.stmt(s.Init)
	}
	c.expr(typeSwitchGuard(s).X)
	var fallback *ast.CaseClause
	clause := func(clause *ast.CaseClause) {
		for _, e := range clause.List {
			if tv := c.info.Types[e]; !tv.IsNil() {
				c.typeRule(e, tv.Type)
			}
		}
		if obj := c.info.Implicits[clause]; obj != nil {
			c.variableRule(clause, obj.Type())
		}
		for _, statement := range clause.Body {
			c.stmt(statement)
		}
	}
	for _, statement := range s.Body.List {
		if cc := statement.(*ast.CaseClause); cc.List == nil {
			fallback = cc
		} else {
			clause(cc)
		}
	}
	if fallback != nil {
		clause(fallback)
	}
}

// typeSwitchGuard is the x.(type) expression of a type switch.
func typeSwitchGuard(s *ast.TypeSwitchStmt) *ast.TypeAssertExpr {
	var e ast.Expr
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		e = a.X
	case *ast.AssignStmt:
		e = a.Rhs[0]
	}
	return ast.Unparen(e).(*ast.TypeAssertExpr)
}

// Emitter.

// ifaceRuntime is emitted for programs with interface values: the two-word
// representation and the comparison helper defined after the prototypes.
const ifaceRuntime = `
/* Interface value: dynamic type ID (0: nil) and data word (the pointer
   itself for pointer types, else a heap copy of the value). */
typedef struct { u64 type; u8 *data; } gosvm_iface;
static boolean gosvm_iface_eq(gosvm_iface a, gosvm_iface b);
`

// convert evaluates e and converts its value to the interface type.
func (g *generator) convert(e ast.Expr, conv *Conversion) string {
	if conv.From == types.Typ[types.UntypedNil] {
		return g.temp(e, conv.To, "(gosvm_iface){0}")
	}
	return g.boxValue(e, conv, g.exprValue(e))
}

// boxValue converts a C value of conv.From to an interface value: pointers
// are stored directly, zero-size values point at the heap base without
// allocating, and every other value is copied to a new heap allocation.
func (g *generator) boxValue(n ast.Node, conv *Conversion, value string) string {
	d := g.ifaces.dynamicType(conv.From)
	if d == nil {
		g.unchecked(n)
	}
	data := ""
	switch {
	case d.Direct:
		data = "(u8 *)" + value
	case d.Size == 0:
		data = "GOSVM_HEAP_BASE"
	default:
		data = "(u8 *)" + g.box(n, conv.From, value)
	}
	return g.temp(n, conv.To, fmt.Sprintf("(gosvm_iface){%dULL,%s}", d.ID, data))
}

// convertAt converts element i of a multiple-value expression when Check
// recorded a conversion for it.
func (g *generator) convertAt(e ast.Expr, i int, value string) string {
	for _, conv := range g.ifaces.byTuple[e] {
		if conv.Index == i {
			return g.boxValue(e, conv, value)
		}
	}
	return value
}

// unbox is the C value of type t held by interface value x (whose type ID
// has been checked).
func (g *generator) unbox(n ast.Node, t types.Type, x string) string {
	d := g.ifaces.dynamicType(t)
	if d == nil {
		g.unchecked(n)
	}
	if d.Direct {
		return fmt.Sprintf("((%s)%s.data)", g.ctype(n, t), x)
	}
	return fmt.Sprintf("(*(%s *)%s.data)", g.ctype(n, t), x)
}

// typeTest is the C condition that interface value x holds a value of type
// t: a concrete type's ID, or an interface t implements.
func (g *generator) typeTest(n ast.Node, t types.Type, x string) string {
	if isIface(t) {
		if t.Underlying().(*types.Interface).Empty() {
			return fmt.Sprintf("(%s.type != 0)", x)
		}
		index := g.ifaces.assertedIndex(t)
		if index < 0 {
			g.unchecked(n)
		}
		return fmt.Sprintf("gosvm_impl%d(%s.type)", index, x)
	}
	d := g.ifaces.dynamicType(t)
	if d == nil {
		g.unchecked(n)
	}
	return fmt.Sprintf("(%s.type == %dULL)", x, d.ID)
}

// typeAssert lowers x.(T): a failed assertion aborts, as Go panics.
func (g *generator) typeAssert(e *ast.TypeAssertExpr) string {
	x := g.expr(e.X)
	t := g.info.Types[e.Type].Type
	g.line("if (!%s) abort();", g.typeTest(e, t, x))
	if isIface(t) {
		return g.temp(e, t, x)
	}
	return g.temp(e, t, g.unbox(e, t, x))
}

// typeAssertOK lowers v, ok := x.(T): v is T's zero value when ok is false.
func (g *generator) typeAssertOK(e *ast.TypeAssertExpr, tuple *types.Tuple) (string, string) {
	x := g.expr(e.X)
	t := g.info.Types[e.Type].Type
	ok := g.temp(e, tuple.At(1).Type(), g.typeTest(e, t, x))
	v := g.temp(e, t, "{0}")
	value := x
	if !isIface(t) {
		value = g.unbox(e, t, x)
	}
	g.line("if (%s) %s = %s;", ok, v, value)
	return v, ok
}

// typeSwitch lowers a type switch: the guard is evaluated once, cases are
// tested in source order and the default clause runs when none matches.
func (g *generator) typeSwitch(s *ast.TypeSwitchStmt) {
	g.line("{")
	if s.Init != nil {
		g.stmt(s.Init)
	}
	x := g.expr(typeSwitchGuard(s).X)
	g.line("do {")
	var fallback *ast.CaseClause
	for _, statement := range s.Body.List {
		clause := statement.(*ast.CaseClause)
		if clause.List == nil {
			fallback = clause
			continue
		}
		var tests []string
		for _, e := range clause.List {
			if tv := g.info.Types[e]; tv.IsNil() {
				tests = append(tests, fmt.Sprintf("(%s.type == 0)", x))
			} else {
				tests = append(tests, g.typeTest(e, tv.Type, x))
			}
		}
		g.next++
		matched := fmt.Sprintf("case%d", g.next)
		g.line("boolean %s=%s;", matched, strings.Join(tests, " || "))
		g.line("if (%s) {", matched)
		g.bindCase(clause, x)
		for _, statement := range clause.Body {
			g.stmt(statement)
		}
		g.line("break;\n}")
	}
	if fallback != nil {
		g.line("{")
		g.bindCase(fallback, x)
		for _, statement := range fallback.Body {
			g.stmt(statement)
		}
		g.line("}")
	}
	g.line("} while (0);\n}")
}

// bindCase declares a type switch clause's variable: the value of the
// clause's single concrete type, or the interface value itself.
func (g *generator) bindCase(clause *ast.CaseClause, x string) {
	obj := g.info.Implicits[clause]
	if obj == nil {
		return
	}
	value := x
	if len(clause.List) == 1 && !isIface(obj.Type()) {
		value = g.unbox(clause, obj.Type(), x)
	}
	if g.moved[obj] {
		g.declareMoved(clause, obj, value)
		return
	}
	g.line("%s %s = %s;", g.variable(clause, obj.Type()), g.name(obj), value)
}

// ifaceEqual is the C condition x == y where at least one operand is an
// interface value (or nil compared with one).
func (g *generator) ifaceEqual(n ast.Node, xe ast.Expr, x string, ye ast.Expr, y string) string {
	xt, yt := g.info.Types[xe], g.info.Types[ye]
	switch {
	case xt.IsNil():
		return fmt.Sprintf("(%s.type == 0)", y)
	case yt.IsNil():
		return fmt.Sprintf("(%s.type == 0)", x)
	case isIface(xt.Type) && isIface(yt.Type):
		return fmt.Sprintf("gosvm_iface_eq(%s,%s)", x, y)
	case isIface(xt.Type):
		return g.concreteEqual(n, x, yt.Type, y)
	}
	return g.concreteEqual(n, y, xt.Type, x)
}

// concreteEqual compares interface value x with a value v of concrete type
// t: equal when x holds a t equal to v. Nothing is allocated.
func (g *generator) concreteEqual(n ast.Node, x string, t types.Type, v string) string {
	d := g.ifaces.dynamicType(t)
	if d == nil {
		g.unchecked(n)
	}
	switch {
	case d.Direct:
		return fmt.Sprintf("(%s.type == %dULL && %s.data == (u8 *)%s)", x, d.ID, x, v)
	case d.Size == 0:
		return fmt.Sprintf("(%s.type == %dULL)", x, d.ID)
	}
	tmp := g.temp(n, t, v)
	return fmt.Sprintf("(%s.type == %dULL && gosvm_eq%d(%s.data,(const u8 *)&%s))", x, d.ID, d.ID, x, tmp)
}

// dynamicCall lowers x.M(args) on an interface value: the receiver, then the
// arguments, are evaluated, and the slot's dispatcher calls the dynamic
// type's method (a nil interface aborts).
func (g *generator) dynamicCall(call *ast.CallExpr, selector *ast.SelectorExpr) string {
	slot := g.ifaces.calls[call]
	if slot == nil {
		g.unchecked(call)
	}
	x := g.expr(selector.X)
	args := append([]string{x}, g.callValues(call)...)
	invocation := fmt.Sprintf("gosvm_call%d(%s)", slot.Index, strings.Join(args, ","))
	if slot.Signature.Results().Len() == 0 {
		g.line("%s;", invocation)
		return ""
	}
	return g.temp(call, g.info.Types[call].Type, invocation)
}

// valueEqual is the C equality of two values of comparable type t (C
// lvalues), field by field in order for structs, as Go compares them.
func (g *generator) valueEqual(n ast.Node, t types.Type, x, y string) string {
	if isIface(t) {
		return fmt.Sprintf("gosvm_iface_eq(%s,%s)", x, y)
	}
	if isString(t) {
		return fmt.Sprintf("gosvm_str_eq(%s,%s)", x, y)
	}
	switch u := t.Underlying().(type) {
	case *types.Array:
		return fmt.Sprintf("%s_equal(%s,%s)", g.arrayName(n, u), x, y)
	case *types.Struct:
		var parts []string
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			parts = append(parts, g.valueEqual(n, f.Type(), x+"."+g.name(f), y+"."+g.name(f)))
		}
		if len(parts) == 0 {
			return "1"
		}
		return "(" + strings.Join(parts, " && ") + ")"
	}
	return fmt.Sprintf("(%s == %s)", x, y)
}

// emitInterfaces defines the per-type equality functions, gosvm_iface_eq,
// the implements tests of asserted interfaces and one dispatcher per called
// method slot. Dispatchers and tests compare type IDs in ID order.
func (g *generator) emitInterfaces(program *CheckedProgram) {
	p := g.ifaces
	node := posNode(token.NoPos)
	for _, d := range p.dynamic {
		if d.Direct || d.Size == 0 || !d.Comparable {
			continue
		}
		ct := g.ctype(node, d.Type)
		g.line("static boolean gosvm_eq%d(const u8 *a,const u8 *b) { /* %s */", d.ID, d.Key)
		g.line("return %s;\n}", g.valueEqual(node, d.Type, "(*("+ct+" *)a)", "(*("+ct+" *)b)"))
	}
	g.line("static boolean gosvm_iface_eq(gosvm_iface a,gosvm_iface b) {")
	g.line("if (a.type != b.type) return 0;")
	g.line("if (a.type == 0) return 1;")
	for _, d := range p.dynamic {
		switch {
		case d.Direct && d.Comparable:
			g.line("if (a.type == %dULL) return a.data == b.data;", d.ID)
		case d.Size == 0:
			g.line("if (a.type == %dULL) return 1;", d.ID)
		case d.Comparable:
			g.line("if (a.type == %dULL) return gosvm_eq%d(a.data,b.data);", d.ID, d.ID)
		default:
			g.line("if (a.type == %dULL) abort(); /* uncomparable %s */", d.ID, d.Key)
		}
	}
	g.line("abort();\n}")
	for i, t := range p.asserted {
		var tests []string
		for _, d := range p.implementers(t) {
			tests = append(tests, fmt.Sprintf("t == %dULL", d.ID))
		}
		if len(tests) == 0 {
			tests = []string{"0"}
		}
		g.line("static boolean gosvm_impl%d(u64 t) { /* %s */", i, typeKey(t))
		g.line("return %s;\n}", strings.Join(tests, " || "))
	}
	for _, slot := range p.slots {
		if !slot.Called {
			continue
		}
		sig := slot.Signature
		ret := "void"
		switch sig.Results().Len() {
		case 0:
		case 1:
			ret = g.variable(node, sig.Results().At(0).Type())
		default:
			ret = g.resultName(node, sig.Results())
		}
		params := []string{"gosvm_iface x"}
		var names []string
		for i := 0; i < sig.Params().Len(); i++ {
			params = append(params, fmt.Sprintf("%s p%d", g.variable(node, sig.Params().At(i).Type()), i))
			names = append(names, fmt.Sprintf("p%d", i))
		}
		g.line("static %s gosvm_call%d(%s) { /* %s */", ret, slot.Index, strings.Join(params, ","), slot.Key)
		for _, d := range p.dynamic {
			for _, m := range d.Methods {
				if m.Slot != slot {
					continue
				}
				receiver := ""
				switch {
				case m.Deref:
					receiver = fmt.Sprintf("*(%s *)gosvm_nonnull(x.data)", g.ctype(node, d.Type.(*types.Pointer).Elem()))
				case d.Direct:
					receiver = fmt.Sprintf("(%s)x.data", g.ctype(node, d.Type))
				default:
					receiver = fmt.Sprintf("*(%s *)x.data", g.ctype(node, d.Type))
				}
				invocation := g.name(m.Func) + "(" + strings.Join(append([]string{receiver}, names...), ",") + ")"
				if ret == "void" {
					g.line("if (x.type == %dULL) { %s; return; }", d.ID, invocation)
				} else {
					g.line("if (x.type == %dULL) return %s;", d.ID, invocation)
				}
			}
		}
		g.line("abort();\n}")
	}
}
