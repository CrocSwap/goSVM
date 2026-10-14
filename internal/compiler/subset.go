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
}

func (c *checker) program(files []*ast.File, decls []ast.Decl) {
	// Array value types written anywhere in source are declared first.
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if n, ok := n.(*ast.ArrayType); ok && n.Len != nil {
				if tv, ok := c.info.Types[n]; ok {
					if a, ok := tv.Type.Underlying().(*types.Array); ok {
						c.declareArray(n, a)
					}
				}
			}
			return true
		})
	}
	for _, d := range decls {
		if gen, ok := d.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			for _, sp := range gen.Specs {
				ts := sp.(*ast.TypeSpec)
				if ts.Assign.IsValid() {
					c.fail(ts, "type aliases unsupported")
				}
				c.namedType(ts, c.info.Defs[ts.Name].Type())
			}
		}
	}
	for _, d := range decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			c.declareResults(fn, c.info.Defs[fn.Name].Type().(*types.Signature).Results())
		}
	}
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			c.signature(d)
		case *ast.GenDecl:
			if d.Tok == token.IMPORT || d.Tok == token.TYPE {
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
				c.block(fn.Body)
			}
		}
	}
}

func (c *checker) declareArray(node ast.Node, a *types.Array) {
	if c.declaredArray(a) {
		return
	}
	c.arrayRule(node, a)
	c.arrays = append(c.arrays, arrayType{typ: a})
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

// namedType admits named scalars, scalar arrays and value structs whose
// fields are themselves admitted.
func (c *checker) namedType(node ast.Node, t types.Type) {
	named, ok := t.(*types.Named)
	if !ok {
		c.fail(node, "only named scalar and value struct types supported")
	}
	if named.TypeParams().Len() != 0 {
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
		} else if _, ok := f.Type().Underlying().(*types.Struct); ok {
			c.namedType(node, f.Type())
		} else {
			if _, ok := f.Type().Underlying().(*types.Basic); !ok {
				c.fail(node, "struct fields must be unsigned scalars, bools, scalar arrays, or value structs")
			}
			c.variableRule(node, f.Type())
		}
	}
	for i := 0; i < st.NumFields(); i++ {
		c.variableRule(node, st.Field(i).Type())
	}
}

func (c *checker) signature(fn *ast.FuncDecl) {
	if fn.Name.Name == "init" {
		c.fail(fn, "init functions are unsupported")
	}
	if fn.Type.TypeParams != nil || fn.Body == nil {
		c.fail(fn, "generics and bodyless functions are unsupported")
	}
	sig := c.info.Defs[fn.Name].Type().(*types.Signature)
	if sig.Variadic() {
		c.fail(fn, "variadics are unsupported")
	}
	for i := 0; i < sig.Results().Len(); i++ {
		if sig.Results().At(i).Name() != "" {
			c.fail(fn, "named results are unsupported")
		}
	}
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
	case *ast.SwitchStmt:
		c.switchStatement(s)
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
		if _, ok := c.info.Types[s.Tag].Type.Underlying().(*types.Struct); ok {
			c.fail(s.Tag, "struct switch comparisons unsupported; compare fields explicitly")
		}
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
		} else {
			count++
		}
	}
	return count
}

func (c *checker) callValues(call *ast.CallExpr) int {
	var expected []types.Type
	if sig, ok := c.info.Types[call.Fun].Type.(*types.Signature); ok {
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

func (c *checker) expr(e ast.Expr) {
	tv := c.info.Types[e]
	if tv.IsNil() {
		return
	}
	if tv.Value != nil {
		c.typeRule(e, tv.Type)
		c.constantRule(e, tv.Value)
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
		if _, ok := tv.Type.Underlying().(*types.Struct); !ok {
			c.fail(e, "only value struct literals supported")
		}
		c.typeRule(e, tv.Type)
		for _, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			c.expr(elt)
		}
	case *ast.SelectorExpr:
		if sel := c.info.Selections[e]; sel != nil && sel.Kind() != types.FieldVal {
			c.fail(e, "method values unsupported; call the method directly")
		}
		if isContext(c.info.Types[e.X].Type) {
			c.fail(e, "Context fields are opaque")
		}
		if field, ok := c.info.Uses[e.Sel].(*types.Var); !ok || !field.IsField() {
			c.fail(e, "only value struct fields supported")
		}
		c.expr(e.X)
		if p, ok := pointer(c.info.Types[e.X].Type); ok {
			c.checkedPointer(e, p)
		}
		c.typeRule(e, tv.Type)
	case *ast.IndexExpr:
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
		case token.NOT, token.ADD, token.XOR:
		case token.SUB:
			if b, ok := tv.Type.Underlying().(*types.Basic); !ok || b.Info()&types.IsUnsigned == 0 {
				c.fail(e, "signed negation unsupported")
			}
		default:
			c.fail(e, "unsupported unary operator")
		}
		c.typeRule(e, tv.Type)
	case *ast.CallExpr:
		c.call(e, tv)
	default:
		c.fail(e, "unsupported expression %T", e)
	}
}

func (c *checker) binary(e *ast.BinaryExpr, tv types.TypeAndValue) {
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
		c.fail(e, "struct comparisons unsupported; compare fields explicitly")
	}
	c.expr(e.X)
	if e.Op == token.LAND || e.Op == token.LOR {
		c.typeRule(e, tv.Type)
		c.expr(e.Y)
		return
	}
	c.expr(e.Y)
	if e.Op == token.SHL || e.Op == token.SHR {
		// Shifts are defined for the unsigned widths only.
		c.typeRule(e, tv.Type)
		b, ok := tv.Type.Underlying().(*types.Basic)
		if !ok || (b.Kind() != types.Uint8 && b.Kind() != types.Uint32 && b.Kind() != types.Uint64 && b.Kind() != types.UntypedInt) {
			c.fail(e, "signed shifts unsupported")
		}
		c.typeRule(e, tv.Type)
		return
	}
	switch e.Op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM, token.AND, token.OR, token.XOR, token.AND_NOT:
		if b, ok := tv.Type.Underlying().(*types.Basic); !ok || b.Info()&types.IsUnsigned == 0 {
			c.fail(e, "signed arithmetic unsupported")
		}
	}
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
		if s, ok := tv.Type.(*types.Slice); ok && types.Identical(s.Elem(), types.Typ[types.Uint8]) {
			if len(e.Args) != 1 {
				c.fail(e, "invalid slice conversion")
			}
			if !c.info.Types[e.Args[0]].IsNil() {
				if !types.Identical(c.info.Types[e.Args[0]].Type, tv.Type) {
					c.fail(e, "slice conversion requires identical byte slices or nil")
				}
				c.expr(e.Args[0])
			}
			c.typeRule(e, tv.Type)
			return
		}
		if a, ok := tv.Type.Underlying().(*types.Array); ok {
			if len(e.Args) != 1 || !types.Identical(c.info.Types[e.Args[0]].Type.Underlying(), a) {
				c.fail(e, "array conversions require identical underlying arrays; slice conversions unsupported")
			}
			c.expr(e.Args[0])
			c.typeRule(e, tv.Type)
			return
		}
	}
	var o types.Object
	builtinName := ""
	switch callee := e.Fun.(type) {
	case *ast.SelectorExpr:
		id, ok := callee.X.(*ast.Ident)
		if !ok {
			c.fail(e, "only direct package function calls supported")
		}
		imported, ok := c.info.Uses[id].(*types.PkgName)
		if !ok {
			c.fail(e, "methods and function values unsupported")
		}
		if imported.Imported().Path() == "gosvm/solana" {
			c.sdkCall(e, callee)
			return
		}
		o = c.info.Uses[callee.Sel]
	case *ast.Ident:
		o = c.info.Uses[callee]
		builtinName = callee.Name
	default:
		c.fail(e, "only direct function calls supported")
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
			c.fail(e, "only len and cap builtins supported")
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
	sig := selection.Obj().(*types.Func).Type().(*types.Signature)
	receiver := sig.Recv().Type()
	if selection.Kind() == types.MethodExpr {
		c.callValues(call)
		actual := selection.Type().(*types.Signature).Params().At(0).Type()
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

// sliceView admits byte views of byte slices and byte arrays; they retain
// the underlying storage and never copy or allocate.
func (c *checker) sliceView(e *ast.SliceExpr) {
	if s, ok := c.info.Types[e].Type.(*types.Slice); !ok || !types.Identical(s.Elem(), types.Typ[types.Uint8]) {
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
		c.expr(value)
	}
}
