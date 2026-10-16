package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Elided &T{...} elements of array type (docs/GO_ARRAYS.md#pointer-literals).
// In a composite literal whose element (or map key or value) type is *T,
// Go lets an element be written {...} for &T{...}. goSVM's lowering of an
// elided element boxes a struct directly; for an array T, Check rewrites the
// element into the explicit form, so every later pass, the emitter and
// alternative backends see &T{...}:
//
//	var multipliers = [2]*Int{nil, {1, 2, 3, 4}}   // type Int [4]uint64
//	becomes
//	var multipliers = [2]*Int{nil, &Int{1, 2, 3, 4}}
//
// The two forms are the same expression in Go (the spec defines the elided
// one as the explicit one), so evaluation order and allocation are unchanged:
// &T{...} is allocated like any composite-literal address
// (docs/GO_ALLOCATION.md). Elided struct elements keep their own lowering,
// so programs without elided array elements are unchanged.

// lowerElidedArrayPointers rewrites, in place, every elided element of
// pointer-to-array type in files.
func lowerElidedArrayPointers(info *types.Info, files []*ast.File) {
	wrap := func(e ast.Expr) ast.Expr {
		lit, ok := e.(*ast.CompositeLit)
		if !ok || lit.Type != nil {
			return e
		}
		tv, ok := info.Types[lit]
		if !ok {
			return e
		}
		p, ok := tv.Type.Underlying().(*types.Pointer)
		if !ok {
			return e
		}
		if _, ok := p.Elem().Underlying().(*types.Array); !ok {
			return e
		}
		u := &ast.UnaryExpr{OpPos: lit.Lbrace, Op: token.AND, X: lit}
		info.Types[u] = tv // a value of type *T
		elem := tv
		elem.Type = p.Elem()
		info.Types[lit] = elem
		return u
	}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for i, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					kv.Key = wrap(kv.Key)
					kv.Value = wrap(kv.Value)
					continue
				}
				lit.Elts[i] = wrap(elt)
			}
			return true
		})
	}
}
