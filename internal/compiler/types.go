package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

// TypedProgram supplies the canonical Go types used by schema generation. It
// shares one type universe across the loaded graph, including the pinned SDK.
// Go type checking does not certify the SBF subset; Check performs the final
// language/borrow validation and requires the generated Process adapter.
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
	defer recoverCompileError(&err)
	f, importer, err := loadProgram(program)
	if err != nil {
		return nil, err
	}
	return &TypedProgram{Entry: importer.checked[program.Entry], Packages: importer.checked, Files: importer.files, Info: f.info, Fset: f.fset}, nil
}
