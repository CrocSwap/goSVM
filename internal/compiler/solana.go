package compiler

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"gosvm/solana"
)

//go:embed solana.c.txt
var solanaRuntime string

//go:embed accounts.c.txt
var accountsABI string

//go:embed cpi.c.txt
var cpiRuntime string

// WideAccounts is the opt-in incoming account limit (Program.MaxAccounts):
// a 64-entry account table, the most a 4 KiB SBF frame holds.
const WideAccounts = 64

// ValidateMaxAccounts accepts 0 (the SDK's limit: 16 for SDK 1, 32 for SDK
// 2) or WideAccounts, which the frozen SDK 1 loader does not support.
func ValidateMaxAccounts(n, sdk int) error {
	if n != 0 && n != WideAccounts {
		return fmt.Errorf("max accounts %d: use 0 (the SDK's limit: 16 for SDK 1, 32 for SDK 2) or %d", n, WideAccounts)
	}
	if n != 0 && sdk == 1 {
		return fmt.Errorf("max accounts %d requires SDK 2; SDK 1 keeps its 16-account loader", n)
	}
	return nil
}

// wideTrampoline runs Process in a frame of its own for opted-in programs:
// the 64-entry table (3,584 bytes) fills most of the entrypoint's 4 KiB SBF
// frame, which Process inlined there would overflow.
const wideTrampoline = `#define GOSVM_WIDE_ACCOUNTS 1
static u64 go_Process(Context *);
static __attribute__((noinline)) u64 gosvm_run(Context *c) {return go_Process(c);}
`

// wideRuntime is a runtime template with the 64-account change, applied only
// to opted-in programs so that every other program's C is unchanged.
func wideRuntime(template, old, replacement string) string {
	if strings.Count(template, old) != 1 {
		panic("gosvm: 64-account runtime template changed")
	}
	return strings.Replace(template, old, replacement, 1)
}

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
		g.unchecked(call)
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
