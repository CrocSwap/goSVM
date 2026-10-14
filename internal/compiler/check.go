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
	// (the order the C emitter uses). Every one has a body.
	Functions []*Function
	// CallOrder lists the same functions with callees before callers. Check
	// rejects recursion, so the direct call graph is acyclic.
	CallOrder []*Function

	// Libraries are the pure native library bindings in the import graph
	// (Program.Libraries). Functions they bind have Function.Library set.
	Libraries []library.Binding
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
}

// BorrowSummary is the borrow checker's result for one function. Results has
// one entry per declared result; non-pointer, non-slice results are empty.
type BorrowSummary struct {
	Results []ResultBorrow
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
			checked.Functions = append(checked.Functions, function)
		}
	}
	// Reject recursion: this experiment has no stack-growth runtime.
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
		function := funcs[o]
		ast.Inspect(function.Decl.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if target := callTarget(f.info, c); funcs[target] != nil {
					visit(target)
					function.addCallee(funcs[target].Object)
				}
			}
			return true
		})
		active[o], visited[o] = false, true
		checked.CallOrder = append(checked.CallOrder, function)
	}
	for _, function := range checked.Functions {
		visit(function.Object)
	}
	summaries := f.checkBorrows(decls, funcs)
	for _, function := range checked.Functions {
		function.Borrows = exportBorrows(summaries[function.Object])
	}
	c := &checker{frontend: f, externals: externals, emitted: map[types.Type]bool{}}
	c.program(importer.files, decls)
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
	f := newFrontend(token.NewFileSet(), &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{}, Scopes: map[ast.Node]*types.Scope{},
	})
	importer := &programImporter{f: f, entryPath: program.Entry, sdk: sdkImporter{version: program.SDK}, units: map[string]PackageSources{}, checked: map[string]*types.Package{}, active: map[string]bool{}}
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

// callTarget is the declared object a call names directly, if any.
func callTarget(info *types.Info, c *ast.CallExpr) types.Object {
	switch callee := c.Fun.(type) {
	case *ast.Ident:
		return info.Uses[callee]
	case *ast.SelectorExpr:
		return info.Uses[callee.Sel]
	}
	return nil
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
	for i, roots := range s.results {
		seen := map[int]bool{}
		for root := range roots {
			switch {
			case root.foreign:
				out.Results[i].SDK = true
			case root.scope == nil && root.parameter >= 0 && !seen[root.parameter]:
				seen[root.parameter] = true
				out.Results[i].Parameters = append(out.Results[i].Parameters, root.parameter)
			}
		}
		sort.Ints(out.Results[i].Parameters)
	}
	return out
}
