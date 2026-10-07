package compiler

import (
	"fmt"
	"go/ast"
	"go/types"
)

const viewRuntime = `
static inline slice gosvm_view(slice s,u64 lo,u64 hi,u64 maximum) {
    if (lo>hi || hi>maximum || maximum>(u64)s.cap) abort();
    return (slice){s.ptr ? s.ptr+lo : 0,hi-lo,maximum-lo};
}
`

func (g *generator) sliceView(e *ast.SliceExpr) string {
	// All supported views retain byte storage; no copy or allocation occurs.
	if s, ok := g.info.Types[e].Type.(*types.Slice); !ok || !types.Identical(s.Elem(), types.Typ[types.Uint8]) {
		g.fail(e, "slice views require byte/uint8 elements")
	}
	var base string
	if a, ok := arrayValue(g.info.Types[e.X].Type); ok {
		var address string
		p := types.NewPointer(g.info.Types[e.X].Type)
		if actual, isPointer := pointer(g.info.Types[e.X].Type); isPointer {
			p = actual
			address = g.expr(e.X)
		} else {
			address = g.temp(e.X, p, "&("+g.fieldTarget(e.X)+")")
		}
		// Pointer-to-array indirection is checked when building the view, after
		// the explicit index calls; a plain array address has already been saved.
		base = fmt.Sprintf("GOSVM_SLICE((%s)->items,%dULL)", g.checkedPointer(e, p, address), a.Len())
	} else {
		base = g.expr(e.X)
	}
	lo := "0ULL"
	if e.Low != nil {
		lo = g.expr(e.Low)
	}
	hi := "(" + base + ").len"
	if e.High != nil {
		hi = g.expr(e.High)
	}
	maximum := "(" + base + ").cap"
	if e.Max != nil {
		maximum = g.expr(e.Max)
	}
	return g.temp(e, g.info.Types[e].Type, fmt.Sprintf("gosvm_view(%s,(u64)%s,(u64)%s,(u64)%s)", base, lo, hi, maximum))
}
