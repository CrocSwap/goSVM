package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"gosvm/internal/library"
	"sort"
)

// EntryKind identifies which supported Process signature the entry package declares.
type EntryKind int

const (
	// EntryBytes is the legacy Process(input, output []byte) uint64 entrypoint.
	EntryBytes EntryKind = iota + 1
	// EntryContext is the SDK Process(solana.Context) uint64 entrypoint.
	EntryContext
)

func (k EntryKind) String() string {
	switch k {
	case EntryBytes:
		return "bytes"
	case EntryContext:
		return "context"
	}
	return fmt.Sprintf("EntryKind(%d)", int(k))
}

// CheckedProgram is a program that passed every on-chain subset rule. It is
// the backend-neutral input to code generation: the C/LLVM emitter consumes
// it, and an alternative backend can consume the same value instead. Nothing
// in it depends on C. All syntax and type objects share TypedProgram's FileSet
// and types.Info; a backend must not mutate them.
type CheckedProgram struct {
	// TypedProgram is the Go type-checked package graph: Entry is the entry
	// package, Packages maps import paths to checked user packages (the SDK is
	// excluded), Files holds every user file in dependency-first, file-name
	// order, Info records types/objects/selections/scopes for all Files, and
	// Fset positions them.
	TypedProgram

	// Program is the checked input, including Libraries and the SDK selector.
	Program *Program

	// SDK is the SDK API version the program was checked against: 0 or 2
	// select the current API (2 additionally enables word-sized aggregate
	// copies in the C backend), 1 selects the exact legacy snapshot.
	SDK int
	// SDKPackage is the checked gosvm/solana package, or nil when no package
	// imports it. Its functions are SDK intrinsics with no Go body to lower.
	SDKPackage *types.Package

	// Process is the entry function and EntryKind its signature form.
	Process   *types.Func
	EntryKind EntryKind

	// Functions lists every function and method declaration in Files order
	// (the order the C emitter uses). Every one has a body. A generic
	// declaration is not listed itself: its instances (Function.Instance set)
	// are listed in its place, sorted by Instance.Key.
	Functions []*Function
	// CallOrder lists the same functions with callees before callers. Check
	// rejects recursion, so the direct call graph is acyclic.
	CallOrder []*Function

	// Libraries are the pure native library bindings in the import graph
	// (Program.Libraries). Functions they bind have Function.Library set.
	Libraries []library.Binding

	// Instances lists every instantiation of a generic function or method
	// that checked code calls, in Functions order. Empty without generics.
	Instances []*Instance
	// TypeInstances lists every instantiated generic type that checked code
	// uses, sorted by Key. Empty without generics.
	TypeInstances []*TypeInstance
	// Instantiated maps each identifier naming a generic callee at a call
	// (F in F(x) or F[T](x), pkg.F's F, a method selector's Sel), in Files
	// and in instance bodies, to the instance the call runs.
	Instantiated map[*ast.Ident]*Instance

	// Interface values (docs/GO_INTERFACES.md); all empty without them.
	//
	// Conversions lists every conversion of a non-interface value (or nil)
	// to an interface type in Functions' bodies, in source order. An
	// interface-to-interface conversion copies the value and is not listed.
	Conversions []*Conversion
	// DynamicTypes lists the concrete types interface values may hold or are
	// compared or asserted against, sorted by Key; DynamicTypes[i].ID is i+1.
	DynamicTypes []*DynamicType
	// MethodSlots lists the distinct interface methods called dynamically or
	// required by an asserted interface, sorted by Key.
	MethodSlots []*MethodSlot
	// DynamicCalls maps each method call on an interface value to its slot.
	DynamicCalls map[*ast.CallExpr]*MethodSlot
	// TypeAssertions and TypeSwitches are the x.(T) expressions (not type
	// switch guards) and type switches, in source order.
	TypeAssertions []*ast.TypeAssertExpr
	TypeSwitches   []*ast.TypeSwitchStmt

	// lowered is Files with generic declarations replaced by their instances.
	lowered []*ast.File
	ifaces  *ifaceProgram
	// usesStrings reports that some checked value has a string type.
	usesStrings bool
}

// Function is one checked function or method declaration.
type Function struct {
	// Object is the declared function; methods have a receiver in its signature.
	Object *types.Func
	// Decl is its syntax. Library-bound bodies are synthetic and ignored by
	// code generation; the subset rules are not applied to them.
	Decl *ast.FuncDecl
	// Callees are the distinct program functions it calls directly, in first
	// call order. Calls to SDK functions and builtins are not listed.
	Callees []*types.Func
	// Library is the native binding that replaces the body, if any.
	Library *library.Binding
	// Borrows summarizes which arguments a returned pointer or slice may alias.
	Borrows BorrowSummary
	// Instance is set when the function is an instantiation of a generic
	// declaration; Object and Decl are then the instance's own.
	Instance *Instance
	// HeapVars are the function's local variables, value parameters and
	// receivers whose references escape: they live on the invocation heap
	// (allocated at their declaration, or at entry for parameters) instead of
	// the stack. HeapLiterals are its &T{...} literals that escape likewise.
	// Both are in source order and empty for programs without escapes. Each
	// instance of a generic declaration has its own (its Decl's objects and
	// literals). See docs/GO_ALLOCATION.md.
	HeapVars     []*types.Var
	HeapLiterals []*ast.CompositeLit
}

// BorrowSummary is the borrow checker's result for one function. Results has
// one entry per declared result; non-pointer, non-slice results are empty.
// Escapes lists, ascending, the argument slots (0 is the receiver for
// methods) whose referenced storage the function may store into heap data;
// a caller passing its own local storage there moves that local to the heap.
type BorrowSummary struct {
	Results []ResultBorrow
	Escapes []int
}

// ResultBorrow describes the storage a pointer or slice result may refer to.
// Check has already proven it never refers to the callee's own locals.
type ResultBorrow struct {
	// Parameters lists argument slots whose storage the result may alias,
	// ascending. Slot 0 is the receiver for methods; parameters follow.
	Parameters []int
	// SDK is true when the result may alias SDK-owned invocation storage
	// (account data, keys, instruction data) obtained through gosvm/solana.
	SDK bool
	// Heap is true when the result may refer to invocation-heap storage: an
	// allocation, an escaping local, or a reference loaded from a pointer or
	// slice field or element (which may also hold SDK storage; SDK is then
	// set too when the program imports gosvm/solana).
	Heap bool
}

// Check type-checks the program and enforces every on-chain subset rule
// without generating code: the Process signature, recursion, borrow escapes,
// and the declaration, type, statement and expression rules. It returns the
// first violation in the same order, with the same text, as CompileProgram.
// See docs/BACKEND_INTERFACE.md and docs/ERROR_CONTRACT.md.
func Check(program *Program) (checked *CheckedProgram, err error) {
	defer func() {
		if err != nil {
			checked = nil
		}
	}()
	defer recoverCompileError(&err)
	f, importer, err := loadProgram(program)
	if err != nil {
		return nil, err
	}
	pkg := importer.checked[program.Entry]
	entry, ok := pkg.Scope().Lookup("Process").(*types.Func)
	byteSlice := types.NewSlice(types.Typ[types.Uint8])
	expected := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, nil, "", byteSlice), types.NewVar(0, nil, "", byteSlice)),
		types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.Uint64])), false)
	contextEntry := false
	if ok {
		sig := entry.Type().(*types.Signature)
		contextEntry = sig.Params().Len() == 1 && isContext(sig.Params().At(0).Type()) && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), types.Typ[types.Uint64]) && !sig.Variadic()
	}
	if !ok || !types.Identical(entry.Type(), expected) && !contextEntry {
		return nil, fmt.Errorf("require Process([]byte, []byte) uint64 or Process(solana.Context) uint64")
	}
	checked = &CheckedProgram{
		TypedProgram: TypedProgram{Entry: pkg, Packages: importer.checked, Files: importer.files, Info: f.info, Fset: f.fset},
		Program:      program,
		SDK:          program.SDK,
		Process:      entry,
		EntryKind:    EntryBytes,
		Libraries:    program.Libraries,
	}
	if contextEntry {
		checked.EntryKind = EntryContext
	}
	if importer.hasSDK {
		checked.SDKPackage = importer.sdk.pkg
	}
	externals := libraryBindings(program.Libraries)
	decls := importer.decls

	// Every function needs a body; there is no linker-provided Go code.
	funcs := map[types.Object]*Function{}
	var declared []*Function
	for _, d := range decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			if fn.Body == nil {
				f.fail(fn, "bodyless functions are unsupported")
			}
			object := f.info.Defs[fn.Name].(*types.Func)
			function := &Function{Object: object, Decl: fn}
			if b, ok := externalBinding(externals, object); ok {
				function.Library = &b
			}
			funcs[object] = function
			declared = append(declared, function)
		}
	}
	// Reject recursion: this experiment has no stack-growth runtime. Calls
	// are followed to generic declarations, so a generic function calling
	// itself with any type arguments is recursion too.
	visited, active := map[types.Object]bool{}, map[types.Object]bool{}
	var visit func(types.Object)
	visit = func(o types.Object) {
		if active[o] {
			f.fail(funcs[o].Decl, "recursion is not supported")
		}
		if visited[o] {
			return
		}
		active[o] = true
		ast.Inspect(funcs[o].Decl.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if target := callTarget(f, c); funcs[target] != nil {
					visit(target)
				}
			}
			return true
		})
		active[o], visited[o] = false, true
	}
	for _, function := range declared {
		visit(function.Object)
	}
	// Monomorphize: each instantiation becomes an ordinary declaration.
	gp := newGenericProgram(f, f.info, importer.ctxt, decls, true)
	gp.run(importer.files)
	gp.runInterfaces(importer.files)
	f.instantiated = gp.instantiated
	checked.lowered = gp.lowered(importer.files)
	checked.Instances = gp.sortedInstances(importer.files)
	checked.TypeInstances = gp.sortedTypeInstances()
	checked.Instantiated = gp.instantiated
	f.ifaces = buildInterfaces(f.info, checked.lowered, gp.instances)
	checked.ifaces = f.ifaces
	checked.Conversions = f.ifaces.conversions
	checked.DynamicTypes = f.ifaces.dynamic
	checked.MethodSlots = f.ifaces.slots
	checked.DynamicCalls = f.ifaces.calls
	checked.TypeAssertions = f.ifaces.asserts
	checked.TypeSwitches = f.ifaces.switches
	var lowered []ast.Decl
	for _, file := range checked.lowered {
		lowered = append(lowered, file.Decls...)
	}
	instances := map[types.Object]*Function{}
	for _, d := range lowered {
		if fn, ok := d.(*ast.FuncDecl); ok {
			function := funcs[f.info.Defs[fn.Name]]
			if inst := gp.instanceOf[fn]; inst != nil {
				function = &Function{Object: inst.Object, Decl: fn, Instance: inst}
			}
			instances[function.Object] = function
			checked.Functions = append(checked.Functions, function)
		}
	}
	// The instance call graph needs its own recursion check: a call through
	// a method constraint (x.M() with x of a type parameter) reaches its
	// concrete method only once instantiated. Without generics the graph is
	// the one checked above.
	ordered, ordering := map[types.Object]bool{}, map[types.Object]bool{}
	var order func(*Function)
	order = func(function *Function) {
		if ordering[function.Object] {
			gp.wrap(function.Instance, func() { f.fail(function.Decl, "recursion is not supported") })
		}
		if ordered[function.Object] {
			return
		}
		ordered[function.Object], ordering[function.Object] = true, true
		defer func() { ordering[function.Object] = false }()
		ast.Inspect(function.Decl.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if slot := f.ifaces.calls[c]; slot != nil {
					// A dynamic call may run any method that fills its slot.
					for _, target := range f.ifaces.targets(slot) {
						if callee := instances[target]; callee != nil {
							order(callee)
						}
					}
				} else if id := callIdent(f.callFun(c.Fun)); id != nil {
					if callee := instances[f.callee(id)]; callee != nil {
						order(callee)
						function.addCallee(callee.Object)
					}
				}
			}
			return true
		})
		checked.CallOrder = append(checked.CallOrder, function)
	}
	for _, function := range checked.Functions {
		order(function)
	}
	summaries := f.checkBorrows(lowered, instances, gp, importer.hasSDK)
	for _, function := range checked.Functions {
		summary := summaries[function.Object]
		function.Borrows = exportBorrows(summary)
		function.HeapVars, function.HeapLiterals = exportMoves(summary)
	}
	c := &checker{frontend: f, externals: externals, emitted: map[types.Type]bool{}, gp: gp, typeInstances: checked.TypeInstances}
	c.program(checked.lowered, lowered)
	checked.usesStrings = f.usesStrings
	return checked, nil
}

// loadProgram parses and type-checks the package graph in one type universe.
func loadProgram(program *Program) (*frontend, *programImporter, error) {
	if program == nil {
		return nil, nil, fmt.Errorf("missing program")
	}
	if program.SDK != 0 && program.SDK != 1 && program.SDK != 2 {
		return nil, nil, fmt.Errorf("unsupported SDK version %d", program.SDK)
	}
	if err := ValidateHeapBytes(program.HeapBytes); err != nil {
		return nil, nil, err
	}
	f := newFrontend(token.NewFileSet(), &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{}, Scopes: map[ast.Node]*types.Scope{},
		Instances: map[*ast.Ident]types.Instance{}, Implicits: map[ast.Node]types.Object{},
	})
	importer := &programImporter{f: f, ctxt: types.NewContext(), entryPath: program.Entry, sdk: sdkImporter{version: program.SDK}, units: map[string]PackageSources{}, checked: map[string]*types.Package{}, active: map[string]bool{}}
	for _, unit := range program.Packages {
		if _, exists := importer.units[unit.Path]; exists {
			return nil, nil, fmt.Errorf("duplicate package %q", unit.Path)
		}
		importer.units[unit.Path] = unit
	}
	if _, err := importer.Import(program.Entry); err != nil {
		return nil, nil, err
	}
	return f, importer, nil
}

// callTarget is the declared object a call names directly, if any. Calls to
// generic functions and to methods of generic types name the generic
// declaration (Origin), whatever the type arguments.
func callTarget(f *frontend, c *ast.CallExpr) types.Object {
	id := callIdent(f.callFun(c.Fun))
	if id == nil {
		return nil
	}
	if fn, ok := f.info.Uses[id].(*types.Func); ok {
		return fn.Origin()
	}
	return f.info.Uses[id]
}

func (fn *Function) addCallee(callee *types.Func) {
	for _, existing := range fn.Callees {
		if existing == callee {
			return
		}
	}
	fn.Callees = append(fn.Callees, callee)
}

func libraryBindings(libraries []library.Binding) map[string]library.Binding {
	externals := map[string]library.Binding{}
	for _, b := range libraries {
		externals[b.ImportPath+"."+b.Function] = b
	}
	return externals
}

func externalBinding(externals map[string]library.Binding, o types.Object) (library.Binding, bool) {
	if o == nil || o.Pkg() == nil {
		return library.Binding{}, false
	}
	b, ok := externals[o.Pkg().Path()+"."+o.Name()]
	return b, ok
}

func exportBorrows(s borrowSummary) BorrowSummary {
	out := BorrowSummary{Results: make([]ResultBorrow, len(s.results))}
	for slot := range s.escapes {
		out.Escapes = append(out.Escapes, slot)
	}
	sort.Ints(out.Escapes)
	for i, roots := range s.results {
		seen := map[int]bool{}
		for root := range roots {
			switch {
			case root.foreign:
				out.Results[i].SDK = true
			case root.heap:
				out.Results[i].Heap = true
			case root.scope == nil && root.parameter >= 0 && !seen[root.parameter]:
				seen[root.parameter] = true
				out.Results[i].Parameters = append(out.Results[i].Parameters, root.parameter)
			}
		}
		sort.Ints(out.Results[i].Parameters)
	}
	return out
}

// exportMoves lists a function's heap-moved variables and literals in source
// order.
func exportMoves(s borrowSummary) ([]*types.Var, []*ast.CompositeLit) {
	var vars []*types.Var
	for o := range s.vars {
		vars = append(vars, o.(*types.Var))
	}
	sort.Slice(vars, func(i, j int) bool {
		if vars[i].Pos() != vars[j].Pos() {
			return vars[i].Pos() < vars[j].Pos()
		}
		// A type switch's clause variables share the guard's position;
		// order them by clause.
		return vars[i].Parent() != nil && vars[j].Parent() != nil && vars[i].Parent().Pos() < vars[j].Parent().Pos()
	})
	var lits []*ast.CompositeLit
	for lit := range s.lits {
		lits = append(lits, lit)
	}
	sort.Slice(lits, func(i, j int) bool { return lits[i].Pos() < lits[j].Pos() })
	return vars, lits
}
