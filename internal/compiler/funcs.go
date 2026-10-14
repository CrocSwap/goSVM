package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// Function values, closures and defer. A function value is one 64-bit word
// (Go's size): 0 is nil; a value below 2^16 is the ID of a target that needs
// no environment (a declared function, a generic instance, a method
// expression or a literal without captures); any other value points at a
// heap object whose first word is the target ID, followed by the captured
// variables' addresses (closures) or the bound receiver (method values).
// Calls through a value dispatch on the ID with compare-and-branch code, one
// dispatcher per called signature, over the program's static set of targets
// of that signature. See docs/GO_FUNCTIONS.md.

// FuncTargetKind classifies a function value target.
type FuncTargetKind int

const (
	// TargetFunction is a declared function (or a generic function's
	// instance) used as a value.
	TargetFunction FuncTargetKind = iota + 1
	// TargetLiteral is a function literal.
	TargetLiteral
	// TargetMethodValue is x.M: the receiver is bound when the value is made.
	TargetMethodValue
	// TargetMethodExpr is T.M or (*T).M: the receiver is the first argument.
	TargetMethodExpr
)

func (k FuncTargetKind) String() string {
	switch k {
	case TargetFunction:
		return "function"
	case TargetLiteral:
		return "literal"
	case TargetMethodValue:
		return "method value"
	case TargetMethodExpr:
		return "method expression"
	}
	return fmt.Sprintf("FuncTargetKind(%d)", int(k))
}

// FuncTarget is one function that a function value may hold: the program's
// address-taken set. IDs are 1-based in discovery order (Functions order,
// then source order); the same declared function, method value or method
// expression used at several sites is one target, each literal is its own.
type FuncTarget struct {
	ID   uint64
	Kind FuncTargetKind
	// Func is the function or method called (an Instance.Object for generic
	// code); nil for literals.
	Func *types.Func
	// Lit is the literal, for TargetLiteral.
	Lit *ast.FuncLit
	// Signature is the value's signature: a method expression's receiver is
	// its first parameter; a method value's receiver is not a parameter.
	Signature *types.Signature
	// PointerReceiver is set for a method value of a pointer-receiver
	// method: the value binds the pointer (16 heap bytes). A value-receiver
	// method value binds a copy of the receiver.
	PointerReceiver bool
	// Deref is set for (*T).M of a value-receiver method: the call copies
	// *arg0, and a nil pointer traps.
	Deref bool
	// Site is the first expression that makes the value.
	Site token.Pos
}

// FuncClass is one signature that the program calls through function
// values, with every target of an identical signature: the possible callees
// of such a call.
type FuncClass struct {
	Index     int
	Signature *types.Signature
	Targets   []*FuncTarget
	Site      token.Pos
}

// Closure is one function literal: its environment is the captured
// variables, in first-use order, which Check always moves to the heap.
type Closure struct {
	Lit    *ast.FuncLit
	Target *FuncTarget
	// Owner is the declared function (or instance) whose body contains the
	// literal; Parent is the enclosing literal, if any.
	Owner  *Function
	Parent *ast.FuncLit
	// Params are the literal's parameter objects (Info.Defs, or the
	// signature's variable for an unnamed parameter); Results are its named
	// result objects (nil when the results are unnamed).
	Params  []*types.Var
	Results []*types.Var
	// Captures are the variables declared outside the literal that its body
	// (including nested literals) uses. Each is in its owner's HeapVars.
	Captures []*types.Var
	// EnvBytes is the heap allocated each time the literal is evaluated: 0
	// without captures, else 8 bytes for the target ID plus 8 per capture.
	EnvBytes int64
	// Callees are the distinct program functions the literal's own body
	// (not nested literals) calls directly, in first call order.
	Callees []*types.Func
}

func (cl *Closure) addCallee(callee *types.Func) {
	for _, existing := range cl.Callees {
		if existing == callee {
			return
		}
	}
	cl.Callees = append(cl.Callees, callee)
}

// DeferKind classifies a deferred call.
type DeferKind int

const (
	// DeferFunction defers a call of a declared function.
	DeferFunction DeferKind = iota + 1
	// DeferMethod defers a static method call (x.M(...) or T.M(x, ...)).
	DeferMethod
	// DeferDynamic defers a method call on an interface value.
	DeferDynamic
	// DeferIndirect defers a call through a function value.
	DeferIndirect
	// DeferLiteral defers a call of a function literal written in place.
	DeferLiteral
	// DeferCopy defers the copy builtin.
	DeferCopy
)

// DeferSite is one defer statement.
type DeferSite struct {
	Stmt *ast.DeferStmt
	// Index is 1-based in program order.
	Index int
	Kind  DeferKind
	// Owner is the declared function containing the statement; Lit is the
	// literal whose body runs it (nil: Owner's own body).
	Owner *Function
	Lit   *ast.FuncLit
	// InLoop is set when the statement is inside a for or range loop of its
	// body: each execution allocates its record on the heap.
	InLoop bool
	// Saved are the types of the values the statement evaluates and saves,
	// in evaluation order: the function value (indirect and literal calls),
	// interface value (dynamic calls) or receiver (method values), then the
	// arguments.
	Saved []types.Type
	// RecordBytes is the heap a deferred call in a loop allocates per
	// execution: a 16-byte header and the saved values laid out as C (and
	// Go) lay out a struct, rounded up to 8.
	RecordBytes int64
}

// funcProgram is a checked program's function-value information.
type funcProgram struct {
	used       bool
	targets    []*FuncTarget
	byExpr     map[ast.Expr]*FuncTarget
	classes    []*FuncClass
	indirect   map[*ast.CallExpr]*FuncClass
	closures   []*Closure
	byLit      map[*ast.FuncLit]*Closure
	owned      map[*Function][]*Closure
	defers     []*DeferSite
	byDefer    map[*ast.DeferStmt]*DeferSite
	bodyDefers map[ast.Node][]*DeferSite // by *ast.FuncDecl or *ast.FuncLit
}

// CallTargets lists, in ID order, the functions a call through a function
// value may run: every target whose signature is identical to the called
// value's (the address-taken set of that signature). A literal called where
// it is written runs only that literal. It returns nil for any other call.
// The list is exactly the dispatcher the C backend emits for the call; a
// value holding none of them is nil, and the call aborts.
func (p *CheckedProgram) CallTargets(call *ast.CallExpr) []*FuncTarget {
	if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		if t := p.FuncValues[lit]; t != nil {
			return []*FuncTarget{t}
		}
		return nil
	}
	if class := p.IndirectCalls[call]; class != nil {
		return class.Targets
	}
	return nil
}

// isFuncValue reports whether t is a function type (a type parameter whose
// core type is one is not).
func isFuncValue(t types.Type) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(*types.TypeParam); ok {
		return false
	}
	_, ok := t.Underlying().(*types.Signature)
	return ok
}

// containsFuncValue reports whether values of t hold function values, or
// t is an interface whose methods pass or return them.
func containsFuncValue(t types.Type) bool { return containsFunc(t, map[types.Type]bool{}) }

func containsFunc(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Signature:
		return true
	case *types.Named:
		if pkg := t.Obj().Pkg(); pkg != nil && pkg.Path() == "gosvm/solana" {
			return false
		}
		return containsFunc(t.Underlying(), seen)
	case *types.Pointer:
		return containsFunc(t.Elem(), seen)
	case *types.Slice:
		return containsFunc(t.Elem(), seen)
	case *types.Array:
		return containsFunc(t.Elem(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsFunc(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if containsFunc(t.At(i).Type(), seen) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumMethods(); i++ {
			sig := t.Method(i).Type().(*types.Signature)
			if containsFunc(sig.Params(), seen) || containsFunc(sig.Results(), seen) {
				return true
			}
		}
	}
	return false
}

// funcTypeRule admits a function type: its parameters and results must be
// storable values, and it must not be variadic.
func (f *frontend) funcTypeRule(n ast.Node, sig *types.Signature) {
	if f.funcChecked == nil {
		f.funcChecked = map[*types.Signature]bool{}
	}
	if f.funcChecked[sig] {
		return
	}
	f.funcChecked[sig] = true
	if sig.Variadic() {
		f.fail(n, "variadic function types are unsupported")
	}
	for _, tuple := range []*types.Tuple{sig.Params(), sig.Results()} {
		for i := 0; i < tuple.Len(); i++ {
			t := tuple.At(i).Type()
			if isContext(t) {
				continue
			}
			f.typeArgRule(n, t)
		}
	}
}

// signatureOf is the signature of a function-typed expression, or nil.
func signatureOf(info *types.Info, e ast.Expr) *types.Signature {
	tv, ok := info.Types[e]
	if !ok || tv.Type == nil || tv.IsType() {
		return nil
	}
	sig, _ := tv.Type.Underlying().(*types.Signature)
	return sig
}

// funcBuilder discovers a program's function values, indirect calls,
// closures and defer statements.
type funcBuilder struct {
	f      *frontend
	info   *types.Info
	p      *funcProgram
	direct map[ast.Node]bool // callee expressions of direct calls
	skip   map[ast.Node]bool // parts of an expression already classified
	keyed  map[targetKey]*FuncTarget
}

type targetKey struct {
	kind  FuncTargetKind
	fn    *types.Func
	ptr   bool
	deref bool
}

// funcWalk is the context of a walked body.
type funcWalk struct {
	owner *Function
	lit   *ast.FuncLit
	loops int
}

// buildFuncValues derives the function-value information of the checked
// functions' bodies (lowered: generic code appears as its instances).
func buildFuncValues(f *frontend, functions []*Function, files []*ast.File) *funcProgram {
	p := &funcProgram{byExpr: map[ast.Expr]*FuncTarget{}, indirect: map[*ast.CallExpr]*FuncClass{}, byLit: map[*ast.FuncLit]*Closure{},
		owned: map[*Function][]*Closure{}, byDefer: map[*ast.DeferStmt]*DeferSite{}, bodyDefers: map[ast.Node][]*DeferSite{}}
	b := &funcBuilder{f: f, info: f.info, p: p, direct: map[ast.Node]bool{}, skip: map[ast.Node]bool{}, keyed: map[targetKey]*FuncTarget{}}
	for _, fn := range functions {
		if fn.Library != nil || fn.Decl.Body == nil {
			continue
		}
		b.walk(fn.Decl.Body, &funcWalk{owner: fn})
	}
	for _, c := range p.classes {
		for _, t := range p.targets {
			if types.Identical(t.Signature, c.Signature) {
				c.Targets = append(c.Targets, t)
			}
		}
	}
	p.used = len(p.targets) > 0 || len(p.indirect) > 0 || len(p.defers) > 0 || mentionsFuncValues(f.info, files)
	return p
}

func (b *funcBuilder) walk(root ast.Node, w *funcWalk) {
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || n == root {
			return true
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			b.literal(n, w)
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			inner := *w
			inner.loops++
			b.walk(n, &inner)
			return false
		case *ast.DeferStmt:
			b.deferSite(n, w)
		case *ast.CallExpr:
			b.call(n)
		case *ast.SelectorExpr:
			b.selector(n)
		case *ast.IndexExpr:
			b.instantiation(n, n.X)
		case *ast.IndexListExpr:
			b.instantiation(n, n.X)
		case *ast.Ident:
			b.ident(n)
		}
		return true
	})
}

// call marks a direct call's callee or records an indirect call's class.
func (b *funcBuilder) call(n *ast.CallExpr) {
	fun := n.Fun
	if tv := b.info.Types[fun]; tv.IsType() {
		b.direct[fun] = true
		return
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if s := b.info.Selections[sel]; s != nil && s.Kind() != types.FieldVal {
			b.direct[sel] = true
			return
		}
	}
	switch e := b.f.callFun(fun).(type) {
	case *ast.Ident:
		switch b.info.Uses[e].(type) {
		case *types.Func, *types.Builtin, *types.TypeName:
			b.direct[fun], b.direct[e] = true, true
			return
		}
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if _, ok := b.info.Uses[id].(*types.PkgName); ok {
				b.direct[fun], b.direct[e] = true, true
				return
			}
		}
	}
	if _, ok := ast.Unparen(fun).(*ast.FuncLit); ok {
		return // calls that literal directly
	}
	if sig := signatureOf(b.info, fun); sig != nil {
		b.p.indirect[n] = b.class(sig, n.Pos())
	}
}

func (b *funcBuilder) class(sig *types.Signature, site token.Pos) *FuncClass {
	for _, c := range b.p.classes {
		if types.Identical(c.Signature, sig) {
			return c
		}
	}
	c := &FuncClass{Index: len(b.p.classes), Signature: sig, Site: site}
	b.p.classes = append(b.p.classes, c)
	return c
}

// calleeFunc is the function an identifier names, resolved to its instance.
func (b *funcBuilder) calleeFunc(id *ast.Ident) *types.Func {
	fn, _ := b.f.callee(id).(*types.Func)
	return fn
}

// intrinsic reports a function without a Go body: gosvm/solana and math/bits.
func intrinsic(fn *types.Func) bool {
	return fn.Pkg() != nil && (fn.Pkg().Path() == "gosvm/solana" || fn.Pkg().Path() == bitsPath)
}

func (b *funcBuilder) selector(n *ast.SelectorExpr) {
	b.skip[n.Sel] = true
	if b.direct[n] || b.skip[n] {
		return
	}
	if s := b.info.Selections[n]; s != nil {
		fn := b.calleeFunc(n.Sel)
		if fn == nil {
			return
		}
		recv := fn.Type().(*types.Signature).Recv()
		if recv == nil {
			return
		}
		_, pointerRecv := recv.Type().(*types.Pointer)
		switch s.Kind() {
		case types.MethodVal:
			if !isIface(b.info.Types[n.X].Type) {
				b.target(n, TargetMethodValue, fn, pointerRecv, false)
			}
		case types.MethodExpr:
			if !isIface(s.Recv()) {
				_, pointerOperand := s.Recv().(*types.Pointer)
				b.target(n, TargetMethodExpr, fn, false, pointerOperand && !pointerRecv)
			}
		}
		return
	}
	if id, ok := n.X.(*ast.Ident); ok {
		if _, ok := b.info.Uses[id].(*types.PkgName); ok {
			if fn := b.calleeFunc(n.Sel); fn != nil && !intrinsic(fn) {
				b.target(n, TargetFunction, fn, false, false)
			}
		}
	}
}

// instantiation records F[T] (or pkg.F[T]) used as a value.
func (b *funcBuilder) instantiation(n ast.Expr, x ast.Expr) {
	id := callIdent(x)
	if id == nil {
		return
	}
	fn, ok := b.info.Uses[id].(*types.Func)
	if !ok || !genericFunc(fn.Origin()) {
		return
	}
	b.skip[x], b.skip[id] = true, true
	if b.direct[n] {
		return
	}
	if callee := b.calleeFunc(id); callee != nil {
		b.target(n, TargetFunction, callee, false, false)
	}
}

func (b *funcBuilder) ident(n *ast.Ident) {
	if b.skip[n] || b.direct[n] {
		return
	}
	fn, ok := b.info.Uses[n].(*types.Func)
	if !ok || fn.Type().(*types.Signature).Recv() != nil || intrinsic(fn) {
		return
	}
	if callee := b.calleeFunc(n); callee != nil {
		b.target(n, TargetFunction, callee, false, false)
	}
}

func (b *funcBuilder) target(e ast.Expr, kind FuncTargetKind, fn *types.Func, pointerRecv, deref bool) {
	sig := signatureOf(b.info, e)
	if sig == nil {
		return
	}
	key := targetKey{kind, fn, pointerRecv, deref}
	t := b.keyed[key]
	if t == nil {
		t = &FuncTarget{ID: uint64(len(b.p.targets) + 1), Kind: kind, Func: fn, Signature: sig, PointerReceiver: pointerRecv, Deref: deref, Site: e.Pos()}
		b.keyed[key] = t
		b.p.targets = append(b.p.targets, t)
	}
	b.p.byExpr[e] = t
}

// literal records a function literal and walks its body.
func (b *funcBuilder) literal(lit *ast.FuncLit, w *funcWalk) {
	sig := signatureOf(b.info, lit)
	if sig == nil {
		return
	}
	t := &FuncTarget{ID: uint64(len(b.p.targets) + 1), Kind: TargetLiteral, Lit: lit, Signature: sig, Site: lit.Pos()}
	b.p.targets = append(b.p.targets, t)
	b.p.byExpr[lit] = t
	cl := &Closure{Lit: lit, Target: t, Owner: w.owner, Parent: w.lit, Captures: literalCaptures(b.info, lit)}
	cl.Params, cl.Results = literalVars(b.info, lit, sig)
	if len(cl.Captures) > 0 {
		cl.EnvBytes = 8 + 8*int64(len(cl.Captures))
	}
	b.p.closures = append(b.p.closures, cl)
	b.p.byLit[lit] = cl
	b.p.owned[w.owner] = append(b.p.owned[w.owner], cl)
	b.walk(lit.Body, &funcWalk{owner: w.owner, lit: lit})
}

// literalVars are a literal's parameter objects and named result objects.
func literalVars(info *types.Info, lit *ast.FuncLit, sig *types.Signature) (params, results []*types.Var) {
	collect := func(list *ast.FieldList, tuple *types.Tuple, named bool) []*types.Var {
		var out []*types.Var
		if list == nil {
			return nil
		}
		i := 0
		for _, field := range list.List {
			if len(field.Names) == 0 {
				if named {
					return nil
				}
				if i < tuple.Len() {
					out = append(out, tuple.At(i))
				}
				i++
				continue
			}
			for _, name := range field.Names {
				v, _ := info.Defs[name].(*types.Var)
				if v == nil && i < tuple.Len() {
					v = tuple.At(i)
				}
				out = append(out, v)
				i++
			}
		}
		return out
	}
	return collect(lit.Type.Params, sig.Params(), false), collect(lit.Type.Results, sig.Results(), true)
}

// literalCaptures lists, in first-use order, the variables declared outside
// lit that its body uses (nested literals included).
func literalCaptures(info *types.Info, lit *ast.FuncLit) []*types.Var {
	scope := info.Scopes[lit.Type]
	seen := map[*types.Var]bool{}
	var out []*types.Var
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := info.Uses[id].(*types.Var)
		if !ok || v.IsField() || seen[v] || v.Parent() == nil || v.Parent() == types.Universe {
			return true
		}
		if v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			return true
		}
		if scope != nil && scopeWithin(v.Parent(), scope) {
			return true
		}
		seen[v] = true
		out = append(out, v)
		return true
	})
	return out
}

func (b *funcBuilder) deferSite(n *ast.DeferStmt, w *funcWalk) {
	kind, saved := b.deferKind(n.Call)
	site := &DeferSite{Stmt: n, Index: len(b.p.defers) + 1, Kind: kind, Owner: w.owner, Lit: w.lit, InLoop: w.loops > 0, Saved: saved}
	site.RecordBytes = recordBytes(saved)
	b.p.defers = append(b.p.defers, site)
	b.p.byDefer[n] = site
	var body ast.Node = w.owner.Decl
	if w.lit != nil {
		body = w.lit
	}
	b.p.bodyDefers[body] = append(b.p.bodyDefers[body], site)
}

func tupleList(t *types.Tuple) []types.Type {
	if t == nil {
		return nil
	}
	return tupleTypes(t)
}

// deferKind classifies a deferred call and lists the types it saves.
func (b *funcBuilder) deferKind(call *ast.CallExpr) (DeferKind, []types.Type) {
	fun := call.Fun
	if _, ok := ast.Unparen(fun).(*ast.FuncLit); ok {
		sig := signatureOf(b.info, fun)
		if sig == nil {
			return DeferLiteral, nil
		}
		return DeferLiteral, append([]types.Type{b.info.Types[fun].Type}, tupleList(sig.Params())...)
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if s := b.info.Selections[sel]; s != nil && s.Kind() != types.FieldVal {
			sig := signatureOf(b.info, sel)
			if sig == nil {
				return DeferMethod, nil
			}
			if s.Kind() == types.MethodVal && isIface(b.info.Types[sel.X].Type) {
				return DeferDynamic, append([]types.Type{b.info.Types[sel.X].Type}, tupleList(sig.Params())...)
			}
			if s.Kind() == types.MethodExpr {
				return DeferMethod, tupleList(sig.Params())
			}
			fn := b.calleeFunc(sel.Sel)
			if fn == nil {
				return DeferMethod, nil
			}
			msig := fn.Type().(*types.Signature)
			return DeferMethod, append([]types.Type{msig.Recv().Type()}, tupleList(msig.Params())...)
		}
	}
	switch e := b.f.callFun(fun).(type) {
	case *ast.Ident:
		switch o := b.info.Uses[e].(type) {
		case *types.Builtin:
			if len(call.Args) == 2 {
				t := b.info.Types[call.Args[0]].Type
				return DeferCopy, []types.Type{t, t}
			}
			return DeferCopy, nil
		case *types.Func:
			_ = o
			if fn := b.calleeFunc(e); fn != nil {
				return DeferFunction, tupleList(fn.Type().(*types.Signature).Params())
			}
			return DeferFunction, nil
		}
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if _, ok := b.info.Uses[id].(*types.PkgName); ok {
				if fn := b.calleeFunc(e.Sel); fn != nil {
					return DeferFunction, tupleList(fn.Type().(*types.Signature).Params())
				}
				return DeferFunction, nil
			}
		}
	}
	sig := signatureOf(b.info, fun)
	if sig == nil {
		return DeferIndirect, nil
	}
	return DeferIndirect, append([]types.Type{b.info.Types[fun].Type}, tupleList(sig.Params())...)
}

// cSizes are the C (and, for admitted types other than zero-size ones, Go)
// sizes and alignments of values: a zero-size struct or array occupies one
// byte in C.
func cSizeof(t types.Type) int64 {
	sizes := &types.StdSizes{WordSize: 8, MaxAlign: 8}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		if u.NumFields() == 0 {
			return 1
		}
		offset, align := int64(0), int64(1)
		for i := 0; i < u.NumFields(); i++ {
			ft := u.Field(i).Type()
			a := sizes.Alignof(ft)
			if a > align {
				align = a
			}
			offset = (offset+a-1)/a*a + cSizeof(ft)
		}
		return (offset + align - 1) / align * align
	case *types.Array:
		n := u.Len()
		if n == 0 {
			n = 1
		}
		return n * cSizeof(u.Elem())
	}
	if isContext(t) {
		return 8
	}
	return sizes.Sizeof(t)
}

// recordBytes is the heap size of a deferred call's record: the 16-byte
// list header, then the saved values, rounded up to 8.
func recordBytes(saved []types.Type) int64 {
	sizes := &types.StdSizes{WordSize: 8, MaxAlign: 8}
	offset := int64(16)
	for _, t := range saved {
		a := int64(8)
		if !isContext(t) {
			a = sizes.Alignof(t)
		}
		offset = (offset+a-1)/a*a + cSizeof(t)
	}
	return (offset + 7) &^ 7
}

// mentionsFuncValues reports whether any declared object, signature or
// value expression in files has a type that holds function values. Names of
// functions in direct calls do not count.
func mentionsFuncValues(info *types.Info, files []*ast.File) bool {
	found := false
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if found || n == nil {
				return false
			}
			switch n := n.(type) {
			case *ast.Ident:
				switch o := info.Defs[n].(type) {
				case *types.Var:
					found = containsFuncValue(o.Type())
				case *types.TypeName:
					found = containsFuncValue(o.Type())
				case *types.Func:
					sig := o.Type().(*types.Signature)
					found = containsFuncValue(sig.Params()) || containsFuncValue(sig.Results()) || sig.Recv() != nil && containsFuncValue(sig.Recv().Type())
				}
				if found {
					return false
				}
				switch info.Uses[n].(type) {
				case *types.Func, *types.Builtin:
					return false
				}
			case *ast.SelectorExpr:
				switch info.Uses[n.Sel].(type) {
				case *types.Func, *types.Builtin:
					return true // the operand may hold values; the selector names a function
				}
			case *ast.IndexExpr:
				if id := callIdent(n.X); id != nil {
					if fn, ok := info.Uses[id].(*types.Func); ok && genericFunc(fn.Origin()) {
						return true
					}
				}
			case *ast.IndexListExpr:
				return true
			}
			if e, ok := n.(ast.Expr); ok {
				if _, isSel := n.(*ast.SelectorExpr); isSel {
					if _, named := info.Uses[n.(*ast.SelectorExpr).Sel].(*types.Func); named {
						return true
					}
				}
				if tv, ok := info.Types[e]; ok && !tv.IsType() && containsFuncValue(tv.Type) {
					found = true
				}
			}
			return !found
		})
		if found {
			return true
		}
	}
	return found
}
