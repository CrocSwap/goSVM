package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Aggregate parameters and the SBF backend (docs/GO_STDLIB.md#aggregate-parameters-on-sbf).
// Clang passes a C struct parameter (a string, slice, interface, struct or
// array value) byval: the IR promises the callee its own copy, so the caller
// passes the address of its own variable when it can (a package variable, a
// heap object's field, an address-taken local, an interface's box), and
// LLVM forwards copies into byval arguments. The SBF backend lowers byval as
// that plain pointer and makes no copy. On a host target the same C is
// correct, so only SBF executions differ, in two ways:
//
//   - a callee that writes its parameter (s = s[1:], p.f = x, a[i] = v, a
//     value receiver's field, &p) writes the caller's variable;
//   - a callee that changes the caller's variable by another path (a
//     package variable, a pointer) and then reads its parameter reads the
//     new value: LLVM assumes byval memory is private, so it reads the
//     parameter (or passes its address on) after the write.
//
// Every function and literal therefore receives such parameters under
// another name and copies them into a local at entry (the local is the Go
// variable), so its body never touches byval memory:
//
//   - a parameter it writes is copied plainly;
//   - a parameter it may read after a write that may change a variable of
//     the parameter's type held by a caller (readAfterWrite) is copied
//     through an opaque pointer, which LLVM cannot assume private: the
//     copy reads the parameter at entry and LLVM never forwards the
//     parameter's address past a write.
//
// A write may change the caller's variable unless it provably writes only
// this call's own locals, or only values of types the variable cannot hold
// (strings, slices, interfaces and maps are headers: writing their
// elements never changes the variable). Calls are summarized by what their
// callees write and through which parameters (funcWrites), so a method
// writing through its receiver, called on a local, writes only that local.
// Copies of parameters larger than byvalInPlaceBytes stay whole in memory:
// LLVM would otherwise split them into scalars and rebuild them at every
// whole use (a return of the parameter), which multiplies code size.
//
// Functions with neither keep their C. Parameters moved to the heap are
// copied by their own prologue already. The C runtime's helpers do not
// write their aggregate parameters.

// byvalInPlaceBytes is the size above which a copied parameter stays whole
// in memory.
const byvalInPlaceBytes = 128

// byvalAggregate reports whether values of t are C structs.
func byvalAggregate(t types.Type) bool {
	if isContext(t) {
		return false // a pointer in C
	}
	if isString(t) || isIface(t) || isEmptyStruct(t) {
		return true
	}
	switch t.Underlying().(type) {
	case *types.Slice, *types.Struct, *types.Array:
		return true
	}
	return false
}

// underlyingOf is e's underlying type, or nil (a package name has none).
func underlyingOf(info *types.Info, e ast.Expr) types.Type {
	if t := info.Types[e].Type; t != nil {
		return t.Underlying()
	}
	return nil
}

// byvalCandidates are the aggregate parameters among params.
func byvalCandidates(params []*types.Var) map[types.Object]bool {
	candidates := map[types.Object]bool{}
	for _, p := range params {
		if p != nil && p.Name() != "_" && byvalAggregate(p.Type()) {
			candidates[p] = true
		}
	}
	return candidates
}

// writtenAggregates lists the aggregate parameters among params that body
// may write: assigned (also as the root of a field or element target),
// incremented, ranged into, or addressed (explicitly, or by a method call
// with a pointer receiver).
func writtenAggregates(info *types.Info, body *ast.BlockStmt, params []*types.Var) []types.Object {
	if body == nil {
		return nil
	}
	candidates := byvalCandidates(params)
	if len(candidates) == 0 {
		return nil
	}
	written := map[types.Object]bool{}
	var root func(ast.Expr)
	root = func(e ast.Expr) {
		switch e := ast.Unparen(e).(type) {
		case *ast.Ident:
			if o := info.Uses[e]; candidates[o] {
				written[o] = true
			}
		case *ast.SelectorExpr:
			// A field of a struct value is part of it; a field reached
			// through a pointer is not.
			if _, ok := underlyingOf(info, e.X).(*types.Struct); ok {
				root(e.X)
			}
		case *ast.IndexExpr:
			// An element of an array value is part of it; slice, string
			// and map elements are not (the parameter is their header).
			if _, ok := underlyingOf(info, e.X).(*types.Array); ok {
				root(e.X)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				for _, lhs := range n.Lhs {
					root(lhs)
				}
			}
		case *ast.IncDecStmt:
			root(n.X)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				root(n.Key)
				if n.Value != nil {
					root(n.Value)
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				root(n.X)
			}
		case *ast.SelectorExpr:
			// x.M with a pointer receiver takes &x.
			if s := info.Selections[n]; s != nil && s.Kind() == types.MethodVal {
				if fn, ok := s.Obj().(*types.Func); ok {
					if _, ptr := fn.Type().(*types.Signature).Recv().Type().(*types.Pointer); ptr {
						if _, isPtr := s.Recv().(*types.Pointer); !isPtr {
							root(n.X)
						}
					}
				}
			}
		case *ast.SliceExpr:
			// Slicing an array parameter takes its address.
			if _, ok := underlyingOf(info, n.X).(*types.Array); ok {
				root(n.X)
			}
		}
		return true
	})
	var out []types.Object
	for _, p := range params {
		if p != nil && written[p] {
			out = append(out, p)
		}
	}
	return out
}

// readAfterWrite lists the aggregate parameters among params that body may
// read after one of the write sites (writeSites, function literals' bodies
// excluded) has run: a use that follows a site in the source, shares a loop
// with one, or is in a function literal (which may run after any).
// Only sites that may write a value of a type the parameter's caller
// variable can hold count (affects).
func readAfterWrite(info *types.Info, body *ast.BlockStmt, params []*types.Var, sites []ast.Node, affects func(site ast.Node, t types.Type) bool) []types.Object {
	if body == nil || len(sites) == 0 {
		return nil
	}
	candidates := byvalCandidates(params)
	if len(candidates) == 0 {
		return nil
	}
	var loops []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			loops = append(loops, n)
		}
		return true
	})
	within := func(outer, n ast.Node) bool { return outer.Pos() <= n.Pos() && n.End() <= outer.End() }
	after := func(use *ast.Ident, inLit bool) bool {
		for _, s := range sites {
			if !affects(s, info.Uses[use].Type()) {
				continue
			}
			if inLit || s.End() <= use.Pos() {
				return true
			}
			for _, l := range loops {
				if within(l, s) && within(l, use) {
					return true
				}
			}
		}
		return false
	}
	aliased := map[types.Object]bool{}
	var visit func(n ast.Node, inLit bool)
	visit = func(n ast.Node, inLit bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch m := m.(type) {
			case *ast.FuncLit:
				if !inLit {
					visit(m.Body, true)
					return false
				}
			case *ast.Ident:
				if o := info.Uses[m]; candidates[o] && !aliased[o] && after(m, inLit) {
					aliased[o] = true
				}
			}
			return true
		})
	}
	visit(body, false)
	var out []types.Object
	for _, p := range params {
		if p != nil && aliased[p] {
			out = append(out, p)
		}
	}
	return out
}

// copiedParameter is the C name a copied parameter arrives under.
func (g *generator) copiedParameter(o types.Object) string { return g.rawName(o) + "_in" }

// noteCopiedParams records, for a function or literal, the aggregate
// parameters it copies at entry (those not moved to the heap): the ones it
// writes, plainly, and the ones it may read after a write that may change
// their callers' variables (sites lists the body's write sites), opaquely.
func (g *generator) noteCopiedParams(node ast.Node, body *ast.BlockStmt, params []*types.Var, sites func() []ast.Node) {
	if len(byvalCandidates(params)) == 0 {
		return
	}
	copies := map[types.Object]bool{}
	for _, o := range writtenAggregates(g.info, body, params) {
		copies[o] = true
	}
	scope := newWriteScope(g.info, body, params)
	for _, o := range readAfterWrite(g.info, body, params, sites(), func(site ast.Node, t types.Type) bool {
		e := g.writeSummaries.siteWrites(site, scope)
		return !e.local && e.types.affects(t)
	}) {
		copies[o] = true
		g.opaqueCopies[o] = true
	}
	for _, p := range params {
		if p != nil && copies[p] && !g.moved[p] {
			g.copied[p] = true
			g.copiedParams[node] = append(g.copiedParams[node], p)
		}
	}
}

// copyPrologue copies a function's copied parameters into their locals.
func (g *generator) copyPrologue(node ast.Node) {
	for _, o := range g.copiedParams[node] {
		t, name, in := g.variable(node, o.Type()), g.rawName(o), g.copiedParameter(o)
		if g.opaqueCopies[o] {
			// The empty asm hides that the pointer is the byval
			// parameter's address, so LLVM must assume other writes may
			// change what it points to.
			g.line("%s %s; { const %s *gosvm_p = &%s; __asm__(\"\" : \"+r\"(gosvm_p)); %s = *gosvm_p; }", t, name, t, in, name)
		} else {
			g.line("%s %s = %s;", t, name, in)
		}
		if gcSizes.Sizeof(o.Type()) > byvalInPlaceBytes {
			// The asm takes the local's address, so LLVM keeps it whole.
			g.line("__asm__(\"\" : : \"r\"(&%s));", name)
		}
	}
}

// writtenTypes is what a write site or function may write: values of the
// listed types, or anything (all).
type writtenTypes struct {
	all   bool
	types []types.Type
}

func (w *writtenTypes) add(t types.Type) {
	for _, u := range w.types {
		if types.Identical(u, t) {
			return
		}
	}
	w.types = append(w.types, t)
}

func (w *writtenTypes) merge(v *writtenTypes) {
	w.all = w.all || v.all
	for _, t := range v.types {
		w.add(t)
	}
}

// affects reports whether writing values of these types may change a
// variable of type t: a written type is t, a component of t (a field or
// array element, at any depth) or has t as a component. Strings, slices,
// interfaces and maps are atomic: what they refer to is not part of the
// variable.
func (w *writtenTypes) affects(t types.Type) bool {
	if w.all {
		return true
	}
	for _, u := range w.types {
		if typeContains(t, u) || typeContains(u, t) {
			return true
		}
	}
	return false
}

// typeContains reports whether t is u or has u as a component.
func typeContains(t, u types.Type) bool {
	if types.Identical(t, u) {
		return true
	}
	switch x := t.Underlying().(type) {
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			if typeContains(x.Field(i).Type(), u) {
				return true
			}
		}
	case *types.Array:
		return typeContains(x.Elem(), u)
	}
	return false
}

// pointerFree reports a type whose values refer to no mutable storage:
// booleans, numbers, strings (immutable), the SDK Context, and arrays and
// structs of them.
func pointerFree(t types.Type) bool {
	if isContext(t) {
		return true
	}
	switch x := t.Underlying().(type) {
	case *types.Basic:
		return true
	case *types.Array:
		return pointerFree(x.Elem())
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			if !pointerFree(x.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// rootVar is the variable at the root of a storage expression (through
// parentheses, fields, indexing, slicing, dereferences and &), or nil.
func rootVar(info *types.Info, e ast.Expr) *types.Var {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			if info.Selections[x] == nil {
				v, _ := info.Uses[x.Sel].(*types.Var) // a package-qualified variable
				return v
			}
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.UnaryExpr:
			if x.Op != token.AND {
				return nil
			}
			e = x.X
		case *ast.Ident:
			v, _ := info.Uses[x].(*types.Var)
			return v
		default:
			return nil
		}
	}
}

// writeScope describes a function body for confining writes: its
// parameters (receiver included) that the body never reassigns, and the
// extent of its body (its locals, fresh in every call, are declared there).
type writeScope struct {
	params map[types.Object]bool
	body   ast.Node
}

// newWriteScope is the scope of a body with these parameters.
func newWriteScope(info *types.Info, body ast.Node, params []*types.Var) writeScope {
	s := writeScope{params: map[types.Object]bool{}, body: body}
	for _, p := range params {
		if p != nil {
			s.params[p] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		var lhs []ast.Expr
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				lhs = n.Lhs
			}
		case *ast.IncDecStmt:
			lhs = []ast.Expr{n.X}
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				lhs = []ast.Expr{n.Key, n.Value}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				lhs = []ast.Expr{n.X}
			}
		}
		for _, e := range lhs {
			// A parameter whose own storage is assigned or addressed
			// may come to refer to anything.
			if e != nil && addressedInPlace(info, e) {
				if v := rootVar(info, e); v != nil {
					delete(s.params, v)
				}
			}
		}
		return true
	})
	return s
}

func (s writeScope) local(v *types.Var) bool {
	return v != nil && !isPackageVar(v) && s.body.Pos() <= v.Pos() && v.Pos() < s.body.End()
}

// localAddress reports an operand that refers to no storage but this
// call's own locals: a pointer-free value, or &x or x[:] of (part of) a
// local x itself.
func (a *evalAnalysis) localAddress(e ast.Expr, s writeScope) bool {
	info := a.f.info
	if t := info.Types[e].Type; t != nil && pointerFree(t) {
		return true
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		return x.Op == token.AND && s.local(rootVar(info, x.X)) && addressedInPlace(info, x.X)
	case *ast.SliceExpr:
		if _, ok := underlyingOf(info, x.X).(*types.Array); ok {
			return s.local(rootVar(info, x.X)) && addressedInPlace(info, x.X)
		}
	}
	return false
}

// addressedInPlace reports a storage expression that names part of its
// root variable itself, not storage reached through a pointer or slice.
func addressedInPlace(info *types.Info, e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			if sel := info.Selections[x]; sel == nil || sel.Indirect() {
				return false
			}
			if _, ok := underlyingOf(info, x.X).(*types.Struct); !ok {
				return false
			}
			e = x.X
		case *ast.IndexExpr:
			if _, ok := underlyingOf(info, x.X).(*types.Array); !ok {
				return false
			}
			e = x.X
		case *ast.Ident:
			return true
		default:
			return false
		}
	}
}

// calleeOperands lists the operands a call passes to its callee, in the
// callee's parameter order: the receiver first for a method (its address
// for a pointer-receiver method on an addressable value), then the
// arguments.
func (a *evalAnalysis) calleeOperands(c *ast.CallExpr) []ast.Expr {
	info := a.f.info
	var ops []ast.Expr
	if sel, ok := ast.Unparen(a.f.callFun(c.Fun)).(*ast.SelectorExpr); ok {
		if s := info.Selections[sel]; s != nil && s.Kind() == types.MethodVal {
			recv := sel.X
			if fn, ok := s.Obj().(*types.Func); ok {
				if _, want := pointer(fn.Type().(*types.Signature).Recv().Type()); want {
					if _, is := pointer(info.Types[sel.X].Type); !is {
						recv = &ast.UnaryExpr{Op: token.AND, X: sel.X, OpPos: sel.X.Pos()}
					}
				}
			}
			ops = append(ops, recv)
		}
	}
	return append(ops, c.Args...)
}

// sdkOutputs are the gosvm/solana functions that write bytes into an output
// slice the program supplies (their last argument); the other writing ones
// change account state only, which holds no Go variable.
var sdkOutputs = map[string]bool{"Clock": true, "Rent": true, "SHA256": true, "CreateAddress": true, "Secp256k1Recover": true}

// siteEffect is what a write site may write.
type siteEffect struct {
	types *writtenTypes
	// through lists the scope's parameters the site writes through; with
	// confined, it writes nothing else but the scope's own locals.
	through  []types.Object
	confined bool
	// local: it writes only the scope's own locals.
	local bool
}

// siteWrites is what a write site in scope s may write.
func (a *evalAnalysis) siteWrites(site ast.Node, s writeScope) siteEffect {
	info := a.f.info
	e := siteEffect{types: &writtenTypes{}, confined: true}
	target := func(x ast.Expr) {
		if t := info.Types[x].Type; t != nil {
			e.types.add(t)
		}
		if v := rootVar(info, x); v != nil && s.params[v] {
			e.through = append(e.through, v)
		} else {
			// A local pointer may point anywhere.
			e.confined = false
		}
	}
	switch n := site.(type) {
	case *ast.AssignStmt:
		for _, l := range n.Lhs {
			if id, ok := l.(*ast.Ident); ok && (n.Tok == token.DEFINE || id.Name == "_") {
				continue
			}
			target(l)
		}
	case *ast.IncDecStmt:
		target(n.X)
	case *ast.RangeStmt:
		for _, x := range []ast.Expr{n.Key, n.Value} {
			if x != nil {
				target(x)
			}
		}
	case *ast.CallExpr:
		return a.callWrites(n, s)
	default:
		e.types.all, e.confined = true, false
	}
	return e
}

// callWrites is siteWrites for a call: what its callee writes, through the
// operands it writes through.
func (a *evalAnalysis) callWrites(c *ast.CallExpr, s writeScope) siteEffect {
	info := a.f.info
	e := siteEffect{types: &writtenTypes{}, confined: true, local: true}
	if info.Types[c.Fun].IsType() {
		return e
	}
	via := func(ops []ast.Expr) {
		for _, op := range ops {
			if a.localAddress(op, s) {
				continue
			}
			e.local = false
			if v := rootVar(info, op); v != nil && s.params[v] {
				e.through = append(e.through, v)
			} else {
				e.confined = false
			}
		}
	}
	var object types.Object
	switch fun := ast.Unparen(a.f.callFun(c.Fun)).(type) {
	case *ast.Ident:
		object = a.f.callee(fun)
	case *ast.SelectorExpr:
		if a.f.ifaces.calls[c] == nil {
			object = a.f.callee(fun.Sel)
		}
	}
	switch o := object.(type) {
	case *types.Builtin:
		switch o.Name() {
		case "append", "copy", "clear", "delete":
			if len(c.Args) == 0 {
				return e
			}
			switch x := info.Types[c.Args[0]].Type.Underlying().(type) {
			case *types.Slice:
				e.types.add(x.Elem())
			case *types.Map:
				e.types.add(x.Key())
				e.types.add(x.Elem())
			default:
				e.types.all = true
			}
			via(c.Args[:1])
		}
		return e
	case *types.Func:
		if o.Pkg() != nil {
			switch o.Pkg().Path() {
			case "gosvm/solana":
				if sdkOutputs[o.Name()] && len(c.Args) > 0 {
					e.types.add(types.Typ[types.Uint8])
					via(c.Args[len(c.Args)-1:])
				}
				return e
			case bitsPath:
				return e
			}
		}
		summary := a.funcWrites(o)
		e.types.merge(summary.types)
		if !summary.confined {
			e.confined, e.local = false, false
			return e
		}
		ops := a.calleeOperands(c)
		var written []ast.Expr
		for i := range ops {
			if summary.through[i] {
				written = append(written, ops[i])
			}
		}
		via(written)
		return e
	}
	e.types.all, e.confined, e.local = true, false, false
	return e
}

// funcSummary is what a declared function may write outside its own locals:
// values of types, through the parameters at the indices in through
// (receiver first) when confined, or anything reachable otherwise.
type funcSummary struct {
	types    *writtenTypes
	confined bool
	through  map[int]bool
}

// funcWrites summarizes a declared function's write sites (function
// literals' bodies included).
func (a *evalAnalysis) funcWrites(o types.Object) *funcSummary {
	if w, ok := a.written[o]; ok {
		return w
	}
	conservative := &funcSummary{types: &writtenTypes{all: true}}
	a.written[o] = conservative // while visiting (recursion is rejected)
	fn := a.byObject[o]
	if fn == nil || fn.Library != nil || fn.Decl.Body == nil {
		return conservative
	}
	sig := o.Type().(*types.Signature)
	var params []*types.Var
	if sig.Recv() != nil {
		params = append(params, sig.Recv())
	}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	scope := newWriteScope(a.f.info, fn.Decl.Body, params)
	result := &funcSummary{types: &writtenTypes{}, confined: true, through: map[int]bool{}}
	for _, site := range a.writeSites(fn.Decl.Body, nil, false, false) {
		e := a.siteWrites(site, scope)
		result.types.merge(e.types)
		result.confined = result.confined && e.confined
		for _, v := range e.through {
			for i, p := range params {
				if p == v {
					result.through[i] = true
				}
			}
		}
	}
	a.written[o] = result
	return result
}
