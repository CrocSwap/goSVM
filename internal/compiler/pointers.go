package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

const pointerRuntime = `
static inline void *gosvm_nonnull(void *p) { if (!p) abort(); return p; }
`

func pointer(t types.Type) (*types.Pointer, bool) { p, ok := t.(*types.Pointer); return p, ok }
func memoryValue(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice:
		return true
	}
	return false
}
func (g *generator) pointerType(n ast.Node, p *types.Pointer) string {
	return g.variable(n, p.Elem()) + " *"
}
func (g *generator) checkedPointer(n ast.Node, p types.Type, value string) string {
	return fmt.Sprintf("((%s)gosvm_nonnull(%s))", g.variable(n, p), value)
}
func (g *generator) pointed(e ast.Expr) string {
	p, ok := pointer(g.info.Types[e].Type)
	if !ok {
		g.unchecked(e)
	}
	value := g.expr(e)
	return g.temp(e, p.Elem(), "*"+g.checkedPointer(e, p, value))
}
func (g *generator) address(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return g.address(e.X)
	case *ast.Ident, *ast.SelectorExpr:
		return "&(" + g.fieldTarget(e) + ")"
	case *ast.StarExpr:
		return g.checkedPointer(e, g.info.Types[e.X].Type, g.expr(e.X)) // Go evaluates the indirection, including its nil check.
	case *ast.CompositeLit:
		if g.movedLits[e] {
			return g.box(e, g.info.Types[e].Type, g.expr(e))
		}
		return "&" + g.expr(e)
	case *ast.IndexExpr:
		if a, ok := arrayValue(g.info.Types[e.X].Type); ok {
			base := g.arrayPointer(e.X)
			index := g.expr(e.Index)
			return fmt.Sprintf("%s_at(%s,%s)", g.arrayName(e, a), base, index)
		}
		base, index := g.expr(e.X), g.expr(e.Index)
		if !isByteSlice(g.info.Types[e.X].Type) {
			return "&" + g.element(e, g.info.Types[e.X].Type, base, index)
		}
		return fmt.Sprintf("at(%s,%s)", base, index)
	default:
		g.unchecked(e)
	}
	return ""
}
func arrayValue(t types.Type) (*types.Array, bool) {
	if p, ok := pointer(t); ok {
		t = p.Elem()
	}
	a, ok := t.Underlying().(*types.Array)
	return a, ok
}
func (g *generator) arrayPointer(e ast.Expr) string {
	if p, ok := pointer(g.info.Types[e].Type); ok {
		return g.checkedPointer(e, p, g.expr(e))
	}
	if g.info.Types[e].Addressable() {
		return "&(" + g.fieldTarget(e) + ")"
	}
	return "&" + g.expr(e)
}
func (g *generator) methodCall(call *ast.CallExpr, selector *ast.SelectorExpr, selection *types.Selection) string {
	if isIface(g.info.Types[selector.X].Type) {
		return g.dynamicCall(call, selector)
	}
	fn, args := g.methodArgs(call, selector, selection)
	sig := fn.Type().(*types.Signature)
	invocation := g.name(fn) + "(" + strings.Join(args, ",") + ")"
	if sig.Results().Len() == 0 {
		g.line("%s;", invocation)
		return ""
	}
	return g.temp(call, g.info.Types[call].Type, invocation)
}

// methodArgs evaluates a static method call's receiver and arguments in
// Go's order and returns the method with its C arguments.
func (g *generator) methodArgs(call *ast.CallExpr, selector *ast.SelectorExpr, selection *types.Selection) (*types.Func, []string) {
	fn := g.method(selector)
	sig := fn.Type().(*types.Signature)
	receiver := sig.Recv().Type()
	var args []string
	if selection.Kind() == types.MethodExpr {
		args = g.callValues(call)
		actual := g.info.Types[selector].Type.(*types.Signature).Params().At(0).Type()
		if p, ok := pointer(actual); ok {
			if _, wantPointer := pointer(receiver); !wantPointer {
				args[0] = g.temp(call, receiver, "*"+g.checkedPointer(call, p, args[0]))
			}
		}
	} else {
		actual := g.info.Types[selector.X].Type
		value := ""
		if _, wantPointer := pointer(receiver); wantPointer {
			if _, isPointer := pointer(actual); isPointer {
				value = g.expr(selector.X)
			} else {
				value = g.temp(selector.X, receiver, g.address(selector.X))
			}
		} else if p, isPointer := pointer(actual); isPointer {
			// Save the pointer operand before argument calls, then copy its value
			// for the receiver when the call is ready. In particular, a nil
			// implicit receiver must not discard argument side effects.
			value = g.expr(selector.X)
			tail := g.callValues(call)
			value = g.temp(selector.X, receiver, "*"+g.checkedPointer(selector.X, p, value))
			args = append([]string{value}, tail...)
		} else {
			value = g.expr(selector.X)
		}
		if args == nil {
			args = append([]string{value}, g.callValues(call)...)
		}
	}
	return fn, args
}

// A root is a caller borrow, a local storage region, SDK-owned invocation
// storage or the invocation heap. Local roots have lexical scopes and name
// their storage: a variable (object) or an &T{...} literal (node). Callers are
// substituted at calls.
type borrowRoot struct {
	parameter int
	scope     *types.Scope
	node      ast.Node
	object    types.Object
	foreign   bool
	heap      bool
}
type borrowSet map[borrowRoot]bool

// heapRoot is invocation-lifetime storage reached through the allocator or
// loaded from a pointer/slice field or element; foreignRoot is SDK storage.
var (
	heapRoot    = borrowRoot{parameter: -1, heap: true}
	foreignRoot = borrowRoot{parameter: -1, foreign: true}
)

func addBorrows(to borrowSet, from borrowSet) bool {
	changed := false
	for root := range from {
		if !to[root] {
			to[root] = true
			changed = true
		}
	}
	return changed
}

// borrowSummary is one function's result origins, the argument slots whose
// storage it may store into heap data (callers move such local arguments to
// the heap) and its own locals that escape and so live on the heap.
type borrowSummary struct {
	results []borrowSet
	escapes map[int]bool
	vars    map[types.Object]bool
	lits    map[*ast.CompositeLit]bool
}
type borrowAnalysis struct {
	f         *frontend
	fn        *ast.FuncDecl
	summaries map[types.Object]borrowSummary
	vars      map[types.Object]borrowSet
	results   []borrowSet
	escapes   map[int]bool
	moved     map[types.Object]bool
	movedLits map[*ast.CompositeLit]bool
	sdk       bool
	changed   bool
	// Closures (docs/GO_FUNCTIONS.md): variables captured by the function's
	// literals live on the heap, so a reference stored in one is stored into
	// heap data and one read from it is heap or SDK storage, like a field.
	// Literal results are returned to unknown callers: stored likewise.
	captured   map[types.Object]bool
	litResults map[types.Object]bool
	litReturns map[*ast.ReturnStmt]bool
	named      map[types.Object]int // the function's named results
}

// checkBorrows proves that no pointer or slice outlives the storage it refers
// to and returns each function's result summary, keyed by its object. Each
// instance of a generic declaration has its own summary, including its own
// heap moves. A local whose reference escapes (returned, kept in a variable
// outside its scope or stored into a field, element, literal or append) is
// moved to the invocation heap, as Go's escape analysis would; see
// docs/GO_ALLOCATION.md. sdk reports whether the program imports
// gosvm/solana, whose views may be stored in heap data and loaded back.
func (f *frontend) checkBorrows(decls []ast.Decl, funcs map[types.Object]*Function, gp *genericProgram, sdk bool) map[types.Object]borrowSummary {
	summaries := map[types.Object]borrowSummary{}
	var check func(types.Object)
	check = func(object types.Object) {
		if _, done := summaries[object]; done {
			return
		}
		fn := funcs[object].Decl
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				var target types.Object
				if id := callIdent(f.callFun(c.Fun)); id != nil {
					target = f.callee(id)
				}
				if funcs[target] != nil {
					check(target)
				}
			}
			return true
		})
		defer gp.rethrow(funcs[object].Instance.context()) // name the instantiation in this body's diagnostics
		sig := object.Type().(*types.Signature)
		a := &borrowAnalysis{f: f, fn: fn, summaries: summaries, vars: map[types.Object]borrowSet{}, results: make([]borrowSet, sig.Results().Len()),
			escapes: map[int]bool{}, moved: map[types.Object]bool{}, movedLits: map[*ast.CompositeLit]bool{}, sdk: sdk,
			captured: map[types.Object]bool{}, litResults: map[types.Object]bool{}, litReturns: map[*ast.ReturnStmt]bool{}, named: map[types.Object]int{}}
		for i := range a.results {
			a.results[i] = borrowSet{}
			if r := sig.Results().At(i); r.Name() != "" {
				a.named[r] = i
			}
		}
		for _, cl := range f.funcs.owned[funcs[object]] {
			for _, v := range cl.Captures {
				a.captured[v], a.moved[v] = true, true
			}
			for _, p := range cl.Params {
				if p != nil && memoryValue(p.Type()) {
					a.vars[p] = a.loaded() // passed by an unknown caller: heap or SDK storage
				}
			}
			for _, r := range cl.Results {
				if r != nil {
					a.litResults[r] = true
				}
			}
			ast.Inspect(cl.Lit.Body, func(n ast.Node) bool {
				if r, ok := n.(*ast.ReturnStmt); ok {
					a.litReturns[r] = true
				}
				return true
			})
		}
		slot := 0
		parameter := func(p *types.Var, slot int) {
			if !memoryValue(p.Type()) {
				return
			}
			if a.captured[p] {
				// The incoming reference is copied to the heap.
				a.escapes[slot] = true
				a.vars[p] = a.loaded()
				return
			}
			a.vars[p] = borrowSet{borrowRoot{parameter: slot}: true}
		}
		if receiver := sig.Recv(); receiver != nil {
			parameter(receiver, 0)
			slot++
		}
		for i := 0; i < sig.Params().Len(); i++ {
			parameter(sig.Params().At(i), i+slot)
		}
		// Joining all assignments is conservative across branches and loops. It
		// never forgets an origin on a control-flow path, including back edges.
		for {
			a.changed = false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if e, ok := n.(ast.Expr); ok {
					a.conversions(e)
				}
				switch n := n.(type) {
				case *ast.TypeSwitchStmt:
					// A clause variable of pointer or slice type holds the
					// interface's data: heap or SDK storage.
					for _, stmt := range n.Body.List {
						if obj := f.info.Implicits[stmt]; obj != nil && memoryValue(obj.Type()) {
							a.assign(stmt, obj, a.loaded())
						}
					}
				case *ast.AssignStmt:
					values := a.values(n.Rhs)
					for i, lhs := range n.Lhs {
						if i >= len(values) {
							break
						}
						if id, ok := lhs.(*ast.Ident); ok {
							obj := f.info.Uses[id]
							if definition := f.info.Defs[id]; definition != nil {
								obj = definition
							}
							a.assign(id, obj, values[i])
						} else if memoryValue(f.info.Types[lhs].Type) {
							// A field, element or pointed-to target holds only heap
							// or SDK storage.
							a.store(values[i])
						}
						if ix, ok := f.isMapIndex(lhs); ok && memoryValue(f.info.Types[ix.Index].Type) {
							// A map key is stored in the map's heap table.
							a.store(a.expr(ix.Index))
						}
					}
				case *ast.ValueSpec:
					values := a.values(n.Values)
					for i, id := range n.Names {
						if i < len(values) {
							a.assign(id, f.info.Defs[id], values[i])
						}
					}
				case *ast.RangeStmt:
					if id, ok := n.Value.(*ast.Ident); ok {
						obj := f.info.Uses[id]
						if definition := f.info.Defs[id]; definition != nil {
							obj = definition
						}
						if obj != nil && memoryValue(obj.Type()) {
							a.assign(id, obj, a.loaded())
						}
					}
				case *ast.CompositeLit:
					a.literal(n)
				case *ast.CallExpr:
					a.call(n)
				case *ast.SelectorExpr:
					// A method value of a pointer-receiver method binds the
					// pointer in its heap environment.
					if t := f.funcs.byExpr[n]; t != nil && t.Kind == TargetMethodValue && t.PointerReceiver {
						if _, isPointer := pointer(f.info.Types[n.X].Type); isPointer {
							a.store(a.expr(n.X))
						} else {
							a.store(a.address(n.X))
						}
					}
				case *ast.DeferStmt:
					a.deferred(n)
				case *ast.ReturnStmt:
					values := a.values(n.Results)
					if a.litReturns[n] {
						// A literal's results go to callers through function values.
						for _, value := range values {
							a.store(value)
						}
						return true
					}
					for i, value := range values {
						if i >= len(a.results) || !memoryValue(sig.Results().At(i).Type()) {
							continue
						}
						a.result(i, value)
					}
				}
				return true
			})
			if !a.changed {
				break
			}
		}
		// A captured named result may be set by a closure (a deferred one,
		// for example): it may hold any heap or SDK reference.
		for obj, i := range a.named {
			if a.captured[obj] && memoryValue(obj.Type()) {
				addBorrows(a.results[i], a.loaded())
			}
		}
		summaries[object] = borrowSummary{results: a.results, escapes: a.escapes, vars: a.moved, lits: a.movedLits}
	}
	for _, d := range decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			check(f.info.Defs[fn.Name])
		}
	}
	return summaries
}
func scopeWithin(child, parent *types.Scope) bool {
	for s := child; s != nil; s = s.Parent() {
		if s == parent {
			return true
		}
	}
	return false
}

// scopeAt is the innermost scope of the analyzed function containing pos.
// Instances of a generic declaration share its positions, so only scopes
// within the function's own scope are considered.
func (a *borrowAnalysis) scopeAt(pos token.Pos) *types.Scope {
	var result *types.Scope
	function := a.f.info.Scopes[a.fn.Type]
	for _, scope := range a.f.info.Scopes {
		if scope.Contains(pos) && scopeWithin(scope, function) && (result == nil || scopeWithin(scope, result)) {
			result = scope
		}
	}
	return result
}

// move puts a local root's storage on the invocation heap.
func (a *borrowAnalysis) move(root borrowRoot) {
	if root.object != nil {
		a.moved[root.object] = true
	} else if lit, ok := root.node.(*ast.CompositeLit); ok {
		a.movedLits[lit] = true
	}
}

// store records that value is written into heap data: a field, element,
// pointed-to target, literal element or appended element. Such storage
// outlives any lexical scope, so locals move to the heap and parameter
// borrows become the caller's obligation.
func (a *borrowAnalysis) store(value borrowSet) {
	for root := range value {
		switch {
		case root.scope != nil:
			a.move(root)
		case root.parameter >= 0:
			a.escapes[root.parameter] = true
		}
	}
}

// conversions stores the references converted to interfaces at e: an
// interface value may be copied anywhere, so (as for heap data) the
// referenced storage must outlive every scope. A local moves to the heap; a
// parameter's storage becomes the caller's obligation.
func (a *borrowAnalysis) conversions(e ast.Expr) {
	if conv := a.f.ifaces.byExpr[e]; conv != nil && !conv.rng && memoryValue(conv.From) {
		a.store(a.expr(e))
	}
	for _, conv := range a.f.ifaces.byTuple[e] {
		if !memoryValue(conv.From) {
			continue
		}
		if call, ok := ast.Unparen(e).(*ast.CallExpr); ok {
			if values := a.call(call); conv.Index < len(values) {
				a.store(values[conv.Index])
			}
		}
	}
}

// loaded is the origin of a reference read from a field or element: the
// store rule guarantees it is heap or SDK storage, never a local.
func (a *borrowAnalysis) loaded() borrowSet {
	if a.sdk {
		return borrowSet{heapRoot: true, foreignRoot: true}
	}
	return borrowSet{heapRoot: true}
}

// literal stores the reference-typed fields or elements of a struct or slice
// literal.
func (a *borrowAnalysis) literal(lit *ast.CompositeLit) {
	t := a.f.info.Types[lit].Type
	if t == nil {
		return
	}
	if p, ok := pointer(t); ok {
		t = p.Elem()
	}
	switch t.Underlying().(type) {
	case *types.Struct, *types.Slice:
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			if memoryValue(a.f.info.Types[elt].Type) {
				a.store(a.expr(elt))
			}
		}
	case *types.Map:
		// Keys and values are stored in the map's heap table.
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				for _, e := range []ast.Expr{kv.Key, kv.Value} {
					if memoryValue(a.f.info.Types[e].Type) {
						a.store(a.expr(e))
					}
				}
			}
		}
	}
}

// result records value as the origin of result i: a local outlives the
// call, so it lives on the heap.
func (a *borrowAnalysis) result(i int, value borrowSet) {
	for root := range value {
		if root.scope != nil {
			// The local outlives this call: it lives on the heap.
			a.move(root)
			root = heapRoot
		}
		if !a.results[i][root] {
			a.results[i][root] = true
			a.changed = true
		}
	}
}

// deferred keeps the references a deferred call saves alive until the
// function returns: locals of an inner block (whose storage ends with the
// block) move to the heap; the body's own locals outlive the deferred call.
func (a *borrowAnalysis) deferred(n *ast.DeferStmt) {
	site := a.f.funcs.byDefer[n]
	if site == nil {
		return
	}
	top := a.f.info.Scopes[a.fn.Type]
	if site.Lit != nil {
		top = a.f.info.Scopes[site.Lit.Type]
	}
	values := a.values(n.Call.Args)
	if sel, ok := n.Call.Fun.(*ast.SelectorExpr); ok {
		if s := a.f.info.Selections[sel]; s != nil && s.Kind() == types.MethodVal && !isIface(a.f.info.Types[sel.X].Type) {
			if fn, ok := a.f.callee(sel.Sel).(*types.Func); ok {
				if _, wantPointer := pointer(fn.Type().(*types.Signature).Recv().Type()); wantPointer {
					if _, isPointer := pointer(a.f.info.Types[sel.X].Type); isPointer {
						values = append(values, a.expr(sel.X))
					} else {
						values = append(values, a.address(sel.X))
					}
				}
			}
		}
	}
	for _, value := range values {
		for root := range value {
			if root.scope != nil && root.scope != top {
				a.move(root)
			}
		}
	}
}

func (a *borrowAnalysis) assign(node ast.Node, obj types.Object, value borrowSet) {
	if obj == nil || obj.Name() == "_" || !memoryValue(obj.Type()) {
		return
	}
	if a.captured[obj] || a.litResults[obj] {
		// Heap storage: the reference is stored, and reading it back
		// yields heap or SDK storage.
		a.store(value)
		if a.vars[obj] == nil {
			a.vars[obj] = borrowSet{}
		}
		if addBorrows(a.vars[obj], a.loaded()) {
			a.changed = true
		}
		return
	}
	if i, ok := a.named[obj]; ok {
		a.result(i, value)
	}
	for root := range value {
		if root.scope != nil && !scopeWithin(obj.Parent(), root.scope) {
			// Kept beyond its lexical scope: the local lives on the heap.
			a.move(root)
		}
	}
	if a.vars[obj] == nil {
		a.vars[obj] = borrowSet{}
	}
	if addBorrows(a.vars[obj], value) {
		a.changed = true
	}
}
func (a *borrowAnalysis) address(e ast.Expr) borrowSet {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return a.address(e.X)
	case *ast.Ident:
		o := a.f.info.Uses[e]
		if o != nil {
			return borrowSet{borrowRoot{scope: o.Parent(), object: o, parameter: -1}: true}
		}
	case *ast.SelectorExpr:
		if _, ok := pointer(a.f.info.Types[e.X].Type); ok {
			return a.expr(e.X)
		}
		return a.address(e.X)
	case *ast.IndexExpr:
		if memoryValue(a.f.info.Types[e.X].Type) {
			return a.expr(e.X)
		}
		return a.address(e.X)
	case *ast.StarExpr:
		return a.expr(e.X)
	case *ast.CompositeLit:
		return borrowSet{borrowRoot{scope: a.scopeAt(e.Pos()), node: e, parameter: -1}: true}
	}
	return borrowSet{}
}
func (a *borrowAnalysis) expr(e ast.Expr) borrowSet {
	if e == nil {
		return borrowSet{}
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return a.expr(e.X)
	case *ast.Ident:
		return a.vars[a.f.info.Uses[e]]
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return a.address(e.X)
		}
	case *ast.CallExpr:
		values := a.call(e)
		if len(values) > 0 {
			return values[0]
		}
	case *ast.SliceExpr:
		if isString(a.f.info.Types[e.X].Type) {
			return borrowSet{} // a string: read-only data or heap bytes
		}
		if memoryValue(a.f.info.Types[e.X].Type) {
			return a.expr(e.X)
		}
		return a.address(e.X)
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
		if memoryValue(a.f.info.Types[e].Type) {
			return a.loaded()
		}
		if ix, ok := a.f.isMapIndex(e); ok && memoryValue(mapOf(a.f.info.Types[ix.X].Type).Elem()) {
			return a.loaded() // v, ok := m[k]: the element is heap or SDK storage
		}
	case *ast.TypeAssertExpr:
		// A pointer or slice held by an interface value: heap or SDK storage.
		if e.Type != nil && memoryValue(a.f.info.Types[e.Type].Type) {
			return a.loaded()
		}
	case *ast.CompositeLit:
		// Slice literals and the elided &T{...} elements of slice literals
		// are allocated on the heap.
		switch a.f.info.Types[e].Type.Underlying().(type) {
		case *types.Slice, *types.Pointer:
			return borrowSet{heapRoot: true}
		}
	}
	return borrowSet{}
}
func (a *borrowAnalysis) values(expressions []ast.Expr) []borrowSet {
	var values []borrowSet
	for _, e := range expressions {
		if c, ok := e.(*ast.CallExpr); ok {
			if t, ok := a.f.info.Types[c].Type.(*types.Tuple); ok && t.Len() > 1 {
				values = append(values, a.call(c)...)
				continue
			}
		}
		values = append(values, a.expr(e))
	}
	return values
}

// builtin gives the origins of new, make and append results. append's result
// may alias its first operand or a new heap array; appended references are
// stored. The elements of an appended or copied slice are already heap or SDK
// storage.
func (a *borrowAnalysis) builtin(call *ast.CallExpr, name string) []borrowSet {
	switch name {
	case "new", "make":
		return []borrowSet{{heapRoot: true}}
	case "append":
		result := borrowSet{heapRoot: true}
		if len(call.Args) > 0 {
			addBorrows(result, a.expr(call.Args[0]))
		}
		if !call.Ellipsis.IsValid() && len(call.Args) > 1 {
			for _, value := range call.Args[1:] {
				if memoryValue(a.f.info.Types[value].Type) {
					a.store(a.expr(value))
				}
			}
		}
		return []borrowSet{result}
	}
	return []borrowSet{{}}
}
func (a *borrowAnalysis) call(call *ast.CallExpr) []borrowSet {
	if a.f.info.Types[call.Fun].IsType() && memoryValue(a.f.info.Types[call].Type) && len(call.Args) == 1 {
		if isString(a.f.info.Types[call.Args[0]].Type) {
			a.expr(call.Args[0])
			return []borrowSet{{heapRoot: true}} // []byte(s), []rune(s) copy to the heap
		}
		return []borrowSet{a.expr(call.Args[0])}
	}
	if id, ok := call.Fun.(*ast.Ident); ok {
		if b, ok := a.f.info.Uses[id].(*types.Builtin); ok {
			return a.builtin(call, b.Name())
		}
	}
	var object types.Object
	var args []borrowSet
	switch e := a.f.callFun(call.Fun).(type) {
	case *ast.Ident:
		object = a.f.callee(e)
	case *ast.SelectorExpr:
		object = a.f.callee(e.Sel)
		if selection := a.f.info.Selections[e]; selection != nil && selection.Kind() == types.MethodVal {
			recv := object.Type().(*types.Signature).Recv().Type()
			if _, wantPointer := pointer(recv); wantPointer {
				if _, isPointer := pointer(a.f.info.Types[e.X].Type); isPointer {
					args = append(args, a.expr(e.X))
				} else {
					args = append(args, a.address(e.X))
				}
			} else {
				args = append(args, borrowSet{})
			}
		}
	}
	args = append(args, a.values(call.Args)...)
	fn, ok := object.(*types.Func)
	if !ok {
		if sig := signatureOf(a.f.info, call.Fun); sig != nil {
			// A call through a function value may run any target: every
			// reference argument may be stored, and a reference result is
			// heap or SDK storage.
			for _, arg := range args {
				a.store(arg)
			}
			results := make([]borrowSet, sig.Results().Len())
			for i := range results {
				results[i] = borrowSet{}
				if memoryValue(sig.Results().At(i).Type()) {
					results[i] = a.loaded()
				}
			}
			if len(results) == 0 {
				return []borrowSet{borrowSet{}}
			}
			return results
		}
		return []borrowSet{borrowSet{}}
	}
	sig := fn.Type().(*types.Signature)
	results := make([]borrowSet, sig.Results().Len())
	for i := range results {
		results[i] = borrowSet{}
	}
	if a.f.ifaces.calls[call] != nil {
		// A dynamic call may run any implementation: every reference
		// argument may be stored, and a reference result is heap or SDK
		// storage.
		for _, arg := range args {
			a.store(arg)
		}
		for i := range results {
			if memoryValue(sig.Results().At(i).Type()) {
				results[i] = a.loaded()
			}
		}
		return results
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == "gosvm/solana" {
		for i := range results {
			if memoryValue(sig.Results().At(i).Type()) {
				results[i][foreignRoot] = true
			}
		}
		return results
	}
	summary := a.summaries[object]
	for slot := range summary.escapes {
		if slot < len(args) {
			a.store(args[slot])
		}
	}
	for i, origins := range summary.results {
		for root := range origins {
			if root.foreign || root.heap {
				results[i][root] = true
			} else if root.parameter >= 0 && root.parameter < len(args) {
				addBorrows(results[i], args[root.parameter])
			}
		}
	}
	return results
}
