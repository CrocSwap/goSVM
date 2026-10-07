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
	t := p.Elem()
	if isContext(t) {
		g.fail(n, "pointers to Context unsupported")
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Signature, *types.Interface:
		g.fail(n, "pointer elements must be unsigned/bool scalars, value structs or scalar arrays")
	}
	return g.variable(n, t) + " *"
}
func (g *generator) checkedPointer(n ast.Node, p types.Type, value string) string {
	return fmt.Sprintf("((%s)gosvm_nonnull(%s))", g.variable(n, p), value)
}
func (g *generator) pointed(e ast.Expr) string {
	p, ok := pointer(g.info.Types[e].Type)
	if !ok {
		g.fail(e, "dereference requires a supported pointer")
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
		return "&" + g.expr(e)
	case *ast.IndexExpr:
		if a, ok := arrayValue(g.info.Types[e.X].Type); ok {
			base := g.arrayPointer(e.X)
			index := g.expr(e.Index)
			return fmt.Sprintf("%s_at(%s,%s)", g.arrayName(e, a), base, index)
		}
		base, index := g.expr(e.X), g.expr(e.Index)
		return fmt.Sprintf("at(%s,%s)", base, index)
	default:
		g.fail(e, "address requires caller-owned local, field or element storage")
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
	fn := selection.Obj().(*types.Func)
	sig := fn.Type().(*types.Signature)
	receiver := sig.Recv().Type()
	var args []string
	if selection.Kind() == types.MethodExpr {
		args = g.callValues(call)
		actual := selection.Type().(*types.Signature).Params().At(0).Type()
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
	invocation := g.name(fn) + "(" + strings.Join(args, ",") + ")"
	if sig.Results().Len() == 0 {
		g.line("%s;", invocation)
		return ""
	}
	return g.temp(call, g.info.Types[call].Type, invocation)
}

// A root is a caller borrow, a local storage region, or SDK-owned invocation
// storage. Local roots have lexical scopes; callers are substituted at calls.
type borrowRoot struct {
	parameter int
	scope     *types.Scope
	node      ast.Node
	foreign   bool
}
type borrowSet map[borrowRoot]bool

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

type borrowSummary struct{ results []borrowSet }
type borrowAnalysis struct {
	g         *generator
	fn        *ast.FuncDecl
	summaries map[types.Object]borrowSummary
	vars      map[types.Object]borrowSet
	results   []borrowSet
	changed   bool
}

func (g *generator) checkBorrows(decls []ast.Decl, funcs map[types.Object]*ast.FuncDecl) {
	summaries := map[types.Object]borrowSummary{}
	var check func(types.Object)
	check = func(object types.Object) {
		if _, done := summaries[object]; done {
			return
		}
		fn := funcs[object]
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				var target types.Object
				switch e := c.Fun.(type) {
				case *ast.Ident:
					target = g.info.Uses[e]
				case *ast.SelectorExpr:
					target = g.info.Uses[e.Sel]
				}
				if funcs[target] != nil {
					check(target)
				}
			}
			return true
		})
		sig := object.Type().(*types.Signature)
		a := &borrowAnalysis{g: g, fn: fn, summaries: summaries, vars: map[types.Object]borrowSet{}, results: make([]borrowSet, sig.Results().Len())}
		for i := range a.results {
			a.results[i] = borrowSet{}
		}
		slot := 0
		if receiver := sig.Recv(); receiver != nil {
			if memoryValue(receiver.Type()) {
				a.vars[receiver] = borrowSet{borrowRoot{parameter: 0}: true}
			}
			slot++
		}
		for i := 0; i < sig.Params().Len(); i++ {
			p := sig.Params().At(i)
			if memoryValue(p.Type()) {
				a.vars[p] = borrowSet{borrowRoot{parameter: i + slot}: true}
			}
		}
		// Joining all assignments is conservative across branches and loops. It
		// never forgets an origin on a control-flow path, including back edges.
		for {
			a.changed = false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.AssignStmt:
					values := a.values(n.Rhs)
					for i, lhs := range n.Lhs {
						if i >= len(values) {
							break
						}
						if id, ok := lhs.(*ast.Ident); ok {
							obj := g.info.Uses[id]
							if definition := g.info.Defs[id]; definition != nil {
								obj = definition
							}
							a.assign(id, obj, values[i])
						}
					}
				case *ast.ValueSpec:
					values := a.values(n.Values)
					for i, id := range n.Names {
						if i < len(values) {
							a.assign(id, g.info.Defs[id], values[i])
						}
					}
				case *ast.ReturnStmt:
					values := a.values(n.Results)
					for i, value := range values {
						if i >= len(a.results) || !memoryValue(sig.Results().At(i).Type()) {
							continue
						}
						for root := range value {
							if root.scope != nil {
								g.fail(n, "reference escapes local storage; heap allocation unsupported")
							}
						}
						if addBorrows(a.results[i], value) {
							a.changed = true
						}
					}
				}
				return true
			})
			if !a.changed {
				break
			}
		}
		summaries[object] = borrowSummary{a.results}
	}
	for _, d := range decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			check(g.info.Defs[fn.Name])
		}
	}
}
func scopeWithin(child, parent *types.Scope) bool {
	for s := child; s != nil; s = s.Parent() {
		if s == parent {
			return true
		}
	}
	return false
}
func (a *borrowAnalysis) scopeAt(pos token.Pos) *types.Scope {
	var result *types.Scope
	for _, scope := range a.g.info.Scopes {
		if scope.Contains(pos) && (result == nil || scopeWithin(scope, result)) {
			result = scope
		}
	}
	return result
}
func (a *borrowAnalysis) assign(node ast.Node, obj types.Object, value borrowSet) {
	if obj == nil || obj.Name() == "_" || !memoryValue(obj.Type()) {
		return
	}
	for root := range value {
		if root.scope != nil && !scopeWithin(obj.Parent(), root.scope) {
			a.g.fail(node, "reference escapes its lexical storage scope; heap allocation unsupported")
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
		o := a.g.info.Uses[e]
		if o != nil {
			return borrowSet{borrowRoot{scope: o.Parent(), node: e, parameter: -1}: true}
		}
	case *ast.SelectorExpr:
		if _, ok := pointer(a.g.info.Types[e.X].Type); ok {
			return a.expr(e.X)
		}
		return a.address(e.X)
	case *ast.IndexExpr:
		if memoryValue(a.g.info.Types[e.X].Type) {
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
		return a.vars[a.g.info.Uses[e]]
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
		if memoryValue(a.g.info.Types[e.X].Type) {
			return a.expr(e.X)
		}
		return a.address(e.X)
	}
	return borrowSet{}
}
func (a *borrowAnalysis) values(expressions []ast.Expr) []borrowSet {
	var values []borrowSet
	for _, e := range expressions {
		if c, ok := e.(*ast.CallExpr); ok {
			if t, ok := a.g.info.Types[c].Type.(*types.Tuple); ok && t.Len() > 1 {
				values = append(values, a.call(c)...)
				continue
			}
		}
		values = append(values, a.expr(e))
	}
	return values
}
func (a *borrowAnalysis) call(call *ast.CallExpr) []borrowSet {
	if a.g.info.Types[call.Fun].IsType() && memoryValue(a.g.info.Types[call].Type) && len(call.Args) == 1 {
		return []borrowSet{a.expr(call.Args[0])}
	}
	var object types.Object
	var args []borrowSet
	switch e := call.Fun.(type) {
	case *ast.Ident:
		object = a.g.info.Uses[e]
	case *ast.SelectorExpr:
		object = a.g.info.Uses[e.Sel]
		if selection := a.g.info.Selections[e]; selection != nil && selection.Kind() == types.MethodVal {
			recv := object.Type().(*types.Signature).Recv().Type()
			if _, wantPointer := pointer(recv); wantPointer {
				if _, isPointer := pointer(a.g.info.Types[e.X].Type); isPointer {
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
		return []borrowSet{borrowSet{}}
	}
	sig := fn.Type().(*types.Signature)
	results := make([]borrowSet, sig.Results().Len())
	for i := range results {
		results[i] = borrowSet{}
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == "gosvm/solana" {
		for i := range results {
			if memoryValue(sig.Results().At(i).Type()) {
				results[i][borrowRoot{parameter: -1, foreign: true}] = true
			}
		}
		return results
	}
	summary := a.summaries[object]
	for i, origins := range summary.results {
		for root := range origins {
			if root.foreign {
				results[i][root] = true
			} else if root.parameter >= 0 && root.parameter < len(args) {
				addBorrows(results[i], args[root.parameter])
			}
		}
	}
	return results
}
