package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
)

// Generics are monomorphized after Go type checking: every distinct
// instantiation of a generic function, method or type becomes an ordinary,
// non-generic declaration that the subset rules, the borrow checker and the
// emitter treat like hand-written code. See docs/GO_GENERICS.md.

// MaxInstantiations bounds the distinct instantiations of one generic
// function, method or type, to keep monomorphized code size predictable.
const MaxInstantiations = 64

// Instance is one instantiation of a generic function or of a method of a
// generic type, lowered to a synthetic non-generic declaration.
type Instance struct {
	// Origin is the generic declaration's object: a function with type
	// parameters, or a method whose receiver type has them.
	Origin *types.Func
	// TypeArgs are the concrete type arguments; for methods, the receiver
	// type's arguments. Instantiated generic types in them are canonical
	// (pointer-identical to every other use of the same instance).
	TypeArgs []types.Type
	// Object is the instance's own function. Its signature is Origin's with
	// the type arguments substituted, its parameters are the objects declared
	// in Decl, and it has no type parameters. Its Name is Name.
	Object *types.Func
	// Decl is a deep copy of Origin's declaration without type parameters.
	// Every node in it has Info entries (Types, Defs, Uses, Scopes and, for
	// selectors, Selections) with the type arguments substituted; see
	// docs/BACKEND_INTERFACE.md for the exceptions.
	Decl *ast.FuncDecl
	// Key is the canonical, package-path-qualified instantiation, for example
	// "example.org/app.Min[uint64]" or "example.org/app.Pair[uint8, bool].Swap"
	// (type arguments as go/types prints them). It depends only on the
	// generic declaration and the type arguments.
	Key string
	// Name is Key relative to Origin's package ("Min[uint64]").
	Name string
	// Symbol is the C function name the built-in backend uses, derived
	// injectively from Key.
	Symbol string
	// Site is the first call that requires the instance, in discovery order.
	Site token.Pos
}

// TypeInstance is one instantiated generic type used by checked code.
type TypeInstance struct {
	// Type is the canonical instance; its Underlying() has the type
	// arguments substituted, and Obj() is the generic declaration's name.
	Type *types.Named
	// Key, Name and Symbol are as for Instance ("example.org/app.Stack[uint8]").
	Key, Name, Symbol string
	// Site is the first use that requires the instance, in discovery order.
	Site token.Pos
}

// posNode positions a diagnostic without a syntax node.
type posNode token.Pos

func (p posNode) Pos() token.Pos { return token.Pos(p) }
func (p posNode) End() token.Pos { return token.Pos(p) }

// genericProgram discovers and builds every instantiation reachable from the
// non-generic declarations, writing the synthetic declarations' entries into
// info.
type genericProgram struct {
	f        *frontend
	info     *types.Info
	ctxt     *types.Context
	validate bool // apply subset rules and the limit (Check); features only discovers

	decls map[*types.Func]*ast.FuncDecl // generic function and method declarations

	instances     map[string]*Instance
	typeInstances map[string]*TypeInstance
	named         map[string]*types.Named // canonical instances by key
	counts        map[types.Object]int
	queue         []*Instance

	instantiated map[*ast.Ident]*Instance
	instanceOf   map[*ast.FuncDecl]*Instance
	copyArgs     map[*ast.Ident][]types.Type // substituted Info.Instances type arguments in copies
	wrapped      map[compileError]bool
}

func newGenericProgram(f *frontend, info *types.Info, ctxt *types.Context, decls []ast.Decl, validate bool) *genericProgram {
	gp := &genericProgram{f: f, info: info, ctxt: ctxt, validate: validate,
		decls:     map[*types.Func]*ast.FuncDecl{},
		instances: map[string]*Instance{}, typeInstances: map[string]*TypeInstance{}, named: map[string]*types.Named{},
		counts: map[types.Object]int{}, instantiated: map[*ast.Ident]*Instance{}, instanceOf: map[*ast.FuncDecl]*Instance{},
		copyArgs: map[*ast.Ident][]types.Type{}, wrapped: map[compileError]bool{}}
	for _, d := range decls {
		if d, ok := d.(*ast.FuncDecl); ok {
			if fn, ok := info.Defs[d.Name].(*types.Func); ok && genericFunc(fn) {
				gp.decls[fn] = d
			}
		}
	}
	return gp
}

// genericFunc reports whether fn is declared with type parameters, directly
// or through its receiver type.
func genericFunc(fn *types.Func) bool {
	sig := fn.Type().(*types.Signature)
	return sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0
}

// constraintSpec reports whether ts declares an interface, which the subset
// admits only as a type constraint.
func constraintSpec(info *types.Info, ts *ast.TypeSpec) bool {
	if ts.Assign.IsValid() || ts.TypeParams != nil {
		return false
	}
	tn, ok := info.Defs[ts.Name].(*types.TypeName)
	if !ok {
		return false
	}
	_, ok = tn.Type().Underlying().(*types.Interface)
	return ok
}

func (gp *genericProgram) genericSpec(ts *ast.TypeSpec) bool {
	return ts.TypeParams != nil || constraintSpec(gp.info, ts)
}

// run discovers instantiations from the non-generic declarations, in file
// and source order, then from each new instance body in discovery order.
func (gp *genericProgram) run(files []*ast.File) {
	for _, file := range files {
		for _, d := range file.Decls {
			if gp.skipped(d) {
				continue
			}
			gp.discover(d)
		}
	}
	gp.drain()
}

// drain discovers the instantiations that queued instance bodies need.
func (gp *genericProgram) drain() {
	for len(gp.queue) > 0 {
		inst := gp.queue[0]
		gp.queue = gp.queue[1:]
		gp.discover(inst.Decl)
	}
}

// skipped reports a generic declaration (it has no code of its own).
func (gp *genericProgram) skipped(d ast.Decl) bool {
	if fn, ok := d.(*ast.FuncDecl); ok {
		object, _ := gp.info.Defs[fn.Name].(*types.Func)
		return object != nil && gp.decls[object] != nil
	}
	return false
}

// discover requests the instances that the calls and types of root need.
func (gp *genericProgram) discover(root ast.Node) {
	ast.Inspect(root, func(n ast.Node) bool {
		if gen, ok := n.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			for _, spec := range gen.Specs {
				if ts := spec.(*ast.TypeSpec); !gp.genericSpec(ts) {
					ast.Inspect(ts, func(n ast.Node) bool { gp.nodeTypes(n); return true })
				}
			}
			return false
		}
		gp.nodeTypes(n)
		if id, ok := n.(*ast.Ident); ok {
			// A generic function or method used as a value (F[T], x.M, T.M)
			// runs the instance its type arguments select; calls were
			// resolved at their call expression, visited first.
			if fn, ok := gp.info.Uses[id].(*types.Func); ok && gp.decls[fn.Origin()] != nil && gp.instantiated[id] == nil {
				if args, ok := gp.instanceArgs(fn, id); ok {
					gp.request(fn.Origin(), args, id)
				}
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id := callIdent(gp.f.callFun(call.Fun))
		if id == nil {
			return true
		}
		fn, ok := gp.info.Uses[id].(*types.Func)
		if !ok {
			return true
		}
		origin := fn.Origin()
		if gp.decls[origin] == nil {
			return true
		}
		var args []types.Type
		if origin.Type().(*types.Signature).RecvTypeParams().Len() > 0 {
			recv := fn.Type().(*types.Signature).Recv().Type()
			if p, ok := recv.(*types.Pointer); ok {
				recv = p.Elem()
			}
			named, ok := recv.(*types.Named)
			if !ok || containsTypeParam(named) {
				gp.f.fail(call, "internal error: uninstantiated generic method receiver")
			}
			args = typeList(named.TypeArgs())
		} else if copied, ok := gp.copyArgs[id]; ok {
			args = copied
		} else if instance, ok := gp.info.Instances[id]; ok {
			args = typeList(instance.TypeArgs)
		} else {
			gp.f.fail(call, "internal error: generic call without type arguments")
		}
		gp.request(origin, args, id)
		return true
	})
}

// instanceArgs are the type arguments of a generic function or method named
// by id outside a call; false when they are not concrete.
func (gp *genericProgram) instanceArgs(fn *types.Func, id *ast.Ident) ([]types.Type, bool) {
	if fn.Origin().Type().(*types.Signature).RecvTypeParams().Len() > 0 {
		recv := fn.Type().(*types.Signature).Recv().Type()
		if p, ok := recv.(*types.Pointer); ok {
			recv = p.Elem()
		}
		named, ok := recv.(*types.Named)
		if !ok || containsTypeParam(named) {
			return nil, false
		}
		return typeList(named.TypeArgs()), true
	}
	if copied, ok := gp.copyArgs[id]; ok {
		return copied, true
	}
	if instance, ok := gp.info.Instances[id]; ok {
		args := typeList(instance.TypeArgs)
		for _, a := range args {
			if containsTypeParam(a) {
				return nil, false
			}
		}
		return args, true
	}
	return nil, false
}

// nodeTypes records the type instances that a node's type or definition uses.
func (gp *genericProgram) nodeTypes(n ast.Node) {
	if e, ok := n.(ast.Expr); ok {
		if tv, ok := gp.info.Types[e]; ok {
			gp.collect(tv.Type, n, map[types.Type]bool{})
		}
	}
	if id, ok := n.(*ast.Ident); ok {
		if o := gp.info.Defs[id]; o != nil {
			gp.collect(o.Type(), n, map[types.Type]bool{})
		}
	}
}

// collect walks t for instantiated generic types and requests them.
func (gp *genericProgram) collect(t types.Type, site ast.Node, seen map[types.Type]bool) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Named:
		if t.TypeArgs().Len() > 0 {
			if containsTypeParam(t) {
				return
			}
			gp.requestType(t, site)
			return
		}
		if t.TypeParams().Len() > 0 {
			return
		}
		switch u := t.Underlying().(type) {
		case *types.Struct, *types.Interface:
			gp.collect(u, site, seen)
		}
	case *types.Pointer:
		gp.collect(t.Elem(), site, seen)
	case *types.Slice:
		gp.collect(t.Elem(), site, seen)
	case *types.Array:
		gp.collect(t.Elem(), site, seen)
	case *types.Map:
		gp.collect(t.Key(), site, seen)
		gp.collect(t.Elem(), site, seen)
	case *types.Chan:
		gp.collect(t.Elem(), site, seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			gp.collect(t.Field(i).Type(), site, seen)
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			gp.collect(t.At(i).Type(), site, seen)
		}
	case *types.Interface:
		// Dynamic calls pass and return the methods' values.
		for i := 0; i < t.NumMethods(); i++ {
			sig := t.Method(i).Type().(*types.Signature)
			gp.collect(sig.Params(), site, seen)
			gp.collect(sig.Results(), site, seen)
		}
	case *types.Signature:
		if t.TypeParams().Len() > 0 || t.RecvTypeParams().Len() > 0 {
			return
		}
		if t.Recv() != nil {
			gp.collect(t.Recv().Type(), site, seen)
		}
		gp.collect(t.Params(), site, seen)
		gp.collect(t.Results(), site, seen)
	}
}

// requestType registers a concrete instance of a generic type.
func (gp *genericProgram) requestType(t *types.Named, site ast.Node) {
	t = gp.canonicalNamed(t)
	key := typeKey(t)
	if gp.typeInstances[key] != nil {
		return
	}
	origin := t.Origin().Obj()
	gp.counts[origin]++
	if gp.counts[origin] > MaxInstantiations {
		if !gp.validate {
			return
		}
		gp.f.fail(site, "too many instantiations of %s (limit %d)", origin.Name(), MaxInstantiations)
	}
	name := relativeTypeString(t, origin.Pkg())
	if gp.validate {
		for i := 0; i < t.TypeArgs().Len(); i++ {
			gp.checkTypeArg(site, name, t.TypeParams().At(i), t.TypeArgs().At(i))
		}
	}
	gp.typeInstances[key] = &TypeInstance{Type: t, Key: key, Name: name, Symbol: "gosvm_ty_" + mangle(key), Site: site.Pos()}
	switch u := t.Underlying().(type) {
	case *types.Struct, *types.Interface:
		gp.collect(u, site, map[types.Type]bool{})
	}
}

// request records the instance of origin for args that the call at site
// runs, building it on first use.
func (gp *genericProgram) request(origin *types.Func, args []types.Type, site *ast.Ident) {
	gp.requestAt(origin, args, site, site)
}

// requestAt is request with a diagnostic position site; id, when not nil, is
// the call identifier that runs the instance (CheckedProgram.Instantiated).
// It returns the instance, or nil past the limit when not validating.
func (gp *genericProgram) requestAt(origin *types.Func, args []types.Type, site ast.Node, id *ast.Ident) *Instance {
	for i, arg := range args {
		args[i] = gp.canonical(arg)
	}
	key, name := instanceKey(origin, args)
	if inst := gp.instances[key]; inst != nil {
		if id != nil {
			gp.instantiated[id] = inst
		}
		return inst
	}
	gp.counts[origin]++
	if gp.counts[origin] > MaxInstantiations {
		if !gp.validate {
			return nil
		}
		display := origin.Name()
		if recv := origin.Type().(*types.Signature).Recv(); recv != nil {
			display = recvBase(recv.Type()).Obj().Name() + "." + display
		}
		gp.f.fail(site, "too many instantiations of %s (limit %d)", display, MaxInstantiations)
	}
	tparams := genericParams(origin)
	if tparams.Len() != len(args) {
		gp.f.fail(site, "internal error: %s has %d type arguments", name, len(args))
	}
	if gp.validate {
		for i, arg := range args {
			gp.checkTypeArg(site, name, tparams.At(i), arg)
		}
	}
	inst := &Instance{Origin: origin, TypeArgs: args, Key: key, Name: name, Symbol: "gosvm_fn_" + mangle(key), Site: site.Pos()}
	gp.instances[key] = inst
	if id != nil {
		gp.instantiated[id] = inst
	}
	gp.build(inst)
	gp.queue = append(gp.queue, inst)
	return inst
}

func genericParams(origin *types.Func) *types.TypeParamList {
	sig := origin.Type().(*types.Signature)
	if sig.RecvTypeParams().Len() > 0 {
		return sig.RecvTypeParams()
	}
	return sig.TypeParams()
}

// checkTypeArg applies the subset's storage rules to one type argument at
// the instantiation site.
func (gp *genericProgram) checkTypeArg(site ast.Node, name string, param *types.TypeParam, arg types.Type) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(compileError)
			if !ok {
				panic(r)
			}
			message := strings.TrimPrefix(string(e), "-: ")
			gp.f.fail(site, "instantiation %s: type argument %s for %s: %s", name, relativeTypeString(arg, param.Obj().Pkg()), param.Obj().Name(), message)
		}
	}()
	gp.f.typeArgRule(posNode(token.NoPos), arg)
}

// wrap runs check and adds instantiation context to a diagnostic raised in
// an instance (nil: none), once: the error then names the instantiation site
// and the instance, followed by the original position and message.
func (gp *genericProgram) wrap(inst *Instance, check func()) {
	defer gp.rethrow(inst.context())
	check()
}

// wrapType is wrap for an instantiated generic type.
func (gp *genericProgram) wrapType(ti *TypeInstance, check func()) {
	defer gp.rethrow(ti.Site, ti.Name)
	check()
}

// rethrow is wrap's deferred half; deferred directly (defer
// gp.rethrow(inst.context())) it wraps what the rest of the function raises.
func (gp *genericProgram) rethrow(site token.Pos, name string) {
	if name == "" {
		return
	}
	if r := recover(); r != nil {
		e, ok := r.(compileError)
		if !ok || gp.wrapped[e] {
			panic(r)
		}
		w := compileError(fmt.Sprintf("%s: instantiation %s: %s", gp.f.fset.Position(site), name, e))
		gp.wrapped[w] = true
		panic(w)
	}
}

// context is the instantiation's site and name; none for nil.
func (inst *Instance) context() (token.Pos, string) {
	if inst == nil {
		return token.NoPos, ""
	}
	return inst.Site, inst.Name
}

// lowered returns files with every generic declaration replaced by its
// instances (sorted by key) and constraint interfaces removed. A file without
// generic declarations is returned unchanged, so programs without generics
// are lowered exactly as before.
func (gp *genericProgram) lowered(files []*ast.File) []*ast.File {
	byOrigin := map[*types.Func][]*Instance{}
	for _, inst := range gp.instances {
		byOrigin[inst.Origin] = append(byOrigin[inst.Origin], inst)
	}
	out := make([]*ast.File, len(files))
	for i, file := range files {
		changed := false
		var decls []ast.Decl
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if object, _ := gp.info.Defs[d.Name].(*types.Func); object != nil && gp.decls[object] != nil {
					changed = true
					instances := byOrigin[object]
					sort.Slice(instances, func(i, j int) bool { return instances[i].Key < instances[j].Key })
					for _, inst := range instances {
						decls = append(decls, inst.Decl)
					}
					continue
				}
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					var specs []ast.Spec
					for _, spec := range d.Specs {
						if !gp.genericSpec(spec.(*ast.TypeSpec)) {
							specs = append(specs, spec)
						}
					}
					if len(specs) != len(d.Specs) {
						changed = true
						if len(specs) > 0 {
							copied := *d
							copied.Specs = specs
							decls = append(decls, &copied)
						}
						continue
					}
				}
			}
			decls = append(decls, d)
		}
		out[i] = file
		if changed {
			copied := *file
			copied.Decls = decls
			out[i] = &copied
		}
	}
	return out
}

// sortedInstances lists instances by generic declaration order, then key.
func (gp *genericProgram) sortedInstances(files []*ast.File) []*Instance {
	var out []*Instance
	for _, file := range gp.lowered(files) {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				if inst := gp.instanceOf[fn]; inst != nil {
					out = append(out, inst)
				}
			}
		}
	}
	return out
}

func (gp *genericProgram) sortedTypeInstances() []*TypeInstance {
	out := make([]*TypeInstance, 0, len(gp.typeInstances))
	for _, ti := range gp.typeInstances {
		out = append(out, ti)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// build makes inst.Decl and inst.Object and records the copy's Info.
func (gp *genericProgram) build(inst *Instance) {
	decl := gp.decls[inst.Origin]
	s := &substituter{gp: gp, params: genericParams(inst.Origin), args: inst.TypeArgs}
	pairs := map[ast.Node]ast.Node{}
	copied := copyAST(decl, pairs).(*ast.FuncDecl)
	copied.Type.TypeParams = nil
	original := map[ast.Node]ast.Node{}
	for old, n := range pairs {
		original[n] = old
	}
	var nodes []ast.Node // the copy, in source order, without type parameters
	ast.Inspect(copied, func(n ast.Node) bool {
		if n != nil {
			nodes = append(nodes, n)
		}
		return true
	})
	info := gp.info
	// Scopes mirror the generic declaration's; the function scope is a root.
	scopes := map[*types.Scope]*types.Scope{}
	for _, n := range nodes {
		if scope := info.Scopes[original[n]]; scope != nil {
			mirror := types.NewScope(scopes[scope.Parent()], scope.Pos(), scope.End(), "instance")
			scopes[scope] = mirror
			info.Scopes[n] = mirror
		}
	}
	// Declared objects get the substituted types.
	objects := map[types.Object]types.Object{}
	sig := inst.Origin.Type().(*types.Signature)
	signatureVars := map[types.Object]bool{}
	for _, tuple := range []*types.Tuple{sig.Params(), sig.Results()} {
		for i := 0; i < tuple.Len(); i++ {
			signatureVars[tuple.At(i)] = true
		}
	}
	if sig.Recv() != nil {
		signatureVars[sig.Recv()] = true
	}
	for _, n := range nodes {
		id, ok := n.(*ast.Ident)
		if !ok || id == copied.Name {
			continue
		}
		var object types.Object
		switch o := info.Defs[original[n].(*ast.Ident)].(type) {
		case nil:
			continue
		case *types.Var:
			if o.IsField() {
				object = o // fields of anonymous structs, which the subset rejects
				break
			}
			if signatureVars[o] {
				object = types.NewParam(o.Pos(), o.Pkg(), o.Name(), s.typ(o.Type()))
			} else {
				object = types.NewVar(o.Pos(), o.Pkg(), o.Name(), s.typ(o.Type()))
			}
			objects[o] = object
			if scope := scopes[o.Parent()]; scope != nil {
				scope.Insert(object)
			}
		case *types.Const:
			object = types.NewConst(o.Pos(), o.Pkg(), o.Name(), s.typ(o.Type()), o.Val())
			objects[o] = object
			if scope := scopes[o.Parent()]; scope != nil {
				scope.Insert(object)
			}
		default:
			object = o // receiver type parameters and other non-value names
		}
		info.Defs[id] = object
	}
	// A type switch clause's implicit variable gets the substituted type.
	for _, n := range nodes {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			continue
		}
		o, ok := info.Implicits[original[n]].(*types.Var)
		if !ok {
			continue
		}
		object := types.NewVar(o.Pos(), o.Pkg(), o.Name(), s.typ(o.Type()))
		objects[o] = object
		if scope := scopes[o.Parent()]; scope != nil {
			scope.Insert(object)
		}
		info.Implicits[clause] = object
	}
	variable := func(v *types.Var) *types.Var {
		if v == nil {
			return nil
		}
		if mapped, ok := objects[v]; ok {
			return mapped.(*types.Var)
		}
		mapped := types.NewParam(v.Pos(), v.Pkg(), v.Name(), s.typ(v.Type()))
		objects[v] = mapped
		return mapped
	}
	tuple := func(t *types.Tuple) *types.Tuple {
		vars := make([]*types.Var, t.Len())
		for i := range vars {
			vars[i] = variable(t.At(i))
		}
		return types.NewTuple(vars...)
	}
	instanceSig := types.NewSignatureType(variable(sig.Recv()), nil, nil, tuple(sig.Params()), tuple(sig.Results()), sig.Variadic())
	inst.Object = types.NewFunc(inst.Origin.Pos(), inst.Origin.Pkg(), inst.Name, instanceSig)
	inst.Decl = copied
	info.Defs[copied.Name] = inst.Object
	gp.instanceOf[copied] = inst
	// Expression types.
	for _, n := range nodes {
		if e, ok := n.(ast.Expr); ok {
			if tv, ok := info.Types[original[n].(ast.Expr)]; ok {
				tv.Type = s.typ(tv.Type)
				info.Types[e] = tv
			}
		}
	}
	// Fields and methods are looked up again in the substituted types.
	for _, n := range nodes {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			selection := info.Selections[original[n].(*ast.SelectorExpr)]
			if selection == nil {
				continue
			}
			info.Selections[e] = selection
			object, _, _ := types.LookupFieldOrMethod(info.Types[e.X].Type, true, selection.Obj().Pkg(), e.Sel.Name)
			if object == nil {
				gp.f.fail(e, "internal error: %s not found in instance %s", e.Sel.Name, inst.Name)
			}
			info.Uses[e.Sel] = object
		case *ast.CompositeLit:
			tv, ok := info.Types[e]
			if !ok {
				continue
			}
			st, ok := tv.Type.Underlying().(*types.Struct)
			if !ok {
				continue
			}
			for _, elt := range e.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						for i := 0; i < st.NumFields(); i++ {
							if st.Field(i).Name() == key.Name {
								info.Uses[key] = st.Field(i)
							}
						}
					}
				}
			}
		}
	}
	for _, n := range nodes {
		id, ok := n.(*ast.Ident)
		if !ok {
			continue
		}
		old := original[n].(*ast.Ident)
		if instance, ok := info.Instances[old]; ok {
			args := typeList(instance.TypeArgs)
			for i, arg := range args {
				args[i] = s.typ(arg)
			}
			gp.copyArgs[id] = args
		}
		if _, done := info.Uses[id]; done {
			continue
		}
		o := info.Uses[old]
		if o == nil {
			continue
		}
		if mapped, ok := objects[o]; ok {
			o = mapped
		}
		info.Uses[id] = o
	}
}

// substituter replaces a generic declaration's type parameters with type
// arguments. Unchanged types keep their identity; instantiated generic types
// are canonical.
type substituter struct {
	gp     *genericProgram
	params *types.TypeParamList
	args   []types.Type
}

func (gp *genericProgram) canonical(t types.Type) types.Type {
	return (&substituter{gp: gp}).typ(t)
}

func (s *substituter) typ(t types.Type) types.Type {
	switch t := t.(type) {
	case *types.TypeParam:
		if s.params != nil && t.Index() < s.params.Len() && s.params.At(t.Index()) == t {
			return s.args[t.Index()]
		}
		panic(compileError("internal error: type parameter " + t.Obj().Name() + " outside its declaration"))
	case *types.Pointer:
		if e := s.typ(t.Elem()); e != t.Elem() {
			return types.NewPointer(e)
		}
	case *types.Slice:
		if e := s.typ(t.Elem()); e != t.Elem() {
			return types.NewSlice(e)
		}
	case *types.Array:
		if e := s.typ(t.Elem()); e != t.Elem() {
			return types.NewArray(e, t.Len())
		}
	case *types.Map:
		if k, e := s.typ(t.Key()), s.typ(t.Elem()); k != t.Key() || e != t.Elem() {
			return types.NewMap(k, e)
		}
	case *types.Chan:
		if e := s.typ(t.Elem()); e != t.Elem() {
			return types.NewChan(t.Dir(), e)
		}
	case *types.Tuple:
		if t == nil {
			return t
		}
		if vars, changed := s.vars(t); changed {
			return types.NewTuple(vars...)
		}
	case *types.Signature:
		if t.TypeParams().Len() > 0 || t.RecvTypeParams().Len() > 0 {
			return t
		}
		params, p := s.vars(t.Params())
		results, r := s.vars(t.Results())
		var recv *types.Var
		changed := p || r
		if t.Recv() != nil {
			recv = t.Recv()
			if rt := s.typ(recv.Type()); rt != recv.Type() {
				recv = types.NewParam(recv.Pos(), recv.Pkg(), recv.Name(), rt)
				changed = true
			}
		}
		if changed {
			return types.NewSignatureType(recv, nil, nil, types.NewTuple(params...), types.NewTuple(results...), t.Variadic())
		}
	case *types.Struct:
		fields := make([]*types.Var, t.NumFields())
		tags := make([]string, t.NumFields())
		changed := false
		for i := range fields {
			f := t.Field(i)
			fields[i], tags[i] = f, t.Tag(i)
			if ft := s.typ(f.Type()); ft != f.Type() {
				fields[i] = types.NewField(f.Pos(), f.Pkg(), f.Name(), ft, f.Embedded())
				changed = true
			}
		}
		if changed {
			return types.NewStruct(fields, tags)
		}
	case *types.Named:
		if t.TypeArgs().Len() == 0 {
			return t
		}
		args := typeList(t.TypeArgs())
		changed := false
		for i, arg := range args {
			if a := s.typ(arg); a != arg {
				args[i], changed = a, true
			}
		}
		if containsTypeParam(t) && !changed {
			return t
		}
		if !changed {
			return s.gp.canonicalNamed(t)
		}
		instance, err := types.Instantiate(s.gp.ctxt, t.Origin(), args, false)
		if err != nil {
			panic(compileError("internal error: " + err.Error()))
		}
		return s.gp.canonicalNamed(instance.(*types.Named))
	}
	return t
}

func (s *substituter) vars(t *types.Tuple) ([]*types.Var, bool) {
	if t == nil {
		return nil, false
	}
	vars := make([]*types.Var, t.Len())
	changed := false
	for i := range vars {
		v := t.At(i)
		vars[i] = v
		if vt := s.typ(v.Type()); vt != v.Type() {
			vars[i] = types.NewParam(v.Pos(), v.Pkg(), v.Name(), vt)
			changed = true
		}
	}
	return vars, changed
}

// canonicalNamed returns the first instance seen with t's key. Go's checker
// and types.Instantiate share a Context, so instances are shared, except that
// identical instances spelled with byte and uint8 (or rune and int32) are
// distinct *types.Named. Keys normalize the spelling; the emitter names
// instance structs by key and their fields by origin, so both work.
func (gp *genericProgram) canonicalNamed(t *types.Named) *types.Named {
	if containsTypeParam(t) {
		return t
	}
	key := typeKey(t)
	if existing := gp.named[key]; existing != nil {
		return existing
	}
	gp.named[key] = t
	return t
}

func containsTypeParam(t types.Type) bool {
	switch t := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if containsTypeParam(t.TypeArgs().At(i)) {
				return true
			}
		}
	case *types.Pointer:
		return containsTypeParam(t.Elem())
	case *types.Slice:
		return containsTypeParam(t.Elem())
	case *types.Array:
		return containsTypeParam(t.Elem())
	case *types.Map:
		return containsTypeParam(t.Key()) || containsTypeParam(t.Elem())
	case *types.Chan:
		return containsTypeParam(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsTypeParam(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if containsTypeParam(t.At(i).Type()) {
				return true
			}
		}
	case *types.Signature:
		return containsTypeParam(t.Params()) || containsTypeParam(t.Results())
	}
	return false
}

func typeList(list *types.TypeList) []types.Type {
	out := make([]types.Type, list.Len())
	for i := range out {
		out[i] = list.At(i)
	}
	return out
}

// typeKey is a package-path-qualified type string, unique for the subset's
// types (which have no function-local type declarations) and independent of
// alias spelling: byte is written uint8 and rune int32.
func typeKey(t types.Type) string {
	var b strings.Builder
	writeType(&b, t, func(p *types.Package) string { return p.Path() })
	return b.String()
}

// relativeTypeString is typeKey relative to pkg, for names and diagnostics.
func relativeTypeString(t types.Type, pkg *types.Package) string {
	var b strings.Builder
	writeType(&b, t, func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		return p.Name()
	})
	return b.String()
}

// writeType writes t as go/types does, but with basic types by kind.
// Composite types the subset never admits as type arguments fall back to
// go/types' spelling.
func writeType(b *strings.Builder, t types.Type, qualifier types.Qualifier) {
	switch t := t.(type) {
	case *types.Basic:
		b.WriteString(types.Typ[t.Kind()].Name())
	case *types.Named:
		if pkg := t.Obj().Pkg(); pkg != nil {
			if q := qualifier(pkg); q != "" {
				b.WriteString(q + ".")
			}
		}
		b.WriteString(t.Obj().Name())
		if t.TypeArgs().Len() > 0 {
			b.WriteString("[")
			for i := 0; i < t.TypeArgs().Len(); i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				writeType(b, t.TypeArgs().At(i), qualifier)
			}
			b.WriteString("]")
		}
	case *types.Pointer:
		b.WriteString("*")
		writeType(b, t.Elem(), qualifier)
	case *types.Slice:
		b.WriteString("[]")
		writeType(b, t.Elem(), qualifier)
	case *types.Array:
		fmt.Fprintf(b, "[%d]", t.Len())
		writeType(b, t.Elem(), qualifier)
	case *types.Interface:
		writeInterface(b, t, qualifier)
	case *types.Signature:
		// Function types are spelled by parameter and result types, so
		// identical types (whatever their parameter names) share a key.
		b.WriteString("func")
		writeSignature(b, t, qualifier)
	case *types.Map:
		b.WriteString("map[")
		writeType(b, t.Key(), qualifier)
		b.WriteString("]")
		writeType(b, t.Elem(), qualifier)
	default:
		b.WriteString(types.TypeString(t, qualifier))
	}
}

func recvBase(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, _ := t.(*types.Named)
	return named
}

// instanceKey returns the canonical key and the display name of an
// instantiation: Pkg.F[A,B] for functions, Pkg.T[A,B].M for methods.
func instanceKey(origin *types.Func, args []types.Type) (key, name string) {
	keys := make([]string, len(args))
	names := make([]string, len(args))
	for i, arg := range args {
		keys[i] = typeKey(arg)
		names[i] = relativeTypeString(arg, origin.Pkg())
	}
	key = "[" + strings.Join(keys, ", ") + "]"
	name = "[" + strings.Join(names, ", ") + "]"
	if recv := origin.Type().(*types.Signature).Recv(); recv != nil {
		base := recvBase(recv.Type()).Obj().Name()
		return origin.Pkg().Path() + "." + base + key + "." + origin.Name(), base + name + "." + origin.Name()
	}
	return origin.Pkg().Path() + "." + origin.Name() + key, origin.Name() + name
}

// mangle encodes a key as a C identifier tail, injectively: letters and
// digits stay, and every other byte becomes an escape introduced by '_'.
func mangle(key string) string {
	var b strings.Builder
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '_':
			b.WriteString("__")
		case c == '.':
			b.WriteString("_D")
		case c == '/':
			b.WriteString("_S")
		case c == '[':
			b.WriteString("_L")
		case c == ']':
			b.WriteString("_R")
		case c == ',':
			b.WriteString("_C")
		case c == '*':
			b.WriteString("_P")
		case c == ' ':
			b.WriteString("_W")
		default:
			fmt.Fprintf(&b, "_x%02x", c)
		}
	}
	return b.String()
}

// callFun strips an explicit instantiation (F[T] or pkg.F[T, U]) from a
// call's function expression. Other expressions are returned unchanged.
func (f *frontend) callFun(e ast.Expr) ast.Expr {
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
	switch o := f.info.Uses[id].(type) {
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

func callIdent(e ast.Expr) *ast.Ident {
	switch e := e.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

// callee is the function a call identifier names: an instance's own object
// for a generic callee, otherwise the recorded use.
func (f *frontend) callee(id *ast.Ident) types.Object {
	if inst := f.instantiated[id]; inst != nil {
		return inst.Object
	}
	return f.info.Uses[id]
}

// method is the function a method call's selector names.
func (f *frontend) method(selector *ast.SelectorExpr) *types.Func {
	fn, _ := f.callee(selector.Sel).(*types.Func)
	return fn
}

// typeArgRule admits a type argument: any type a variable may have. Arrays,
// including those inside pointer and slice types ([][32]byte, []*[4]T), are
// bounded by arrayRule; they are declared where the instance uses them.
// Map types bound theirs through mapRule, and function types through
// funcTypeRule; both apply typeArgRule to their component types.
func (f *frontend) typeArgRule(n ast.Node, t types.Type) {
	if p, ok := pointer(t); ok {
		f.pointerRuleWith(n, p, f.typeArgRule)
		return
	}
	if a, ok := t.Underlying().(*types.Array); ok {
		f.arrayRule(n, a)
		return
	}
	if s, ok := t.(*types.Slice); ok && !types.Identical(s.Elem(), types.Typ[types.Uint8]) {
		f.sliceRuleWith(n, s, f.typeArgRule)
		return
	}
	f.variableRule(n, t)
}

var (
	astObjectType  = reflect.TypeOf((*ast.Object)(nil))
	astScopeType   = reflect.TypeOf((*ast.Scope)(nil))
	astCommentType = reflect.TypeOf((*ast.CommentGroup)(nil))
)

// copyAST deep-copies a syntax tree, keeping positions and recording each
// copied node in pairs (original to copy). Comments and the deprecated
// ast.Object resolution are dropped.
func copyAST(n ast.Node, pairs map[ast.Node]ast.Node) ast.Node {
	return copyValue(reflect.ValueOf(n), pairs).Interface().(ast.Node)
}

func copyValue(v reflect.Value, pairs map[ast.Node]ast.Node) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(copyValue(v.Elem(), pairs))
		return out
	case reflect.Pointer:
		if v.IsNil() || v.Type() == astObjectType || v.Type() == astScopeType || v.Type() == astCommentType {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Elem().Type())
		out.Elem().Set(v.Elem())
		for i := 0; i < out.Elem().NumField(); i++ {
			if field := out.Elem().Field(i); field.CanSet() {
				field.Set(copyValue(v.Elem().Field(i), pairs))
			}
		}
		if node, ok := v.Interface().(ast.Node); ok {
			pairs[node] = out.Interface().(ast.Node)
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(copyValue(v.Index(i), pairs))
		}
		return out
	}
	return v
}

// Instantiation describes one instantiation a type-checked program needs.
type Instantiation struct {
	// Kind is "function", "method" or "type".
	Kind string
	// Generic names the generic declaration: "pkg/path.F", "pkg/path.T.M"
	// or "pkg/path.T".
	Generic string
	// Key is the canonical instantiation (Instance.Key, TypeInstance.Key).
	Key string
}

// Instantiations discovers the instantiations reachable from tp's
// non-generic code, as Check would, without applying the subset rules or the
// instantiation limit (discovery stops after MaxInstantiations per generic
// declaration). tp is not modified. The result is sorted by Generic, then Key.
func Instantiations(tp *TypedProgram) (out []Instantiation, err error) {
	defer recoverCompileError(&err)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{}, Scopes: map[ast.Node]*types.Scope{}, Instances: map[*ast.Ident]types.Instance{},
		Implicits: map[ast.Node]types.Object{}}
	for k, v := range tp.Info.Types {
		info.Types[k] = v
	}
	for k, v := range tp.Info.Defs {
		info.Defs[k] = v
	}
	for k, v := range tp.Info.Uses {
		info.Uses[k] = v
	}
	for k, v := range tp.Info.Selections {
		info.Selections[k] = v
	}
	for k, v := range tp.Info.Scopes {
		info.Scopes[k] = v
	}
	for k, v := range tp.Info.Instances {
		info.Instances[k] = v
	}
	for k, v := range tp.Info.Implicits {
		info.Implicits[k] = v
	}
	f := newFrontend(tp.Fset, info)
	var decls []ast.Decl
	for _, file := range tp.Files {
		decls = append(decls, file.Decls...)
	}
	gp := newGenericProgram(f, info, types.NewContext(), decls, false)
	f.instantiated = gp.instantiated
	gp.run(tp.Files)
	gp.runInterfaces(tp.Files)
	for _, inst := range gp.instances {
		kind, generic := "function", inst.Origin.Pkg().Path()+"."+inst.Origin.Name()
		if recv := inst.Origin.Type().(*types.Signature).Recv(); recv != nil {
			kind, generic = "method", inst.Origin.Pkg().Path()+"."+recvBase(recv.Type()).Obj().Name()+"."+inst.Origin.Name()
		}
		out = append(out, Instantiation{Kind: kind, Generic: generic, Key: inst.Key})
	}
	for _, ti := range gp.typeInstances {
		origin := ti.Type.Origin().Obj()
		out = append(out, Instantiation{Kind: "type", Generic: origin.Pkg().Path() + "." + origin.Name(), Key: ti.Key})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Generic != out[j].Generic {
			return out[i].Generic < out[j].Generic
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}
