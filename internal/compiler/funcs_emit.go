package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Emitter for function values, closures and defer (docs/GO_FUNCTIONS.md).

// funcRuntime is emitted for programs with function values or defer.
const funcRuntime = `
/* Function value: 0 (nil), the ID of a target without an environment
   (below 2^16), or a heap closure whose first word is the target ID. */
typedef u64 gosvm_func;
#define GOSVM_FUNC_ID(f) ((f) <= 0xffffULL ? (f) : *(const u64 *)(f))
/* Deferred calls of a function invocation, most recent first. */
typedef struct gosvm_defer { struct gosvm_defer *next; u64 site; } gosvm_defer;
`

// fnContext is the C function being emitted when it has named results or
// defer statements.
type fnContext struct {
	results *types.Tuple
	named   []*types.Var // named results; nil when unnamed
	sites   []*DeferSite // defer statements of this body
}

// namedResults are a signature's result variables when they are named.
func namedResults(t *types.Tuple) []*types.Var {
	if t.Len() == 0 || t.At(0).Name() == "" {
		return nil
	}
	out := make([]*types.Var, t.Len())
	for i := range out {
		out[i] = t.At(i)
	}
	return out
}

// funcValue makes the function value of e.
func (g *generator) funcValue(e ast.Expr, t *FuncTarget) string {
	switch t.Kind {
	case TargetLiteral:
		return g.closureValue(e.(*ast.FuncLit), t)
	case TargetMethodValue:
		return g.methodValueExpr(e.(*ast.SelectorExpr), t)
	}
	return fmt.Sprintf("%dULL", t.ID)
}

// closureValue makes a literal's value: its ID without captures, otherwise
// a new environment holding the ID and the captured variables' addresses.
func (g *generator) closureValue(lit *ast.FuncLit, t *FuncTarget) string {
	cl := g.funcs.byLit[lit]
	if cl == nil {
		g.unchecked(lit)
	}
	if len(cl.Captures) == 0 {
		return fmt.Sprintf("%dULL", t.ID)
	}
	g.next++
	env := fmt.Sprintf("env%d", g.next)
	g.line("u64 *%s = (u64 *)gosvm_alloc(%dULL);", env, cl.EnvBytes)
	g.line("%s[0] = %dULL;", env, t.ID)
	for i, v := range cl.Captures {
		if !g.moved[v] {
			g.unchecked(lit)
		}
		g.line("%s[%d] = (u64)%s;", env, i+1, g.rawName(v))
	}
	return g.temp(lit, t.Signature, "(gosvm_func)"+env)
}

// methodValueExpr makes x.M: a new environment holding the ID and the
// receiver pointer (pointer receivers) or a copy of the receiver.
func (g *generator) methodValueExpr(e *ast.SelectorExpr, t *FuncTarget) string {
	x := g.info.Types[e.X].Type
	receiver := t.Func.Type().(*types.Signature).Recv().Type()
	g.next++
	env := fmt.Sprintf("env%d", g.next)
	if t.PointerReceiver {
		var p string
		if _, isPointer := pointer(x); isPointer {
			p = g.expr(e.X)
		} else {
			p = g.temp(e.X, receiver, g.address(e.X))
		}
		g.line("u64 *%s = (u64 *)gosvm_alloc(16ULL);", env)
		g.line("%s[0] = %dULL;", env, t.ID)
		g.line("%s[1] = (u64)%s;", env, p)
		return g.temp(e, t.Signature, "(gosvm_func)"+env)
	}
	var value string
	if p, isPointer := pointer(x); isPointer {
		v := g.expr(e.X)
		value = g.temp(e.X, receiver, "*"+g.checkedPointer(e.X, p, v))
	} else {
		value = g.expr(e.X)
	}
	size := (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(receiver)
	g.line("u8 *%s = gosvm_alloc(%dULL);", env, 8+(size+7)&^7)
	g.line("*(u64 *)%s = %dULL;", env, t.ID)
	if size > 0 {
		g.line("*(%s *)(%s + 8) = %s;", g.variable(e, receiver), env, value)
	}
	return g.temp(e, t.Signature, "(gosvm_func)"+env)
}

// indirectCall lowers a call through a function value: the value, then the
// arguments, then the dispatcher (a nil value aborts). A literal called
// where it is written is called directly.
func (g *generator) indirectCall(e *ast.CallExpr) string {
	var invocation string
	if lit, ok := ast.Unparen(e.Fun).(*ast.FuncLit); ok {
		f := g.expr(e.Fun)
		args := append([]string{f}, g.callValues(e)...)
		invocation = fmt.Sprintf("gosvm_lit%d(%s)", g.funcs.byLit[lit].Target.ID, strings.Join(args, ","))
	} else {
		class := g.funcs.indirect[e]
		if class == nil {
			g.unchecked(e)
		}
		f := g.expr(e.Fun)
		args := append([]string{f}, g.callValues(e)...)
		invocation = fmt.Sprintf("gosvm_fcall%d(%s)", class.Index, strings.Join(args, ","))
	}
	tv := g.info.Types[e]
	if tup, ok := tv.Type.(*types.Tuple); ok && tup.Len() == 0 {
		g.line("%s;", invocation)
		return ""
	}
	return g.temp(e, tv.Type, invocation)
}

// returnType is the C type a function with results t returns.
func (g *generator) returnType(n ast.Node, t *types.Tuple) string {
	switch t.Len() {
	case 0:
		return "void"
	case 1:
		return g.variable(n, t.At(0).Type())
	}
	return g.resultName(n, t)
}

// literalSignature is a literal's C function header.
func (g *generator) literalSignature(cl *Closure) string {
	sig := cl.Target.Signature
	args := []string{"gosvm_func gosvm_env"}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		if i < len(cl.Params) && cl.Params[i] != nil {
			p = cl.Params[i]
		}
		name := g.rawName(p)
		if g.moved[p] {
			name = g.movedParameter(p)
		}
		args = append(args, g.variable(cl.Lit, sig.Params().At(i).Type())+" "+name)
	}
	return fmt.Sprintf("static %s gosvm_lit%d(%s)", g.returnType(cl.Lit, sig.Results()), cl.Target.ID, strings.Join(args, ","))
}

// emitLiteral defines a literal's C function: the captured variables'
// addresses are loaded from the environment, and moved parameters copied
// to the heap.
func (g *generator) emitLiteral(cl *Closure) {
	sig := cl.Target.Signature
	sites := g.funcs.bodyDefers[cl.Lit]
	g.emitDeferTypes(cl.Lit, sites)
	g.WriteString(g.literalSignature(cl))
	g.line("{")
	for i, v := range cl.Captures {
		t := g.variable(cl.Lit, v.Type())
		g.line("%s *%s = (%s *)((u64 *)gosvm_env)[%d];", t, g.rawName(v), t, i+1)
	}
	for _, p := range cl.Params {
		if p != nil && g.moved[p] {
			g.declareMoved(cl.Lit, p, g.movedParameter(p))
		}
	}
	g.currentResults = sig.Results()
	g.emitBody(cl.Lit, cl.Lit.Body, sig.Results(), cl.Results, sites)
	g.line("}")
}

// emitBody emits a body inside its C function's block. Named results are
// zeroed first. With defer statements, the body's statements share that
// block (so its locals outlive the deferred calls), results are stored and
// every return runs the deferred calls, most recent first, before
// returning.
func (g *generator) emitBody(n ast.Node, body *ast.BlockStmt, results *types.Tuple, named []*types.Var, sites []*DeferSite) {
	if named == nil && len(sites) == 0 {
		g.block(body)
		return
	}
	g.fn = &fnContext{results: results, named: named, sites: sites}
	defer func() { g.fn = nil }()
	for _, v := range named {
		if g.moved[v] {
			g.declareMoved(n, v, "{0}")
		} else {
			g.line("%s %s = {0};", g.variable(n, v.Type()), g.name(v))
		}
	}
	if len(sites) == 0 {
		g.block(body)
		return
	}
	if named == nil && results.Len() > 0 {
		g.line("%s gosvm_res = {0};", g.returnType(n, results))
	}
	g.line("gosvm_defer *gosvm_dl = 0;")
	for _, site := range sites {
		if !site.InLoop {
			g.line("gosvm_d%d gosvm_dr%d;", site.Index, site.Index)
		}
	}
	for _, s := range body.List {
		g.stmt(s)
	}
	g.line("goto gosvm_exit;")
	g.line("gosvm_exit: ;")
	g.line("while (gosvm_dl) {")
	g.line("gosvm_defer *gosvm_dc = gosvm_dl;")
	g.line("gosvm_dl = gosvm_dc->next;")
	g.line("switch (gosvm_dc->site) {")
	for _, site := range sites {
		g.line("case %dULL: { gosvm_d%d *r = (gosvm_d%d *)gosvm_dc; %s; break; }", site.Index, site.Index, site.Index, g.deferInvocation(site))
	}
	g.line("}\n}")
	if results.Len() == 0 {
		g.line("return;")
		return
	}
	g.line("return %s;", g.resultValue(n))
}

// resultStorage is where result i is stored before a return.
func (g *generator) resultStorage(i int) string {
	if g.fn.named != nil {
		return g.name(g.fn.named[i])
	}
	if g.fn.results.Len() == 1 {
		return "gosvm_res"
	}
	return fmt.Sprintf("gosvm_res.r%d", i)
}

// resultValue is the value a function with stored results returns.
func (g *generator) resultValue(n ast.Node) string {
	if g.fn.named == nil {
		return "gosvm_res"
	}
	if len(g.fn.named) == 1 {
		return g.name(g.fn.named[0])
	}
	fields := make([]string, len(g.fn.named))
	for i, v := range g.fn.named {
		fields[i] = fmt.Sprintf(".r%d=%s", i, g.name(v))
	}
	return fmt.Sprintf("(%s){%s}", g.resultName(n, g.fn.results), strings.Join(fields, ","))
}

// returnStmt lowers return in a function with named results or defer
// statements: the values are evaluated, then stored, then the deferred
// calls run (they may change named results) and the stored results are
// returned.
func (g *generator) returnStmt(s *ast.ReturnStmt) {
	if len(s.Results) > 0 {
		values := g.expressionValuesAs(s.Results, tupleTypes(g.fn.results))
		if len(values) != g.fn.results.Len() {
			g.unchecked(s)
		}
		for i, v := range values {
			g.line("%s = %s;", g.resultStorage(i), v)
		}
	}
	if len(g.fn.sites) > 0 {
		g.line("goto gosvm_exit;")
		return
	}
	if g.fn.results.Len() == 0 {
		g.line("return;")
		return
	}
	g.line("return %s;", g.resultValue(s))
}

// emitDeferTypes declares the record type of each defer statement: the
// list header, then the saved values.
func (g *generator) emitDeferTypes(n ast.Node, sites []*DeferSite) {
	for _, site := range sites {
		fields := []string{"gosvm_defer h;"}
		for i, t := range site.Saved {
			fields = append(fields, fmt.Sprintf("%s a%d;", g.variable(n, t), i))
		}
		g.line("typedef struct { %s } gosvm_d%d;", strings.Join(fields, " "), site.Index)
	}
}

// deferStmt evaluates a deferred call's function value (or receiver) and
// arguments as Go does at the defer statement, saves them in the
// statement's record (on the heap inside a loop) and pushes it.
func (g *generator) deferStmt(s *ast.DeferStmt) {
	site := g.funcs.byDefer[s]
	if site == nil {
		g.unchecked(s)
	}
	call := s.Call
	var values []string
	switch site.Kind {
	case DeferFunction:
		values = g.callValues(call)
	case DeferMethod:
		selector := call.Fun.(*ast.SelectorExpr)
		selection := g.info.Selections[selector]
		if selection.Kind() == types.MethodExpr {
			values = g.callValues(call) // (*T).M dereferences when called
		} else {
			_, values = g.methodArgs(call, selector, selection)
		}
	case DeferDynamic:
		selector := call.Fun.(*ast.SelectorExpr)
		x := g.expr(selector.X)
		values = append([]string{x}, g.callValues(call)...)
		g.line("if (%s.type == 0) abort();", x) // Go evaluates the method value: a nil interface panics here
	case DeferIndirect, DeferLiteral:
		f := g.expr(call.Fun)
		values = append([]string{f}, g.callValues(call)...)
	case DeferCopy:
		t := g.info.Types[call.Args[0]].Type
		values = []string{g.exprAs(call.Args[0], t), g.exprAs(call.Args[1], t)}
	}
	if len(values) != len(site.Saved) {
		g.unchecked(s)
	}
	record := fmt.Sprintf("gosvm_dr%d.", site.Index)
	if site.InLoop {
		g.next++
		r := fmt.Sprintf("defer%d", g.next)
		g.line("gosvm_d%d *%s = (gosvm_d%d *)gosvm_alloc(sizeof(gosvm_d%d));", site.Index, r, site.Index, site.Index)
		record = r + "->"
	}
	for i, v := range values {
		g.line("%sa%d = %s;", record, i, v)
	}
	g.line("%sh.site = %dULL;", record, site.Index)
	g.line("%sh.next = gosvm_dl;", record)
	g.line("gosvm_dl = &%sh;", record)
}

// deferInvocation is the call a deferred record r makes when the function
// returns.
func (g *generator) deferInvocation(site *DeferSite) string {
	call := site.Stmt.Call
	args := make([]string, len(site.Saved))
	for i := range args {
		args[i] = fmt.Sprintf("r->a%d", i)
	}
	invoke := func(name string, args []string) string { return name + "(" + strings.Join(args, ",") + ")" }
	switch site.Kind {
	case DeferFunction:
		id := callIdent(g.callFun(call.Fun))
		if id == nil {
			g.unchecked(call)
		}
		return invoke(g.name(g.callee(id)), args)
	case DeferMethod:
		selector := call.Fun.(*ast.SelectorExpr)
		fn := g.method(selector)
		if g.info.Selections[selector].Kind() == types.MethodExpr {
			receiver := fn.Type().(*types.Signature).Recv().Type()
			actual := g.info.Types[selector].Type.(*types.Signature).Params().At(0).Type()
			if p, ok := pointer(actual); ok {
				if _, wantPointer := pointer(receiver); !wantPointer {
					args[0] = "*" + g.checkedPointer(call, p, args[0])
				}
			}
		}
		return invoke(g.name(fn), args)
	case DeferDynamic:
		slot := g.ifaces.calls[call]
		if slot == nil {
			g.unchecked(call)
		}
		return invoke(fmt.Sprintf("gosvm_call%d", slot.Index), args)
	case DeferIndirect:
		class := g.funcs.indirect[call]
		if class == nil {
			g.unchecked(call)
		}
		return invoke(fmt.Sprintf("gosvm_fcall%d", class.Index), args)
	case DeferLiteral:
		cl := g.funcs.byLit[ast.Unparen(call.Fun).(*ast.FuncLit)]
		return invoke(fmt.Sprintf("gosvm_lit%d", cl.Target.ID), args)
	case DeferCopy:
		t := g.info.Types[call.Args[0]].Type
		elem := g.variable(call, t.(*types.Slice).Elem())
		return fmt.Sprintf("(void)gosvm_copy(r->a0,r->a1,sizeof(%s))", elem)
	}
	g.unchecked(call)
	return ""
}

// emitFuncDispatchers defines one dispatcher per signature called through
// function values: it compares the value's target ID with each target of
// the signature, in ID order, and calls it; nil (and no match) aborts.
func (g *generator) emitFuncDispatchers() {
	node := posNode(token.NoPos)
	for _, class := range g.funcs.classes {
		sig := class.Signature
		ret := g.returnType(node, sig.Results())
		params := []string{"gosvm_func f"}
		var names []string
		for i := 0; i < sig.Params().Len(); i++ {
			params = append(params, fmt.Sprintf("%s p%d", g.variable(node, sig.Params().At(i).Type()), i))
			names = append(names, fmt.Sprintf("p%d", i))
		}
		g.line("static %s gosvm_fcall%d(%s) { /* %s */", ret, class.Index, strings.Join(params, ","), typeKey(sig))
		if len(class.Targets) > 0 {
			g.line("u64 id = GOSVM_FUNC_ID(f);")
		}
		for _, t := range class.Targets {
			invocation := g.targetInvocation(t, names)
			if ret == "void" {
				g.line("if (id == %dULL) { %s; return; }", t.ID, invocation)
			} else {
				g.line("if (id == %dULL) return %s;", t.ID, invocation)
			}
		}
		g.line("abort();\n}")
	}
}

// targetInvocation calls target t from a dispatcher with value f and
// arguments names.
func (g *generator) targetInvocation(t *FuncTarget, names []string) string {
	node := posNode(t.Site)
	args := append([]string(nil), names...)
	switch t.Kind {
	case TargetLiteral:
		args = append([]string{"f"}, args...)
		return fmt.Sprintf("gosvm_lit%d(%s)", t.ID, strings.Join(args, ","))
	case TargetMethodValue:
		receiver := t.Func.Type().(*types.Signature).Recv().Type()
		var bound string
		switch {
		case t.PointerReceiver:
			bound = fmt.Sprintf("(%s)((u64 *)f)[1]", g.variable(node, receiver))
		case (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(receiver) == 0:
			bound = fmt.Sprintf("(%s){0}", g.variable(node, receiver))
		default:
			bound = fmt.Sprintf("*(%s *)((u8 *)f + 8)", g.variable(node, receiver))
		}
		args = append([]string{bound}, args...)
	case TargetMethodExpr:
		if t.Deref {
			receiver := t.Func.Type().(*types.Signature).Recv().Type()
			args[0] = fmt.Sprintf("*(%s *)gosvm_nonnull(%s)", g.variable(node, receiver), args[0])
		}
	}
	return g.name(t.Func) + "(" + strings.Join(args, ",") + ")"
}

// freshLoopVariables gives each iteration of a three-clause loop its own
// copy of the heap-moved variables its init statement declares (Go 1.22):
// the copy is made before the post statement.
func (g *generator) freshLoopVariables(s *ast.ForStmt) []types.Object {
	init, ok := s.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE {
		return nil
	}
	var out []types.Object
	for _, lhs := range init.Lhs {
		if id, ok := lhs.(*ast.Ident); ok {
			if o := g.info.Defs[id]; o != nil && g.moved[o] {
				out = append(out, o)
			}
		}
	}
	return out
}

func (g *generator) copyLoopVariables(s *ast.ForStmt, vars []types.Object) {
	for _, o := range vars {
		t := g.variable(s, o.Type())
		raw := g.rawName(o)
		g.next++
		fresh := fmt.Sprintf("t%d", g.next)
		g.line("%s *%s = (%s *)gosvm_alloc(sizeof(%s));", t, fresh, t, t)
		g.line("*%s = *%s;", fresh, raw)
		g.line("%s = %s;", raw, fresh)
	}
}
