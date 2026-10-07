package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// TypedProgram supplies the canonical Go types used by schema generation. It
// shares one type universe across the loaded graph, including the pinned SDK.
// Go type checking does not certify the SBF subset; CompileProgram remains the
// final language/borrow validation and requires the generated Process adapter.
type TypedProgram struct {
	Entry    *types.Package
	Packages map[string]*types.Package
	Files    []*ast.File
	Info     *types.Info
	Fset     *token.FileSet
}

// TypeCheckProgram checks library/handler sources before an entry adapter exists.
// It parses and checks declarations without executing package initialization.
func TypeCheckProgram(program *Program) (out *TypedProgram, err error) {
	if program == nil {
		return nil, fmt.Errorf("missing program")
	}
	if program.SDK != 0 && program.SDK != 1 && program.SDK != 2 {
		return nil, fmt.Errorf("unsupported SDK version %d", program.SDK)
	}
	g := &generator{fset: token.NewFileSet(), info: &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{}, Scopes: map[ast.Node]*types.Scope{},
	}}
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(compileError); ok {
				out, err = nil, e
			} else {
				panic(r)
			}
		}
	}()
	importer := &programImporter{g: g, entryPath: program.Entry, sdk: sdkImporter{version: program.SDK}, units: map[string]PackageSources{}, checked: map[string]*types.Package{}, active: map[string]bool{}}
	for _, unit := range program.Packages {
		if _, exists := importer.units[unit.Path]; exists {
			return nil, fmt.Errorf("duplicate package %q", unit.Path)
		}
		importer.units[unit.Path] = unit
	}
	entry, err := importer.Import(program.Entry)
	if err != nil {
		return nil, err
	}
	return &TypedProgram{Entry: entry, Packages: importer.checked, Files: importer.files, Info: g.info, Fset: g.fset}, nil
}
