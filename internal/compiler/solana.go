package compiler

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"gosvm/solana"
)

//go:embed solana.c.txt
var solanaRuntime string

//go:embed accounts.c.txt
var accountsABI string

//go:embed cpi.c.txt
var cpiRuntime string

func sdkSource(version int) string {
	if version == 1 {
		return solana.LegacySource
	}
	return solana.Source
}

type sdkImporter struct {
	pkg     *types.Package
	version int
}

func (s *sdkImporter) Import(path string) (*types.Package, error) {
	if path != "gosvm/solana" {
		return nil, fmt.Errorf("unsupported import %q; only gosvm/solana is available", path)
	}
	if s.pkg != nil {
		return s.pkg, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "solana/api.go", sdkSource(s.version), 0)
	if err != nil {
		return nil, err
	}
	s.pkg, err = (&types.Config{Sizes: &types.StdSizes{WordSize: 8, MaxAlign: 8}}).Check(path, fset, []*ast.File{f}, nil)
	return s.pkg, err
}

func isContext(t types.Type) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "gosvm/solana" && n.Obj().Name() == "Context"
}

func (g *generator) importedCall(call *ast.CallExpr, selector *ast.SelectorExpr) string {
	fn, ok := g.info.Uses[selector.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "gosvm/solana" {
		g.fail(call, "only gosvm/solana direct calls are supported")
	}
	args := g.callValues(call)
	return g.temp(call, g.info.Types[call].Type, "sol_"+fn.Name()+"("+joinArgs(args)+")")
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += ","
		}
		out += a
	}
	return out
}
