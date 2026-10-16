package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// Package-level variables and init functions (docs/GO_GLOBALS.md).
//
// Every SBF invocation is a fresh process, so every package-level variable
// is initialized again before Process runs, in Go's order: packages in the Go
// 1.21 package order (imports first, ties by import path), and within a
// package the variables in go/types' dependency order, then the package's
// init functions in file and source order. Variables live in one block that
// is the invocation heap's first allocation; variables that can never be
// written and have constant initializers are placed in read-only data
// instead and cost nothing. Programs without package-level variables or init
// functions take none of these paths and emit byte-identical C.

// Global is one named package-level variable.
type Global struct {
	// Var is the variable and Spec its declaration.
	Var  *types.Var
	Spec *ast.ValueSpec
	// Init is the declared initializer of Var alone, or nil when there is
	// none (the zero value) or when one multiple-value call initializes
	// several variables.
	Init ast.Expr
	// ReadOnly reports that Check placed the variable in read-only data. Its
	// value is fixed: the zero value or Init, a constant expression or a
	// composite literal of constants, of a type without pointers (integers,
	// bools, arrays and structs of them), a constant string, or a slice of
	// such elements. No code anywhere can change it: the variable is never
	// assigned, incremented or ranged into, its address (or a field's or
	// element's) is never taken, explicitly, by slicing an array or by a
	// pointer-receiver method or method value, and a slice variable's value
	// is only indexed for reading, ranged over, compared with nil or passed
	// to len or cap. A read-only variable takes no heap storage and has no
	// initialization code.
	ReadOnly bool
	// Offset is the variable's byte offset in the globals block, or -1 when
	// ReadOnly.
	Offset int64
}

// PackageInit is the initialization of one package.
type PackageInit struct {
	Package *types.Package
	// Vars is the package's synthetic variable initializer, or nil when no
	// variable needs initialization code. Its body is one assignment per
	// go/types initializer (Info.InitOrder) that is not read-only, in that
	// order: `a = e`, `a, b = f()`, or `_ = e` for a blank variable (whose
	// initializer still runs). The assigned identifiers are synthetic (their
	// Uses and Types entries name the variable); the right-hand sides are the
	// declared initializers.
	Vars *Function
	// Funcs are the package's init functions in file and source order.
	Funcs []*Function
	// EvaluatedVars and EvaluatedFuncs are the leading statements of Vars'
	// body and the leading Funcs that were evaluated at compile time
	// (CheckedProgram.InitImage); the rest run at every invocation.
	EvaluatedVars, EvaluatedFuncs int
}

// globalProgram is the package-level variable information of a program.
type globalProgram struct {
	globals []*Global
	byVar   map[*types.Var]*Global
	inits   []*PackageInit
	bytes   int64       // GlobalBytes
	vars    []*Function // synthetic initializers, in inits order
}

// isPackageVar reports whether o is a variable declared at package scope.
func isPackageVar(o types.Object) bool {
	v, ok := o.(*types.Var)
	return ok && !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

// packageVar is the program's package-level variable that e names directly
// (V) or qualified by its package (pkg.V), or nil.
func (f *frontend) packageVar(e ast.Expr) *types.Var {
	if f.globals == nil {
		return nil
	}
	var id *ast.Ident
	switch e := e.(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		if !ok {
			return nil
		}
		if _, ok := f.info.Uses[x].(*types.PkgName); !ok {
			return nil
		}
		id = e.Sel
	default:
		return nil
	}
	v, ok := f.info.Uses[id].(*types.Var)
	if !ok || f.globals.byVar[v] == nil {
		return nil
	}
	return v
}

// packageOrder is Go 1.21's package initialization order: repeatedly the
// first package, by import path, whose imports are all initialized.
func packageOrder(packages map[string]*types.Package) []string {
	var paths []string
	for path := range packages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	done := map[string]bool{}
	var out []string
	for len(out) < len(paths) {
		progress := false
		for _, path := range paths {
			if done[path] {
				continue
			}
			ready := true
			for _, im := range packages[path].Imports() {
				if _, user := packages[im.Path()]; user && !done[im.Path()] {
					ready = false
					break
				}
			}
			if ready {
				done[path], progress = true, true
				out = append(out, path)
				break
			}
		}
		if !progress { // unreachable: the importer rejects cycles
			break
		}
	}
	return out
}

// plainData reports whether values of t contain no pointers, slices,
// strings, maps, interfaces or function values: integers, bools, and arrays
// and structs of them.
func plainData(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsInteger|types.IsBoolean) != 0 && u.Info()&types.IsUntyped == 0
	case *types.Array:
		return plainData(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if !plainData(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// constantInit reports whether e is a constant expression or a composite
// literal whose elements are, recursively.
func (f *frontend) constantInit(e ast.Expr) bool {
	if f.info.Types[e].Value != nil {
		return true
	}
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			elt = kv.Value
		}
		if !f.constantInit(elt) {
			return false
		}
	}
	return true
}

// readOnlyCandidate reports whether g's type and initializer allow
// read-only data; the uses decide (globalWrites).
func (f *frontend) readOnlyCandidate(g *Global) bool {
	t := g.Var.Type()
	init := g.Init
	if init != nil && f.info.Types[init].IsNil() {
		init = nil
	}
	if g.Init == nil && len(g.Spec.Values) > 0 {
		return false // one call initializes several variables
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.String {
			return init == nil || f.info.Types[init].Value != nil
		}
	case *types.Slice:
		if _, named := t.(*types.Named); named || !plainData(u.Elem()) {
			return false
		}
		if init == nil {
			return true
		}
		lit, ok := ast.Unparen(init).(*ast.CompositeLit)
		return ok && len(lit.Elts) > 0 && f.constantInit(lit)
	}
	return plainData(t) && (init == nil || f.constantInit(init))
}

// globalWrites marks the package-level variables whose storage some code in
// roots may write: an assignment, ++/--, range assignment, address (&, an
// array slice, a pointer-receiver method or method value), through a field
// or element path; for slice variables, also any use of the slice value
// other than indexing, ranging, len, cap, a comparison with nil, the source
// of copy or the spread source of append (a copy could be written through).
func (f *frontend) globalWrites(roots []ast.Node, globals map[*types.Var]*Global) map[*types.Var]bool {
	written := map[*types.Var]bool{}
	for _, root := range roots {
		var stack []ast.Node
		ast.Inspect(root, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			v, ok := f.info.Uses[id].(*types.Var)
			if !ok || globals[v] == nil || written[v] {
				return true
			}
			if f.globalUseWrites(stack) {
				written[v] = true
			}
			return true
		})
	}
	return written
}

// globalUseWrites classifies the use of a package-level variable whose
// identifier ends stack (its ancestors before it).
func (f *frontend) globalUseWrites(stack []ast.Node) bool {
	cur := stack[len(stack)-1]
	i := len(stack) - 2
	if i >= 0 {
		if sel, ok := stack[i].(*ast.SelectorExpr); ok && sel.Sel == cur {
			cur, i = sel, i-1 // pkg.V
		}
	}
	// A read-only variable that is not plain data (a string or a slice) is
	// emitted as a value, not an object, so its own address may not reach
	// a read-only pointer parameter (pointers to strings and slices are not
	// in the subset today; this keeps the rule sound if they are added).
	// Its elements' addresses are fine.
	addressable := plainData(f.info.Types[cur.(ast.Expr)].Type)
	return f.globalStorageWrites(stack, i, cur, addressable)
}

// globalStorageWrites classifies a use of storage cur (a package-level
// variable or a path into it, or the storage a read-only pointer parameter
// points to) whose ancestors are stack[:i+1]. addressable reports whether
// cur itself may be addressed for a read-only pointer parameter; paths into
// it always may.
func (f *frontend) globalStorageWrites(stack []ast.Node, i int, cur ast.Node, addressable bool) bool {
	// copied reports a read of cur's whole value: a slice value could then
	// be written through.
	copied := func() bool {
		_, isSlice := f.info.Types[cur.(ast.Expr)].Type.(*types.Slice)
		return isSlice
	}
	for ; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.SelectorExpr:
			if p.X != cur {
				return copied()
			}
			t := f.info.Types[p.X].Type
			if _, isPointer := t.Underlying().(*types.Pointer); isPointer {
				return false // reads the pointer
			}
			s := f.info.Selections[p]
			if s == nil {
				return true
			}
			if s.Kind() == types.FieldVal {
				cur, addressable = p, true
				continue
			}
			fn, _ := f.info.Uses[p.Sel].(*types.Func)
			if fn == nil {
				return true
			}
			recv := fn.Type().(*types.Signature).Recv()
			if recv == nil {
				return true
			}
			if _, pointerRecv := recv.Type().(*types.Pointer); pointerRecv {
				// A read-only receiver only reads (readonlyptr.go).
				return !addressable || !f.pointerReceiverRead(stack, i, fn)
			}
			return copied()
		case *ast.IndexExpr:
			if p.X != cur {
				return copied()
			}
			switch f.info.Types[p.X].Type.Underlying().(type) {
			case *types.Array, *types.Slice:
				cur, addressable = p, true
				continue
			}
			return false // a map lookup or a string byte
		case *ast.SliceExpr:
			if p.X != cur {
				return copied()
			}
			return !isString(f.info.Types[p.X].Type)
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				// An address passed only to a read-only pointer
				// parameter (readonlyptr.go).
				return !addressable || !f.addressRead(stack, i)
			}
			return copied()
		case *ast.AssignStmt:
			for _, lhs := range p.Lhs {
				if lhs == cur {
					return true
				}
			}
			return copied()
		case *ast.IncDecStmt:
			return true
		case *ast.RangeStmt:
			return p.Key == cur || p.Value == cur
		case *ast.CallExpr:
			if id, ok := ast.Unparen(p.Fun).(*ast.Ident); ok {
				if b, ok := f.info.Uses[id].(*types.Builtin); ok {
					// len and cap, the source of copy and the spread source
					// of append only read the elements.
					switch {
					case b.Name() == "len" || b.Name() == "cap":
						return false
					case b.Name() == "copy" && len(p.Args) == 2 && p.Args[1] == cur:
						return false
					case b.Name() == "append" && p.Ellipsis.IsValid() && len(p.Args) == 2 && p.Args[1] == cur:
						return false
					}
				}
			}
			return copied()
		case *ast.BinaryExpr:
			if p.Op == token.EQL || p.Op == token.NEQ {
				other := p.X
				if other == cur {
					other = p.Y
				}
				if f.info.Types[other].IsNil() {
					return false
				}
			}
			return copied()
		default:
			return copied()
		}
	}
	return copied()
}

// buildGlobals collects the package-level variables and init functions,
// decides read-only placement and the globals block layout, and returns the
// lowered files with each package's variable initializer added. Specs keep
// their initializers only when every variable they declare is read-only;
// every other initializer moves into the package's initializer function, so
// each expression appears once in the lowered files.
func (f *frontend) buildGlobals(importer *programImporter, lowered []*ast.File) []*ast.File {
	byVar := map[*types.Var]*Global{}
	var all []*Global
	packageGlobals := map[string][]*Global{}
	initFuncs := map[string][]*ast.FuncDecl{}
	for i, file := range importer.files {
		path := importer.filePaths[i]
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					spec := spec.(*ast.ValueSpec)
					for j, name := range spec.Names {
						v, ok := f.info.Defs[name].(*types.Var)
						if !ok || name.Name == "_" {
							continue
						}
						g := &Global{Var: v, Spec: spec, Offset: -1}
						if len(spec.Values) == len(spec.Names) {
							g.Init = spec.Values[j]
						}
						byVar[v] = g
						all = append(all, g)
						packageGlobals[path] = append(packageGlobals[path], g)
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "init" {
					initFuncs[path] = append(initFuncs[path], d)
				}
			}
		}
	}
	if len(all) == 0 && len(initFuncs) == 0 && len(importer.initOrder) == 0 {
		return lowered
	}
	gp := &globalProgram{byVar: byVar}
	f.globals = gp

	// Read-only placement: a sound syntactic analysis of every body and
	// initializer; any possible write keeps the variable in the block.
	var roots []ast.Node
	for _, file := range lowered {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				roots = append(roots, fn)
			}
		}
	}
	for _, path := range sortedKeys(importer.initOrder) {
		for _, init := range importer.initOrder[path] {
			roots = append(roots, init.Rhs)
		}
	}
	f.roPointers = f.newReadOnlyPointers(roots)
	written := f.globalWrites(roots, byVar)
	for _, g := range all {
		g.ReadOnly = !written[g.Var] && f.readOnlyCandidate(g)
	}

	// Globals in package initialization order, then declaration order; the
	// block holds the writable ones as a C struct would.
	sizes := &types.StdSizes{WordSize: 8, MaxAlign: 8}
	offset, align := int64(0), int64(1)
	order := packageOrder(importer.checked)
	for _, path := range order {
		for _, g := range packageGlobals[path] {
			gp.globals = append(gp.globals, g)
			if g.ReadOnly {
				continue
			}
			a := sizes.Alignof(g.Var.Type())
			if a > align {
				align = a
			}
			offset = (offset + a - 1) / a * a
			g.Offset = offset
			offset += cSizeof(g.Var.Type())
		}
	}
	if offset > 0 {
		gp.bytes = roundUp8((offset + align - 1) / align * align)
	}

	// Each package's initializer: the non-read-only initializers in
	// go/types' order. A specification keeps its initializers (for the
	// read-only data) only when all its variables are read-only.
	synthetic := map[string]*ast.FuncDecl{}
	for _, path := range order {
		pkg := importer.checked[path]
		var stmts []ast.Stmt
		pos := token.NoPos
		for _, init := range importer.initOrder[path] {
			readOnly := true
			for _, v := range init.Lhs {
				if g := byVar[v]; g == nil || !g.ReadOnly {
					readOnly = false
				}
			}
			if readOnly {
				continue
			}
			lhs := make([]ast.Expr, len(init.Lhs))
			for i, v := range init.Lhs {
				id := &ast.Ident{NamePos: v.Pos(), Name: v.Name()}
				if v.Name() == "_" {
					f.info.Defs[id] = nil
				} else if err := types.CheckExpr(f.fset, pkg, token.NoPos, id, f.info); err != nil {
					f.fail(id, "internal error: %v", err)
				}
				lhs[i] = id
			}
			if !pos.IsValid() {
				pos = init.Lhs[0].Pos()
			}
			stmts = append(stmts, &ast.AssignStmt{Lhs: lhs, TokPos: init.Rhs.Pos(), Tok: token.ASSIGN, Rhs: []ast.Expr{init.Rhs}})
		}
		var vars *Function
		if len(stmts) > 0 {
			object := types.NewFunc(pos, pkg, "init.vars", types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(), false))
			decl := &ast.FuncDecl{Name: &ast.Ident{NamePos: pos, Name: object.Name()}, Type: &ast.FuncType{Func: pos, Params: &ast.FieldList{}},
				Body: &ast.BlockStmt{Lbrace: pos, List: stmts, Rbrace: pos}}
			f.info.Defs[decl.Name] = object
			// Initializers are evaluated at package scope; a composite
			// literal there belongs to its file's scope.
			f.info.Scopes[decl.Type] = pkg.Scope()
			vars = &Function{Object: object, Decl: decl, PackageInit: pkg}
			gp.vars = append(gp.vars, vars)
			synthetic[path] = decl
		}
		if vars != nil || len(initFuncs[path]) > 0 {
			gp.inits = append(gp.inits, &PackageInit{Package: pkg, Vars: vars})
		}
	}

	out := make([]*ast.File, len(lowered))
	last := map[string]int{}
	for i := range lowered {
		last[importer.filePaths[i]] = i
	}
	for i, file := range lowered {
		path := importer.filePaths[i]
		changed := false
		var decls []ast.Decl
		for _, d := range file.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				decls = append(decls, d)
				continue
			}
			changed = true
			copied := *gen
			copied.Specs = nil
			for _, spec := range gen.Specs {
				spec := spec.(*ast.ValueSpec)
				keep := len(spec.Values) > 0
				for _, name := range spec.Names {
					v, _ := f.info.Defs[name].(*types.Var)
					if g := byVar[v]; g == nil || !g.ReadOnly {
						keep = false
					}
				}
				stripped := *spec
				if !keep {
					stripped.Values = nil
				}
				copied.Specs = append(copied.Specs, &stripped)
			}
			decls = append(decls, &copied)
		}
		if decl := synthetic[path]; decl != nil && last[path] == i {
			changed = true
			decls = append(decls, decl)
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

// finishInits fills the init functions of each package's PackageInit from
// the checked functions (Files order is file and source order).
func (gp *globalProgram) finishInits(functions []*Function) {
	byPackage := map[*types.Package]*PackageInit{}
	for _, pi := range gp.inits {
		byPackage[pi.Package] = pi
	}
	for _, fn := range functions {
		if fn.PackageInit == nil && fn.Decl.Recv == nil && fn.Decl.Name.Name == "init" && fn.Instance == nil {
			if pi := byPackage[fn.Object.Pkg()]; pi != nil {
				pi.Funcs = append(pi.Funcs, fn)
			}
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// globalBudget rejects a globals block that cannot fit the heap budget.
func (f *frontend) globalBudget(heapBytes int) {
	if f.globals == nil || f.globals.bytes == 0 {
		return
	}
	if heapBytes == 0 {
		heapBytes = DefaultHeapBytes
	}
	if f.globals.bytes > int64(heapBytes)-8 {
		for _, g := range f.globals.globals {
			if !g.ReadOnly {
				f.fail(g.Spec, "package-level variables need %d heap bytes; the %d-byte heap budget leaves %d", f.globals.bytes, heapBytes, heapBytes-8)
			}
		}
	}
}

// globalSpec applies the declaration rules to a package-level var
// specification of the lowered files.
func (c *checker) globalSpec(spec *ast.ValueSpec) {
	for i, name := range spec.Names {
		if name.Name == "_" {
			continue
		}
		o := c.info.Defs[name]
		if isContext(o.Type()) {
			c.fail(spec, "Context must come from the entrypoint, not a package-level variable")
		}
		if i < len(spec.Values) {
			// A read-only variable's constant initializer.
			c.exprAs(spec.Values[i], o.Type())
			if _, ok := o.Type().Underlying().(*types.Array); ok {
				continue
			}
		}
		c.variableRule(spec, o.Type())
	}
}

// Emitter.

// globalsBase is the C pointer to the globals block.
const globalsBase = "GOSVM_GLOBALS"

// emitGlobals declares the globals block and the read-only data, and names
// every package-level variable.
func (g *generator) emitGlobals(program *CheckedProgram) {
	if len(program.Globals) == 0 {
		return
	}
	if program.GlobalBytes > 0 {
		g.line("typedef struct {")
		for i, global := range program.Globals {
			if !global.ReadOnly {
				g.line("%s g%d; /* %s */", g.variable(global.Spec, global.Var.Type()), i, globalComment(global.Var))
			}
		}
		g.line("} gosvm_globals;")
		g.line("_Static_assert(((sizeof(gosvm_globals)+7)&~7ULL) == %dULL, \"globals block layout\");", program.GlobalBytes)
		// The block is the invocation heap's first allocation (gosvm_init).
		g.line("#define %s ((gosvm_globals *)(GOSVM_HEAP_BASE + 8))", globalsBase)
	}
	for i, global := range program.Globals {
		t := global.Var.Type()
		if !global.ReadOnly {
			g.names[global.Var] = fmt.Sprintf("(%s->g%d)", globalsBase, i)
			continue
		}
		init := global.Init
		if init != nil && g.info.Types[init].IsNil() {
			init = nil
		}
		if isString(t) {
			if init == nil {
				g.names[global.Var] = "((gosvm_string){0,0})"
			} else {
				g.names[global.Var] = stringLiteral(g.info.Types[init].Value)
			}
			continue
		}
		if s, ok := t.(*types.Slice); ok {
			if init == nil {
				g.names[global.Var] = "((slice){0})"
				continue
			}
			lit := ast.Unparen(init).(*ast.CompositeLit)
			items, n := g.constElements(lit, s.Elem())
			g.line("static const %s gosvm_ro%d[%d] = {%s}; /* %s */", g.variable(global.Spec, s.Elem()), i, n, items, globalComment(global.Var))
			g.names[global.Var] = fmt.Sprintf("GOSVM_SLICE((u8 *)gosvm_ro%d,%d)", i, n)
			continue
		}
		value := "{0}"
		if init != nil {
			value = g.constInit(init, t)
		}
		ctype := g.variable(global.Spec, t)
		g.line("static const %s gosvm_ro%d = %s; /* %s */", ctype, i, value, globalComment(global.Var))
		g.names[global.Var] = fmt.Sprintf("(*(%s *)&gosvm_ro%d)", ctype, i)
	}
}

// globalComment names a variable in generated C (package path and name);
// characters that could end the comment are dropped.
func globalComment(v *types.Var) string {
	return strings.ReplaceAll(v.Pkg().Path()+"."+v.Name(), "*/", "")
}

// constInit is the C constant initializer of a read-only value of type t.
func (g *generator) constInit(e ast.Expr, t types.Type) string {
	tv := g.info.Types[e]
	if tv.Value != nil {
		switch tv.Value.Kind() {
		case constant.Bool:
			if constant.BoolVal(tv.Value) {
				return "1"
			}
			return "0"
		case constant.Int:
			if isSigned(t) {
				if negative, ok := signedConstant(tv.Value); ok {
					return negative
				}
			}
			v, ok := constant.Uint64Val(tv.Value)
			if !ok {
				g.unchecked(e)
			}
			return fmt.Sprintf("%dULL", v)
		}
		g.unchecked(e)
	}
	lit, ok := ast.Unparen(e).(*ast.CompositeLit)
	if !ok {
		g.unchecked(e)
	}
	switch u := t.Underlying().(type) {
	case *types.Array:
		items, _ := g.constElements(lit, u.Elem())
		if items == "" {
			return "{0}"
		}
		return "{{" + items + "}}"
	case *types.Struct:
		var fields []string
		for i, elt := range lit.Elts {
			field, value := i, elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				value = kv.Value
				for j := 0; j < u.NumFields(); j++ {
					if u.Field(j).Name() == kv.Key.(*ast.Ident).Name {
						field = j
					}
				}
			}
			fields = append(fields, fmt.Sprintf(".%s=%s", g.name(u.Field(field)), g.constInit(value, u.Field(field).Type())))
		}
		if len(fields) == 0 {
			return "{0}"
		}
		return "{" + strings.Join(fields, ",") + "}"
	}
	g.unchecked(e)
	return ""
}

// constElements are an array or slice literal's designated C initializers
// and its length (one past the largest index).
func (g *generator) constElements(lit *ast.CompositeLit, elem types.Type) (string, int64) {
	var items []string
	index, n := int64(0), int64(0)
	for _, elt := range lit.Elts {
		value := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			var valid bool
			index, valid = constant.Int64Val(g.info.Types[kv.Key].Value)
			if !valid {
				g.unchecked(kv)
			}
			value = kv.Value
		}
		items = append(items, fmt.Sprintf("[%d]=%s", index, g.constInit(value, elem)))
		index++
		if index > n {
			n = index
		}
	}
	return strings.Join(items, ","), n
}

// emitInit defines gosvm_init, which allocates the globals block and runs
// every package's initialization in order, and the go_Process wrapper that
// calls it before the entry function on every invocation.
func (g *generator) emitInit(program *CheckedProgram) {
	if !program.initializes() {
		return
	}
	var install []string
	if program.InitImage != nil {
		// The compile-time evaluated prefix (initeval_emit.go).
		install = g.emitImage(program)
	}
	g.line("static void gosvm_init(void) {")
	if install != nil {
		for _, s := range install {
			g.line("%s", s)
		}
	} else if program.GlobalBytes > 0 {
		g.line("(void)gosvm_alloc(%dULL); /* the globals block: the first allocation, at GOSVM_HEAP_BASE + 8 */", program.GlobalBytes)
	}
	for _, pi := range program.Inits {
		if pi.Vars != nil && pi.EvaluatedVars < len(pi.Vars.Decl.Body.List) {
			g.line("%s();", g.name(pi.Vars.Object))
		}
		for _, fn := range pi.Funcs[pi.EvaluatedFuncs:] {
			g.line("%s();", g.name(fn.Object))
		}
	}
	g.line("}")
	sig := program.Process.Type().(*types.Signature)
	var params, args []string
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, fmt.Sprintf("%s p%d", g.variable(posNode(program.Process.Pos()), sig.Params().At(i).Type()), i))
		args = append(args, fmt.Sprintf("p%d", i))
	}
	g.line("static u64 go_Process(%s) {", strings.Join(params, ","))
	g.line("gosvm_init();")
	g.line("return %s(%s);", entryBody, strings.Join(args, ","))
	g.line("}")
}

// entryBody is the C name of Process in programs with initialization: the
// generated go_Process initializes, then calls it.
const entryBody = "gosvm_process"

// initializes reports whether the program has initialization code or
// writable package-level storage.
func (p *CheckedProgram) initializes() bool {
	return p.GlobalBytes > 0 || len(p.Inits) > 0
}

// inPlaceTable reports whether every value whose type has underlying array
// type a is a package-level variable used only in place, so that no copy of
// it is ever made (docs/GO_GLOBALS.md#large-tables). Such a variable lives in
// read-only data or the globals block, never in a stack frame, so the
// per-value array limit (which protects SBF stack frames) does not apply.
//
// In the lowered files, no object other than a package-level variable may
// have the type; every expression of the type (or of a pointer to it) must
// be a read-only variable's composite-literal initializer, or name such a
// variable as the operand of an index expression, of len or cap, or of a
// range statement without an iteration value. A writable variable with an
// initializer is assigned in its package's initializer, which copies the
// value, so it is not admitted.
func (c *checker) inPlaceTable(a *types.Array) bool {
	if c.globals == nil {
		return false
	}
	isTable := func(t types.Type) bool {
		if t == nil {
			return false
		}
		if p, ok := t.Underlying().(*types.Pointer); ok {
			t = p.Elem()
		}
		return types.Identical(t.Underlying(), a)
	}
	inits := map[ast.Expr]bool{}
	for _, g := range c.globals.globals {
		if g.ReadOnly && g.Init != nil {
			inits[ast.Unparen(g.Init)] = true
		}
	}
	admitted, used := true, false
	for _, file := range c.files {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if !admitted {
				return false
			}
			stack = append(stack, n)
			if id, ok := n.(*ast.Ident); ok {
				if o := c.info.Defs[id]; o != nil {
					if _, isType := o.(*types.TypeName); !isType && isTable(o.Type()) && !isPackageVar(o) {
						admitted = false
					}
				}
			}
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			tv, ok := c.info.Types[e]
			if !ok || tv.IsType() || !isTable(tv.Type) {
				return true
			}
			if inits[e] {
				used = true
				return true
			}
			if _, isParen := e.(*ast.ParenExpr); isParen {
				return true // its operand is classified
			}
			if len(stack) >= 2 {
				if sel, ok := stack[len(stack)-2].(*ast.SelectorExpr); ok && sel.Sel == e && c.packageVar(sel) != nil {
					return true // pkg.V is classified as a whole
				}
			}
			if !c.inPlaceUse(stack) {
				admitted = false
			}
			used = true
			return true
		})
	}
	return admitted && used
}

// inPlaceUse reports whether the expression ending stack (its ancestors
// before it) names a package-level variable used in place: indexed, the
// operand of len or cap, or ranged over without an iteration value.
func (c *checker) inPlaceUse(stack []ast.Node) bool {
	e := stack[len(stack)-1].(ast.Expr)
	if v := c.packageVar(e); v == nil || v.Name() == "_" {
		return false
	}
	if _, isPointer := c.info.Types[e].Type.Underlying().(*types.Pointer); isPointer {
		return false
	}
	var cur ast.Node = e
	for i := len(stack) - 2; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.IndexExpr:
			return p.X == cur
		case *ast.RangeStmt:
			return p.X == cur && p.Value == nil
		case *ast.CallExpr:
			if id, ok := ast.Unparen(p.Fun).(*ast.Ident); ok && len(p.Args) == 1 && p.Args[0] == cur {
				if b, ok := c.info.Uses[id].(*types.Builtin); ok {
					return b.Name() == "len" || b.Name() == "cap"
				}
			}
		}
		return false
	}
	return false
}
