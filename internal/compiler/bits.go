package compiler

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// bitsPath is the one standard-library package on-chain programs may import.
// It is type-checked against bitsAPI and lowered to the C runtime in
// bits.c.txt; see docs/GO_BITS.md.
const bitsPath = "math/bits"

//go:embed bits_api.go.txt
var bitsAPI string

//go:embed bits.c.txt
var bitsRuntime string

//go:embed bits16.c.txt
var bits16Runtime string

// bitsFunctions are the math/bits functions available on-chain. Every one has
// a gosvm_bits_<Name> helper in bits.c.txt. The uint-typed functions are not
// listed because uint is not in the subset. The 16-bit helpers live in
// bits16.c.txt, emitted only for programs that also use uint16.
var bitsFunctions = map[string]bool{
	"Add64": true, "Sub64": true, "Mul64": true, "Div64": true, "Rem64": true,
	"Add32": true, "Sub32": true, "Mul32": true, "Div32": true, "Rem32": true,
	"LeadingZeros64": true, "LeadingZeros32": true, "LeadingZeros8": true,
	"TrailingZeros64": true, "TrailingZeros32": true, "TrailingZeros8": true,
	"OnesCount64": true, "OnesCount32": true, "OnesCount8": true,
	"Len64": true, "Len32": true, "Len8": true,
	"Reverse64": true, "Reverse32": true, "Reverse8": true,
	"ReverseBytes64": true, "ReverseBytes32": true,
	"RotateLeft64": true, "RotateLeft32": true, "RotateLeft8": true,
	"LeadingZeros16": true, "TrailingZeros16": true, "OnesCount16": true, "Len16": true,
	"Reverse16": true, "ReverseBytes16": true, "RotateLeft16": true,
}

// importBits type-checks the declaration-only math/bits API. Each program
// check gets its own package object, like the SDK.
func importBits() (*types.Package, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "math/bits/bits.go", bitsAPI, 0)
	if err != nil {
		return nil, err
	}
	return (&types.Config{Sizes: &types.StdSizes{WordSize: 8, MaxAlign: 8}}).Check(bitsPath, fset, []*ast.File{f}, nil)
}

// isBitsFunc reports whether the call's callee is a math/bits function.
func isBitsFunc(info *types.Info, call *ast.CallExpr) (*types.Func, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	fn, ok := info.Uses[selector.Sel].(*types.Func)
	return fn, ok && fn.Pkg() != nil && fn.Pkg().Path() == bitsPath
}

// bitsResults lists, in source order, the multiple-result tuples of math/bits
// calls (Add64, Mul64, Div64, ...), which need the same result aggregate as a
// program function returning them. Unsupported functions are left to bitsCall,
// so their uint results are reported by name rather than as a type error.
func bitsResults(info *types.Info, files []*ast.File) []*ast.CallExpr {
	var calls []*ast.CallExpr
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if fn, ok := isBitsFunc(info, call); ok && bitsFunctions[fn.Name()] {
					if t, ok := info.Types[call].Type.(*types.Tuple); ok && t.Len() > 1 {
						calls = append(calls, call)
					}
				}
			}
			return true
		})
	}
	return calls
}

// bitsCall is the subset rule for math/bits calls: only the listed functions,
// and RotateLeft's int rotation count must be a constant expression. Count
// results are int, admitted exactly where len's result is.
func (c *checker) bitsCall(call *ast.CallExpr, selector *ast.SelectorExpr) {
	fn, ok := c.info.Uses[selector.Sel].(*types.Func)
	if !ok {
		c.fail(call, "only direct math/bits function calls are supported")
	}
	name := fn.Name()
	if !bitsFunctions[name] {
		c.fail(call, "math/bits.%s is unsupported: uint is not in the on-chain subset; use the 64-, 32- or 8-bit variant", name)
	}
	if strings.HasPrefix(name, "RotateLeft") {
		if len(call.Args) != 2 || c.info.Types[call.Args[1]].Value == nil {
			c.fail(call, "math/bits.%s requires a constant rotation count", name)
		}
		c.expr(call.Args[0])
	} else {
		c.callValues(call)
	}
	c.typeRule(call, c.info.Types[call].Type)
}

// bitsCall lowers a certified math/bits call to its runtime helper.
func (g *generator) bitsCall(call *ast.CallExpr) string {
	fn, ok := isBitsFunc(g.info, call)
	if !ok || !bitsFunctions[fn.Name()] {
		g.unchecked(call)
	}
	t := g.info.Types[call].Type
	var args []string
	if strings.HasPrefix(fn.Name(), "RotateLeft") {
		k, ok := constant.Int64Val(g.info.Types[call.Args[1]].Value)
		if !ok {
			g.unchecked(call)
		}
		width := uint64(64)
		switch fn.Name() {
		case "RotateLeft32":
			width = 32
		case "RotateLeft16":
			width = 16
		case "RotateLeft8":
			width = 8
		}
		// Go: s := uint(k) & (n - 1), so negative k rotates right.
		args = []string{g.expr(call.Args[0]), fmt.Sprintf("%dULL", uint64(k)&(width-1))}
	} else {
		args = g.callValues(call)
	}
	value := "gosvm_bits_" + fn.Name() + "(" + strings.Join(args, ",") + ")"
	if tuple, ok := t.(*types.Tuple); ok && tuple.Len() == 2 {
		pair := "gosvm_bits_u64x2"
		if types.Identical(tuple.At(0).Type(), types.Typ[types.Uint32]) {
			pair = "gosvm_bits_u32x2"
		}
		g.next++
		name := fmt.Sprintf("bits%d", g.next)
		g.line("%s %s = %s;", pair, name, value)
		return g.temp(call, t, fmt.Sprintf("{%s.r0,%s.r1}", name, name))
	}
	return g.temp(call, t, value)
}

// usesBits reports whether any program file calls math/bits.
func usesBits(info *types.Info, files []*ast.File) bool {
	found := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && !found {
				_, found = isBitsFunc(info, call)
			}
			return !found
		})
	}
	return found
}
