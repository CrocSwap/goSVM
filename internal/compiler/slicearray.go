package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
)

// Conversions from a slice to an array pointer, (*[N]T)(s), and to an array,
// [N]T(s) (docs/GO_ARRAYS.md#slice-to-array-conversions). Both trap when
// len(s) < N, as Go panics. The pointer aliases the slice's backing array
// (nil for a nil slice, which only N == 0 reaches); the array is a copy of
// the first N elements.

// sliceToArray reports whether the conversion call e converts a slice to an
// array or to a pointer to an array (ptr), and the array type.
func sliceToArray(info *types.Info, e *ast.CallExpr) (a *types.Array, ptr, ok bool) {
	if len(e.Args) != 1 || !info.Types[e.Fun].IsType() {
		return nil, false, false
	}
	if _, isSlice := info.Types[e.Args[0]].Type.Underlying().(*types.Slice); !isSlice {
		return nil, false, false
	}
	to := info.Types[e].Type
	if p, isPointer := to.Underlying().(*types.Pointer); isPointer {
		to, ptr = p.Elem(), true
	}
	a, ok = to.Underlying().(*types.Array)
	return a, ptr, ok
}

// sliceToArrayRule admits (*[N]T)(s) and [N]T(s). go/types has required
// identical element types; the slice and the result must be storable.
func (c *checker) sliceToArrayRule(e *ast.CallExpr, tv types.TypeAndValue) {
	c.expr(e.Args[0])
	c.typeRule(e, tv.Type)
}

// sliceToArray lowers (*[N]T)(s) and [N]T(s): the slice header is read once,
// a short slice aborts, and the result is the slice's data pointer or a copy
// of the array it points to. A nil slice has a null data pointer (non-nil
// empty slices point at the heap base), so N == 0 gives Go's nil pointer.
func (g *generator) sliceToArray(e *ast.CallExpr, t types.Type, a *types.Array, ptr bool) string {
	s := g.temp(e.Args[0], g.info.Types[e.Args[0]].Type, g.expr(e.Args[0]))
	if a.Len() > 0 {
		g.line("if ((u64)%s.len < %dULL) abort();", s, a.Len())
	}
	if ptr {
		return g.temp(e, t, fmt.Sprintf("((%s)(%s.ptr))", g.variable(e, t), s))
	}
	if a.Len() == 0 {
		return g.temp(e, t, "{0}") // no element is read; a nil slice is fine
	}
	return g.temp(e, t, fmt.Sprintf("(*(%s *)(%s.ptr))", g.variable(e, t), s))
}
