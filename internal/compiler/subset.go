package compiler

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"gosvm/internal/library"
)

// checker enforces the declaration, type, statement and expression rules of
// the on-chain Go subset. It is the only place those rules are applied.
//
// It visits declarations and evaluates operands in the order the C emitter
// lowers them, so that the first violation reported is the same one users
// have always seen. Every place the emitter needs a C type for a value is a
// typeRule (or, for storage, variableRule) application here.
type checker struct {
	*frontend
	externals map[string]library.Binding
	emitted   map[types.Type]bool // named types already checked
	current   *types.Tuple        // results of the function being checked
	loops     int                 // enclosing for statements

	gp            *genericProgram // instances, for diagnostics within them
	typeInstances []*TypeInstance // instantiated generic types, checked after named types
	files         []*ast.File     // the lowered files (program)
}

// within runs check for one declaration; a diagnostic inside an instance of
// a generic declaration names the instantiation.
func (c *checker) within(d ast.Node, check func()) {
	var inst *Instance
	if fn, ok := d.(*ast.FuncDecl); ok && c.gp != nil {
		inst = c.gp.instanceOf[fn]
	}
	if inst == nil {
		check()
		return
	}
	c.gp.wrap(inst, check)
}

func (c *checker) program(files []*ast.File, decls []ast.Decl) {
	c.files = files
	// Array value types written anywhere in source are declared first.
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if d, ok := n.(*ast.FuncDecl); ok && c.gp != nil && c.gp.instanceOf[d] != nil {
				c.within(d, func() { ast.Inspect(d, c.declareArrays) })
				return false
			}
			return c.declareArrays(n)
		})
	}
	// Dispatchers pass and return the arrays of called method slots, whose
	// interface declarations are not part of the lowered files.
	for _, a := range slotArrays(c.ifaces.slots) {
		c.declareArray(posNode(a.site), a.typ)
	}
	for _, d := range decls {
		if gen, ok := d.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			for _, sp := range gen.Specs {
				ts := sp.(*ast.TypeSpec)
				if ts.Assign.IsValid() {
					c.alias(ts)
					continue
				}
				c.namedType(ts, c.info.Defs[ts.Name].Type())
			}
		}
	}
	for _, ti := range c.typeInstances {
		ti := ti
		c.gp.wrapType(ti, func() { c.namedType(posNode(ti.Site), ti.Type) })
	}
	for _, d := range decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			c.within(fn, func() { c.declareResults(fn, c.info.Defs[fn.Name].Type().(*types.Signature).Results()) })
		}
	}
	for _, call := range bitsResults(c.info, files) {
		c.declareResults(call, c.info.Types[call].Type.(*types.Tuple))
	}
	// Dynamic calls return the results of their method slot.
	for _, slot := range c.ifaces.slots {
		if slot.Called {
			c.declareResults(posNode(slot.Site), slot.Signature.Results())
		}
	}
	// Literals and calls through function values return their signatures'
	// results.
	c.declareFuncResults()
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			c.within(d, func() { c.signature(d) })
		case *ast.GenDecl:
			if d.Tok == token.IMPORT || d.Tok == token.TYPE {
				continue
			}
			if d.Tok == token.VAR {
				// Package-level variables (docs/GO_GLOBALS.md).
				for _, spec := range d.Specs {
					c.globalSpec(spec.(*ast.ValueSpec))
				}
				continue
			}
			if d.Tok != token.CONST {
				c.fail(d, "only functions and constants are supported at package scope")
			}
		default:
			c.fail(d, "unsupported declaration")
		}
	}
	for _, d := range decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			c.current = c.info.Defs[fn.Name].Type().(*types.Signature).Results()
			if _, ok := externalBinding(c.externals, c.info.Defs[fn.Name]); ok {
				// A library stub's body is replaced by the native call.
				c.lookupResult(fn, c.current)
			} else {
				c.within(fn, func() { c.block(fn.Body) })
			}
		}
	}
}

// declareArrays declares the array value type written at n, if any; it is
// an ast.Inspect visitor.
func (c *checker) declareArrays(n ast.Node) bool {
	if n, ok := n.(*ast.ArrayType); ok && n.Len != nil {
		if tv, ok := c.info.Types[n]; ok {
			if a, ok := tv.Type.Underlying().(*types.Array); ok {
				c.declareArray(n, a)
			}
		}
	}
	return true
}

func (c *checker) declareArray(node ast.Node, a *types.Array) {
	if c.declaredArray(a) {
		return
	}
	c.arrayRuleExcept(node, a, c.inPlaceTable)
	// An array element, or the array a pointer element points to, is
	// declared before the array that holds it (the emitter's order).
	if inner, ok := elementArray(a.Elem()); ok {
		c.declareArray(node, inner)
	}
	c.arrays = append(c.arrays, arrayType{typ: a})
}

// arrayElement reports whether e is an addressable struct element of an
// array (or of an array through a pointer): a[i], whose fields are read in
// place.
func arrayElement(info *types.Info, e ast.Expr) bool {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	if !ok || !info.Types[e].Addressable() {
		return false
	}
	if _, ok := info.Types[e].Type.Underlying().(*types.Struct); !ok {
		return false
	}
	_, ok = arrayValue(info.Types[ix.X].Type)
	return ok
}

// elementArray is the array an array element of type t needs declared
// first: t's own array type, or the array a pointer element points to.
func elementArray(t types.Type) (*types.Array, bool) {
	if p, ok := pointer(t); ok {
		t = p.Elem()
	}
	a, ok := t.Underlying().(*types.Array)
	return a, ok
}

func (c *checker) declareResults(node ast.Node, t *types.Tuple) {
	if t.Len() < 2 || c.declaredResult(t) {
		return
	}
	for i := 0; i < t.Len(); i++ {
		c.variableRule(node, t.At(i).Type())
	}
	c.results = append(c.results, resultType{typ: t})
}

// alias admits a type alias declaration, type A = T (docs/GO_TYPES.md). An
// alias declares no new type: go/types resolves every use of A to T, so T's
// own rules apply wherever it is used. A named T (including an instantiated
// generic type) is checked at its declaration or instantiation; any other T
// must be a storable type. go/types materializes aliases as *types.Alias
// only under GODEBUG=gotypesalias=1, which the compiler does not support.
func (c *checker) alias(ts *ast.TypeSpec) {
	t := c.info.Defs[ts.Name].Type()
	if _, ok := t.(*types.Alias); ok {
		c.fail(ts, "type aliases require go/types' default alias resolution (GODEBUG gotypesalias=0)")
	}
	if _, ok := t.(*types.Named); ok {
		return
	}
	c.variableRule(ts, t)
}

// namedType admits named scalars, scalar arrays and value structs whose
// fields are themselves admitted.
func (c *checker) namedType(node ast.Node, t types.Type) {
	named, ok := t.(*types.Named)
	if !ok {
		c.fail(node, "only named scalar and value struct types supported")
	}
	if named.TypeParams().Len() != 0 && named.TypeArgs().Len() == 0 {
		c.fail(node, "generic types unsupported")
	}
	if c.emitted[t] {
		return
	}
	c.emitted[t] = true
	if a, ok := t.Underlying().(*types.Array); ok {
		c.declareArray(node, a)
		return
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		c.variableRule(node, t)
		return
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if f.Embedded() || isContext(f.Type()) {
			c.fail(node, "embedded and Context fields unsupported")
		}
		if a, ok := f.Type().Underlying().(*types.Array); ok {
			c.declareArray(node, a)
		} else if _, ok := f.Type().Underlying().(*types.Struct); ok && !isEmptyStruct(f.Type()) {
			c.namedType(node, f.Type())
		} else if memoryValue(f.Type()) || isIface(f.Type()) || isFuncValue(f.Type()) || isMap(f.Type()) || isEmptyStruct(f.Type()) {
			// Pointer and slice fields hold only heap or SDK storage; the
			// borrow analysis moves any local stored into one to the heap.
			// Interface fields hold such storage or heap copies, function
			// values refer only to heap environments, and map fields refer
			// to heap tables.
			c.variableRule(node, f.Type())
		} else {
			if _, ok := f.Type().Underlying().(*types.Basic); !ok {
				c.fail(node, "struct fields must be integer scalars, bools, scalar arrays, value structs, pointers, slices or interfaces")
			}
			c.variableRule(node, f.Type())
		}
	}
	for i := 0; i < st.NumFields(); i++ {
		c.variableRule(node, st.Field(i).Type())
	}
}

func (c *checker) signature(fn *ast.FuncDecl) {
	if fn.Type.TypeParams != nil || fn.Body == nil {
		c.fail(fn, "generics and bodyless functions are unsupported")
	}
	sig := c.info.Defs[fn.Name].Type().(*types.Signature)
	if sig.Results().Len() > 1 {
		c.lookupResult(fn, sig.Results())
	}
	if sig.Results().Len() == 1 {
		c.variableRule(fn, sig.Results().At(0).Type())
	}
	if recv := sig.Recv(); recv != nil {
		c.variableRule(fn, recv.Type())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		c.variableRule(fn, sig.Params().At(i).Type())
	}
}

func (c *checker) block(b *ast.BlockStmt) {
	for _, s := range b.List {
		c.stmt(s)
	}
}

func (c *checker) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.BlockStmt:
		c.block(s)
	case *ast.ReturnStmt:
		if c.current != nil && c.current.Len() > 1 {
			expected := make([]types.Type, c.current.Len())
			for i := range expected {
				expected[i] = c.current.At(i).Type()
			}
			c.valuesAs(s.Results, expected)
			c.lookupResult(s, c.current)
			return
		}
		if len(s.Results) != 0 {
			c.exprAs(s.Results[0], c.current.At(0).Type())
		}
	case *ast.ExprStmt:
		c.expr(s.X)
	case *ast.AssignStmt:
		if s.Tok == token.ADD_ASSIGN && len(s.Lhs) == 1 && isString(c.info.Types[s.Lhs[0]].Type) {
			c.stringAddAssign(s)
			return
		}
		if _, ok := compoundOps[s.Tok]; ok && len(s.Lhs) == 1 && isInteger(c.info.Types[s.Lhs[0]].Type) {
			c.compoundAssign(s)
			return
		}
		if s.Tok != token.DEFINE && s.Tok != token.ASSIGN {
			c.fail(s, "compound assignment unsupported")
		}
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
			c.assignMany(s, s.Lhs, s.Rhs, s.Tok)
			return
		}
		switch lhs := s.Lhs[0].(type) {
		case *ast.Ident:
			if lhs.Name == "_" {
				c.expr(s.Rhs[0])
				return
			}
			o := c.info.Uses[lhs]
			if s.Tok == token.DEFINE {
				o = c.info.Defs[lhs]
			}
			c.exprAs(s.Rhs[0], o.Type())
			if s.Tok == token.DEFINE {
				if _, ok := o.Type().Underlying().(*types.Array); ok {
					return
				}
				c.variableRule(lhs, o.Type())
			}
		case *ast.StarExpr, *ast.SelectorExpr:
			c.fieldTarget(lhs)
			c.expr(s.Rhs[0])
		case *ast.IndexExpr:
			if _, ok := pointer(c.info.Types[lhs.X].Type); ok {
				c.captureTarget(lhs)
				c.expr(s.Rhs[0])
				return
			}
			if a, ok := c.info.Types[lhs.X].Type.Underlying().(*types.Array); ok {
				c.fieldTarget(lhs.X)
				c.expr(lhs.Index)
				c.expr(s.Rhs[0])
				c.lookupArray(lhs, a)
				return
			}
			c.expr(lhs.X)
			c.expr(lhs.Index)
			c.expr(s.Rhs[0])
		default:
			c.fail(s, "unsupported assignment target")
		}
	case *ast.DeclStmt:
		d := s.Decl.(*ast.GenDecl)
		if d.Tok != token.VAR && d.Tok != token.CONST {
			c.fail(s, "unsupported local declaration")
		}
		if d.Tok == token.CONST {
			return
		}
		for _, sp := range d.Specs {
			v := sp.(*ast.ValueSpec)
			if len(v.Names) != 1 || len(v.Values) > 1 {
				c.multiDeclaration(v)
				continue
			}
			o := c.info.Defs[v.Names[0]]
			if isContext(o.Type()) && len(v.Values) == 0 {
				c.fail(v, "Context must come from the entrypoint, not a zero value")
			}
			if len(v.Values) == 1 {
				c.exprAs(v.Values[0], o.Type())
				if _, ok := o.Type().Underlying().(*types.Array); ok {
					continue
				}
			}
			c.variableRule(v, o.Type())
		}
	case *ast.IfStmt:
		if s.Init != nil {
			c.stmt(s.Init)
		}
		c.expr(s.Cond)
		c.block(s.Body)
		if s.Else != nil {
			c.stmt(s.Else)
		}
	case *ast.ForStmt:
		c.loops++
		if s.Init != nil {
			c.stmt(s.Init)
		}
		if s.Cond != nil {
			c.expr(s.Cond)
		}
		c.block(s.Body)
		if s.Post != nil {
			c.stmt(s.Post)
		}
		c.loops--
	case *ast.RangeStmt:
		c.rangeStatement(s)
	case *ast.SwitchStmt:
		c.switchStatement(s)
	case *ast.TypeSwitchStmt:
		c.typeSwitch(s)
	case *ast.IncDecStmt:
		id, ok := s.X.(*ast.Ident)
		if !ok {
			c.variableRule(s, c.info.Types[s.X].Type)
			c.captureTarget(s.X)
			return
		}
		c.variableRule(s, c.info.Uses[id].Type())
	case *ast.BranchStmt:
		if s.Label != nil {
			c.fail(s, "labeled branches unsupported")
		}
		switch s.Tok {
		case token.BREAK:
		case token.CONTINUE:
			if c.loops == 0 {
				c.fail(s, "continue outside a loop")
			}
		default:
			c.fail(s, "fallthrough and goto unsupported")
		}
	case *ast.DeferStmt:
		c.deferStmt(s)
	case *ast.EmptyStmt:
	default:
		c.fail(s, "unsupported statement %T", s)
	}
}

// Cases are tested in source order; the default clause body comes last.
func (c *checker) switchStatement(s *ast.SwitchStmt) {
	if s.Init != nil {
		c.stmt(s.Init)
	}
	if s.Tag != nil {
		// A struct tag compares like == (docs/GO_TYPES.md#struct-equality).
		c.expr(s.Tag)
	}
	var fallback *ast.CaseClause
	for _, statement := range s.Body.List {
		clause := statement.(*ast.CaseClause)
		if clause.List == nil {
			fallback = clause
			continue
		}
		for _, test := range clause.List {
			c.expr(test)
			if s.Tag != nil {
				if a, ok := c.info.Types[s.Tag].Type.Underlying().(*types.Array); ok {
					c.lookupArray(s.Tag, a)
				}
			}
		}
		for _, statement := range clause.Body {
			c.stmt(statement)
		}
	}
	if fallback != nil {
		for _, statement := range fallback.Body {
			c.stmt(statement)
		}
	}
}

// rangeStatement admits Go 1.22 range loops over integers (of any admitted
// type, or untyped constants), fixed scalar arrays, pointers to them, slices
// and strings. Iteration variables are fresh in every iteration.
func (c *checker) rangeStatement(s *ast.RangeStmt) {
	xt := c.info.Types[s.X].Type
	switch u := xt.Underlying().(type) {
	case *types.Basic:
		// Strings range over their UTF-8 encoded runes.
		if u.Info()&(types.IsInteger|types.IsString) == 0 {
			c.fail(s.X, "range requires an integer, a scalar array, a pointer to one, or a slice")
		}
	case *types.Map:
		c.fail(s.X, rangeMapRule)
	case *types.Chan:
		c.fail(s.X, "range over channels unsupported")
	case *types.Signature:
		c.fail(s.X, "range over functions unsupported")
	case *types.Slice:
	case *types.Array:
	case *types.Pointer:
		if _, ok := u.Elem().Underlying().(*types.Array); !ok {
			c.fail(s.X, "range requires an integer, a scalar array, a pointer to one, or a slice")
		}
	default:
		c.fail(s.X, "range requires an integer, a scalar array, a pointer to one, or a slice")
	}
	if s.Tok == token.ASSIGN {
		for _, target := range []ast.Expr{s.Key, s.Value} {
			if _, ok := target.(*ast.Ident); target != nil && !ok {
				c.fail(target, "range assignment targets must be local variables")
			}
		}
	}
	a, isArray := arrayValue(xt)
	if isArray && rangeTarget(s.Value) == nil {
		// Without a value variable Go never loads the array: only the calls
		// on the operand path run. Conversions and literals would be loaded.
		c.rangeOperandPath(s.X)
	}
	c.expr(s.X)
	if p, ok := pointer(xt); ok {
		c.checkedPointer(s.X, p)
	}
	if isArray {
		c.lookupArray(s.X, a)
	}
	if s.Tok == token.DEFINE {
		for _, target := range []ast.Expr{s.Key, s.Value} {
			id := rangeTarget(target)
			if id == nil {
				continue
			}
			c.variableRule(id, c.info.Defs[id].Type())
		}
	}
	for _, target := range []ast.Expr{s.Key, s.Value} {
		if conv := c.ifaces.byExpr[target]; target != nil && conv != nil {
			c.conversionRule(target, conv)
		}
	}
	c.loops++
	c.block(s.Body)
	c.loops--
}

// rangeOperandPath admits the operand of an index-only array range: locals,
// fields, dereferences and calls.
func (c *checker) rangeOperandPath(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
	case *ast.ParenExpr:
		c.rangeOperandPath(e.X)
	case *ast.SelectorExpr:
		c.rangeOperandPath(e.X)
	case *ast.StarExpr:
		c.rangeOperandPath(e.X)
	case *ast.CallExpr:
		if c.info.Types[e.Fun].IsType() {
			c.fail(e, "index-only range over an array conversion unsupported; range over the operand")
		}
	case *ast.IndexExpr:
		// An element of an array of arrays: without calls, len is constant
		// and Go evaluates nothing (no bounds or nil check).
		if _, ok := arrayValue(c.info.Types[e.X].Type); !ok || containsCall(e) {
			c.fail(e, "index-only range over an array requires a variable, field, dereference or call")
		}
	default:
		c.fail(e, "index-only range over an array requires a variable, field, dereference or call")
	}
}

// containsCall reports whether e contains a call expression.
func containsCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.CallExpr); ok {
			found = true
		}
		return !found
	})
	return found
}

// rangeTarget is a range iteration variable, or nil when absent or blank.
func rangeTarget(e ast.Expr) *ast.Ident {
	if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
		return id
	}
	return nil
}

func (c *checker) assignMany(node ast.Node, lhs []ast.Expr, rhs []ast.Expr, tok token.Token) {
	defs := make([]types.Object, len(lhs))
	for i, left := range lhs {
		if id, ok := left.(*ast.Ident); ok && tok == token.DEFINE {
			defs[i] = c.info.Defs[id]
			if id.Name == "_" || defs[i] != nil {
				continue
			}
		}
		c.captureTarget(left)
	}
	expected := make([]types.Type, len(lhs))
	for i, left := range lhs {
		expected[i] = c.info.Types[left].Type
		if defs[i] != nil {
			expected[i] = defs[i].Type()
		} else if id, ok := left.(*ast.Ident); ok && c.info.Uses[id] != nil {
			expected[i] = c.info.Uses[id].Type()
		}
	}
	if c.valuesAs(rhs, expected) != len(lhs) {
		c.fail(node, "assignment value count mismatch")
	}
	for i := range lhs {
		if defs[i] == nil {
			continue
		}
		if id, ok := lhs[i].(*ast.Ident); ok && id.Name == "_" {
			continue
		}
		if _, ok := defs[i].Type().Underlying().(*types.Array); ok {
			continue
		}
		c.variableRule(lhs[i], defs[i].Type())
	}
}

func (c *checker) multiDeclaration(v *ast.ValueSpec) {
	if len(v.Values) > 0 {
		lhs := make([]ast.Expr, len(v.Names))
		for i, id := range v.Names {
			lhs[i] = id
		}
		c.assignMany(v, lhs, v.Values, token.DEFINE)
		return
	}
	for _, id := range v.Names {
		if id.Name == "_" {
			continue
		}
		o := c.info.Defs[id]
		if isContext(o.Type()) {
			c.fail(v, "Context must come from the entrypoint, not a zero value")
		}
		c.variableRule(v, o.Type())
	}
}

// valuesAs checks expressions left to right and returns the number of values
// they produce once multiple-result calls are expanded.
func (c *checker) valuesAs(expressions []ast.Expr, expected []types.Type) int {
	count := 0
	for i, expression := range expressions {
		var want types.Type
		if i < len(expected) {
			want = expected[i]
		}
		c.exprAs(expression, want)
		if t, ok := c.info.Types[expression].Type.(*types.Tuple); ok && t.Len() > 1 {
			count += t.Len()
			c.tupleConversions(expression)
		} else {
			count++
		}
	}
	return count
}

func (c *checker) callValues(call *ast.CallExpr) int {
	var expected []types.Type
	if sig, ok := c.info.Types[call.Fun].Type.Underlying().(*types.Signature); ok {
		for i := 0; i < sig.Params().Len(); i++ {
			expected = append(expected, sig.Params().At(i).Type())
		}
	}
	return c.valuesAs(call.Args, expected)
}

// exprAs checks e where a value of the expected type is required. A nil
// slice in that position needs no representation of its own.
func (c *checker) exprAs(e ast.Expr, expected types.Type) {
	if c.info.Types[e].IsNil() {
		if _, ok := expected.(*types.Slice); ok {
			return
		}
	}
	c.expr(e)
}

// checkedPointer is a nil-checked use of a pointer value of type p.
func (c *checker) checkedPointer(n ast.Node, p types.Type) { c.variableRule(n, p) }

// expr checks e; a value Check recorded as converted to an interface must
// also be admitted as an interface value.
func (c *checker) expr(e ast.Expr) {
	if conv := c.ifaces.byExpr[e]; conv != nil && !conv.rng {
		c.exprValue(e)
		c.conversionRule(e, conv)
		return
	}
	c.exprValue(e)
}

func (c *checker) exprValue(e ast.Expr) {
	tv := c.info.Types[e]
	if tv.IsNil() {
		return
	}
	if tv.Value != nil {
		c.typeRule(e, tv.Type)
		c.constantRule(e, tv.Type, tv.Value)
		return
	}
	if t := c.funcs.byExpr[e]; t != nil {
		c.funcValue(e, t)
		return
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		c.expr(e.X)
	case *ast.Ident:
		if _, ok := c.info.Uses[e].(*types.Var); !ok {
			c.fail(e, "unsupported identifier")
		}
		c.typeRule(e, tv.Type)
	case *ast.SliceExpr:
		if isString(c.info.Types[e.X].Type) {
			c.stringSlice(e)
			return
		}
		c.sliceView(e)
	case *ast.StarExpr:
		c.pointed(e.X)
	case *ast.CompositeLit:
		if isContext(tv.Type) {
			c.fail(e, "Context literals unsupported")
		}
		if a, ok := tv.Type.Underlying().(*types.Array); ok {
			c.arrayLiteral(e, a)
			return
		}
		if s, ok := tv.Type.(*types.Slice); ok {
			c.sliceLiteral(e, s)
			return
		}
		if isMap(tv.Type) {
			c.mapLiteral(e, tv.Type)
			return
		}
		literal := tv.Type
		if p, ok := pointer(literal); ok {
			// An elided &T{...} element of a slice literal: always heap.
			if _, ok := p.Elem().Underlying().(*types.Struct); !ok {
				c.fail(e, "elided &T{...} literals require a struct type; write &T{...}")
			}
			c.typeRule(e, literal)
			literal = p.Elem()
		}
		if _, ok := literal.Underlying().(*types.Struct); !ok {
			c.fail(e, "only value struct literals supported")
		}
		c.typeRule(e, literal)
		for _, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			c.expr(elt)
		}
	case *ast.SelectorExpr:
		if c.packageVar(e) != nil {
			c.typeRule(e, tv.Type) // pkg.V
			return
		}
		if sel := c.info.Selections[e]; sel != nil && sel.Kind() != types.FieldVal {
			if isIface(c.info.Types[e.X].Type) || isIface(sel.Recv()) {
				c.fail(e, ifaceMethodValueRule)
			}
			c.fail(e, "method values unsupported; call the method directly")
		}
		if path := c.intrinsicValue(e); path != "" {
			c.fail(e, intrinsicValueRule, path)
		}
		if isContext(c.info.Types[e.X].Type) {
			c.fail(e, "Context fields are opaque")
		}
		if field, ok := c.info.Uses[e.Sel].(*types.Var); !ok || !field.IsField() {
			c.fail(e, "only value struct fields supported")
		}
		if arrayElement(c.info, e.X) {
			// A field of an array element is read in place, without
			// copying the element.
			c.fieldTarget(e.X)
			c.typeRule(e, tv.Type)
			return
		}
		c.expr(e.X)
		if p, ok := pointer(c.info.Types[e.X].Type); ok {
			c.checkedPointer(e, p)
		}
		c.typeRule(e, tv.Type)
	case *ast.IndexExpr:
		if isMap(c.info.Types[e.X].Type) {
			c.mapIndex(e, tv)
			return
		}
		if _, ok := pointer(c.info.Types[e.X].Type); ok {
			if a, ok := arrayValue(c.info.Types[e.X].Type); ok {
				c.arrayPointer(e.X)
				c.expr(e.Index)
				c.lookupArray(e, a)
				c.typeRule(e, tv.Type)
				return
			}
		}
		if a, ok := c.info.Types[e.X].Type.Underlying().(*types.Array); ok {
			// Addressable array storage is indexed in place, without a copy.
			if c.info.Types[e.X].Addressable() {
				c.fieldTarget(e.X)
			} else {
				c.expr(e.X)
			}
			c.expr(e.Index)
			c.lookupArray(e, a)
			c.typeRule(e, tv.Type)
			return
		}
		c.expr(e.X)
		c.expr(e.Index)
		c.typeRule(e, tv.Type)
	case *ast.BinaryExpr:
		c.binary(e, tv)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			c.address(e.X)
			c.typeRule(e, tv.Type)
			return
		}
		c.expr(e.X)
		switch e.Op {
		case token.NOT, token.ADD, token.XOR, token.SUB:
		default:
			c.fail(e, "unsupported unary operator")
		}
		c.typeRule(e, tv.Type)
	case *ast.CallExpr:
		c.call(e, tv)
	case *ast.TypeAssertExpr:
		c.typeAssert(e, tv)
	default:
		c.fail(e, "unsupported expression %T", e)
	}
}

func (c *checker) binary(e *ast.BinaryExpr, tv types.TypeAndValue) {
	if isIface(c.info.Types[e.X].Type) || isIface(c.info.Types[e.Y].Type) {
		// == and != with an interface operand compare dynamic types and
		// values; nothing is converted or allocated.
		c.expr(e.X)
		c.expr(e.Y)
		c.typeRule(e, tv.Type)
		return
	}
	sliceType := c.info.Types[e.X].Type
	if _, ok := sliceType.(*types.Slice); !ok {
		sliceType = c.info.Types[e.Y].Type
	}
	if _, ok := sliceType.(*types.Slice); ok {
		if e.Op != token.EQL && e.Op != token.NEQ {
			c.fail(e, "only slice/nil comparison supported")
		}
		c.exprAs(e.X, sliceType)
		c.exprAs(e.Y, sliceType)
		c.typeRule(e, tv.Type)
		return
	}
	if a, ok := c.info.Types[e.X].Type.Underlying().(*types.Array); ok {
		c.expr(e.X)
		c.expr(e.Y)
		c.lookupArray(e, a)
		if e.Op != token.EQL && e.Op != token.NEQ {
			c.fail(e, "only == and != array comparisons supported")
		}
		c.typeRule(e, tv.Type)
		return
	}
	if _, ok := c.info.Types[e.X].Type.Underlying().(*types.Struct); ok {
		// == and != compare the non-blank fields in order, stopping at the
		// first difference (docs/GO_TYPES.md#struct-equality); go/types has
		// required a comparable struct type.
		c.expr(e.X)
		c.expr(e.Y)
		c.typeRule(e, tv.Type)
		return
	}
	c.expr(e.X)
	if e.Op == token.LAND || e.Op == token.LOR {
		c.typeRule(e, tv.Type)
		c.expr(e.Y)
		return
	}
	c.expr(e.Y)
	// Integer operators follow Go for every admitted width, signed or not:
	// wrapping arithmetic, truncating division, sign-filling right shifts and
	// signed or unsigned shift counts (a negative count traps).
	c.typeRule(e, tv.Type)
}

func (c *checker) call(e *ast.CallExpr, tv types.TypeAndValue) {
	if selector, ok := e.Fun.(*ast.SelectorExpr); ok {
		if selection := c.info.Selections[selector]; selection != nil && selection.Kind() != types.FieldVal {
			c.methodCall(e, selector, selection)
			return
		}
	}
	if c.info.Types[e.Fun].IsType() {
		if classifyConversion(c.info, e) != notStringConversion {
			c.stringConversionRule(e, tv)
			return
		}
		if _, _, ok := sliceToArray(c.info, e); ok {
			c.sliceToArrayRule(e, tv)
			return
		}
		if isIface(tv.Type) {
			// I(x): the argument is converted (or copied, when it is an
			// interface value already).
			c.expr(e.Args[0])
			c.typeRule(e, tv.Type)
			return
		}
		if isMap(tv.Type) {
			c.mapConversion(e, tv)
			return
		}
		if s, ok := tv.Type.(*types.Slice); ok {
			if len(e.Args) != 1 {
				c.fail(e, "invalid slice conversion")
			}
			if !c.info.Types[e.Args[0]].IsNil() {
				if !types.Identical(c.info.Types[e.Args[0]].Type, tv.Type) {
					if isByteSlice(s) {
						c.fail(e, "slice conversion requires identical byte slices or nil")
					}
					c.fail(e, "slice conversion requires identical slices or nil")
				}
				c.expr(e.Args[0])
			}
			c.typeRule(e, tv.Type)
			return
		}
		if a, ok := tv.Type.Underlying().(*types.Array); ok {
			if len(e.Args) != 1 || !types.Identical(c.info.Types[e.Args[0]].Type.Underlying(), a) {
				c.fail(e, "array conversions require identical underlying arrays")
			}
			c.expr(e.Args[0])
			c.typeRule(e, tv.Type)
			return
		}
		if isFuncValue(tv.Type) {
			// F(f) and (func())(nil): Go requires identical underlying
			// signatures; the value is copied.
			if len(e.Args) != 1 {
				c.fail(e, "invalid conversion")
			}
			c.expr(e.Args[0])
			c.typeRule(e, tv.Type)
			return
		}
		if _, ok := tv.Type.Underlying().(*types.Pointer); ok {
			// (*T)(p) and (*T)(nil): Go requires identical underlying base
			// types (ignoring struct tags); the address is unchanged
			// (docs/GO_POINTERS.md#conversions).
			if len(e.Args) != 1 {
				c.fail(e, "invalid conversion")
			}
			c.expr(e.Args[0])
			c.typeRule(e, tv.Type)
			return
		}
	}
	var o types.Object
	builtinName := ""
	switch callee := c.callFun(e.Fun).(type) {
	case *ast.SelectorExpr:
		id, ok := callee.X.(*ast.Ident)
		if !ok {
			c.indirectCall(e, tv) // a function-valued field
			return
		}
		imported, ok := c.info.Uses[id].(*types.PkgName)
		if !ok {
			c.indirectCall(e, tv)
			return
		}
		if imported.Imported().Path() == "gosvm/solana" {
			c.sdkCall(e, callee)
			return
		}
		if imported.Imported().Path() == bitsPath {
			c.bitsCall(e, callee)
			return
		}
		o = c.callee(callee.Sel)
		if _, ok := o.(*types.Var); ok {
			c.indirectCall(e, tv) // a function-valued package-level variable
			return
		}
	case *ast.Ident:
		o = c.callee(callee)
		builtinName = callee.Name
		if _, ok := o.(*types.Var); ok {
			c.indirectCall(e, tv) // a function-valued variable
			return
		}
	default:
		c.indirectCall(e, tv) // a literal, element, call result, ...
		return
	}
	if b, ok := o.(*types.Builtin); ok && allocBuiltins[b.Name()] {
		c.allocBuiltin(e, b.Name(), tv)
		return
	}
	if name, ok := c.mapBuiltinCall(e); ok {
		c.mapBuiltin(e, name, tv)
		return
	}
	if b, ok := o.(*types.Builtin); ok && (b.Name() == "panic" || b.Name() == "recover") {
		c.panicCall(e, b.Name())
		return
	}
	values := c.callValues(e)
	if _, ok := o.(*types.TypeName); ok {
		if _, ok := tv.Type.Underlying().(*types.Struct); ok {
			c.fail(e, "struct conversions unsupported; construct fields explicitly")
		}
		if values != 1 {
			c.fail(e, "invalid conversion")
		}
		c.variableRule(e, tv.Type)
		c.typeRule(e, tv.Type)
		return
	}
	if _, ok := o.(*types.Builtin); ok {
		if len(e.Args) == 1 {
			if _, ok := arrayValue(c.info.Types[e.Args[0]].Type); ok && (builtinName == "len" || builtinName == "cap") {
				c.typeRule(e, tv.Type)
				return
			}
		}
		if builtinName != "len" && builtinName != "cap" {
			c.fail(e, builtinRule)
		}
		c.typeRule(e, tv.Type)
		return
	}
	if _, ok := o.(*types.Func); !ok {
		c.fail(e, "function values unsupported")
	}
	if tup, ok := tv.Type.(*types.Tuple); ok && tup.Len() == 0 {
		return
	}
	c.typeRule(e, tv.Type)
}

func (c *checker) sdkCall(call *ast.CallExpr, selector *ast.SelectorExpr) {
	fn, ok := c.info.Uses[selector.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "gosvm/solana" {
		c.fail(call, "only gosvm/solana direct calls are supported")
	}
	c.callValues(call)
	c.typeRule(call, c.info.Types[call].Type)
}

// methodCall follows Go's receiver evaluation: a pointer receiver operand is
// saved before the arguments, and a value copy through a pointer is taken
// after them.
func (c *checker) methodCall(call *ast.CallExpr, selector *ast.SelectorExpr, selection *types.Selection) {
	if isIface(c.info.Types[selector.X].Type) {
		if selection.Kind() != types.MethodVal {
			c.fail(call, "method expressions of interface types unsupported")
		}
		// A dynamic call: the receiver, then the arguments.
		c.expr(selector.X)
		c.callValues(call)
		if tup, ok := c.info.Types[call].Type.(*types.Tuple); !ok || tup.Len() != 0 {
			c.typeRule(call, c.info.Types[call].Type)
		}
		return
	}
	sig := c.method(selector).Type().(*types.Signature)
	receiver := sig.Recv().Type()
	if selection.Kind() == types.MethodExpr {
		c.callValues(call)
		actual := c.info.Types[selector].Type.(*types.Signature).Params().At(0).Type()
		if p, ok := pointer(actual); ok {
			if _, wantPointer := pointer(receiver); !wantPointer {
				c.checkedPointer(call, p)
				c.typeRule(call, receiver)
			}
		}
	} else {
		actual := c.info.Types[selector.X].Type
		argumentsDone := false
		if _, wantPointer := pointer(receiver); wantPointer {
			if _, isPointer := pointer(actual); isPointer {
				c.expr(selector.X)
			} else {
				c.address(selector.X)
				c.typeRule(selector.X, receiver)
			}
		} else if p, isPointer := pointer(actual); isPointer {
			c.expr(selector.X)
			c.callValues(call)
			c.checkedPointer(selector.X, p)
			c.typeRule(selector.X, receiver)
			argumentsDone = true
		} else {
			c.expr(selector.X)
		}
		if !argumentsDone {
			c.callValues(call)
		}
	}
	if sig.Results().Len() != 0 {
		c.typeRule(call, c.info.Types[call].Type)
	}
}

func (c *checker) pointed(e ast.Expr) {
	p, ok := pointer(c.info.Types[e].Type)
	if !ok {
		c.fail(e, "dereference requires a supported pointer")
	}
	c.expr(e)
	c.checkedPointer(e, p)
	c.typeRule(e, p.Elem())
}

// address admits caller-owned locals, fields, elements and composite literals.
func (c *checker) address(e ast.Expr) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		c.address(e.X)
	case *ast.Ident, *ast.SelectorExpr:
		c.fieldTarget(e)
	case *ast.StarExpr:
		c.expr(e.X)
		c.checkedPointer(e, c.info.Types[e.X].Type)
	case *ast.CompositeLit:
		c.expr(e)
	case *ast.IndexExpr:
		if a, ok := arrayValue(c.info.Types[e.X].Type); ok {
			c.arrayPointer(e.X)
			c.expr(e.Index)
			c.lookupArray(e, a)
			return
		}
		c.expr(e.X)
		c.expr(e.Index)
	default:
		c.fail(e, "address requires caller-owned local, field or element storage")
	}
}

func (c *checker) arrayPointer(e ast.Expr) {
	if p, ok := pointer(c.info.Types[e].Type); ok {
		c.expr(e)
		c.checkedPointer(e, p)
		return
	}
	if c.info.Types[e].Addressable() {
		c.fieldTarget(e)
		return
	}
	c.expr(e)
}

// fieldTarget admits storage that keeps its identity: locals, fields of
// value structs and pointer indirections.
func (c *checker) fieldTarget(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
	case *ast.ParenExpr:
		c.fieldTarget(e.X)
	case *ast.StarExpr:
		c.expr(e.X)
		c.checkedPointer(e, c.info.Types[e.X].Type)
	case *ast.SelectorExpr:
		if c.packageVar(e) != nil {
			return // pkg.V
		}
		if isContext(c.info.Types[e.X].Type) {
			c.fail(e, "Context fields are opaque")
		}
		if f, ok := c.info.Uses[e.Sel].(*types.Var); !ok || !f.IsField() {
			c.fail(e, "unsupported field target")
		}
		if p, ok := pointer(c.info.Types[e.X].Type); ok {
			c.expr(e.X)
			c.checkedPointer(e, p)
			return
		}
		c.fieldTarget(e.X)
	case *ast.IndexExpr:
		// An element of addressable array storage (or of an array through a
		// pointer) keeps its identity, as in a[i].F = v and a[i][j] = v.
		if a, ok := arrayValue(c.info.Types[e.X].Type); ok {
			c.arrayPointer(e.X)
			c.expr(e.Index)
			c.lookupArray(e, a)
			return
		}
		// An element of a slice is addressable storage, as in s[i].F = v.
		if _, ok := c.info.Types[e.X].Type.(*types.Slice); !ok {
			c.fail(e, "field target must be a local value struct")
		}
		c.expr(e.X)
		c.expr(e.Index)
		c.typeRule(e, c.info.Types[e].Type)
	default:
		c.fail(e, "field target must be a local value struct")
	}
}

// captureTarget evaluates assignment operands before any right-hand side.
func (c *checker) captureTarget(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
	case *ast.SelectorExpr, *ast.StarExpr:
		c.fieldTarget(e)
	case *ast.IndexExpr:
		if _, ok := pointer(c.info.Types[e.X].Type); ok {
			if a, ok := arrayValue(c.info.Types[e.X].Type); ok {
				c.arrayPointer(e.X)
				c.expr(e.Index)
				c.lookupArray(e, a)
				return
			}
		}
		if a, ok := c.info.Types[e.X].Type.Underlying().(*types.Array); ok {
			c.fieldTarget(e.X)
			c.lookupArray(e, a)
			c.expr(e.Index)
			return
		}
		c.expr(e.X)
		c.expr(e.Index)
	default:
		c.fail(e, "unsupported assignment target")
	}
}

// sliceView admits views of slices and arrays; they retain the underlying
// storage and never copy or allocate.
func (c *checker) sliceView(e *ast.SliceExpr) {
	if _, ok := c.info.Types[e].Type.(*types.Slice); !ok {
		c.fail(e, "slice views require byte/uint8 elements")
	}
	if _, ok := arrayValue(c.info.Types[e.X].Type); ok {
		p := types.NewPointer(c.info.Types[e.X].Type)
		if actual, isPointer := pointer(c.info.Types[e.X].Type); isPointer {
			p = actual
			c.expr(e.X)
		} else {
			c.fieldTarget(e.X)
			c.typeRule(e.X, p)
		}
		c.checkedPointer(e, p)
	} else {
		c.expr(e.X)
	}
	for _, bound := range []ast.Expr{e.Low, e.High, e.Max} {
		if bound != nil {
			c.expr(bound)
		}
	}
	c.typeRule(e, c.info.Types[e].Type)
}

func (c *checker) arrayLiteral(e *ast.CompositeLit, a *types.Array) {
	c.typeRule(e, c.info.Types[e].Type)
	for _, elt := range e.Elts {
		value := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			index, valid := constant.Int64Val(c.info.Types[kv.Key].Value)
			if !valid || index < 0 || index >= a.Len() {
				c.fail(kv, "invalid array literal index")
			}
			value = kv.Value
		}
		c.exprAs(value, a.Elem())
	}
}
