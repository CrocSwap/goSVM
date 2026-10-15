package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Aggregate parameters and the SBF backend. Clang passes a C struct
// parameter (a string, slice, interface, struct or array value) byval: the
// IR promises the callee its own copy, so the caller may pass a pointer to
// its own variable. The SBF backend lowers byval as that plain pointer and
// makes no copy, so a callee that writes its parameter (s = s[1:], p.f = x,
// &p) can change the caller's variable. On a host target the same C is
// correct (small structs travel in registers), so only SBF executions
// differ. To keep every function from writing byval memory, a function that
// writes an aggregate parameter receives it under another name and copies
// it into a local at entry (the local is the Go variable). Parameters moved
// to the heap are copied by their own prologue already.
//
// This is applied to the standard-library overlay only (copiesByvalParams):
// applying it to program code too would change the C and the ELFs of
// existing programs that write aggregate parameters (64 entries of the
// frozen check corpus, and the full-swap ELFs), which is a decision for the
// project (docs/GO_STDLIB.md#aggregate-parameters-on-sbf).

// byvalCopiesProgramCode extends the parameter copies to program code. It
// is false so that programs keep their C and ELFs byte for byte.
const byvalCopiesProgramCode = false

// copiesByvalParams reports whether fn's body copies the aggregate
// parameters it writes.
func copiesByvalParams(fn *types.Func) bool {
	return byvalCopiesProgramCode || fn.Pkg() != nil && stdlibPackage(fn.Pkg().Path())
}

// byvalAggregate reports whether values of t are C structs.
func byvalAggregate(t types.Type) bool {
	if isString(t) || isIface(t) || isEmptyStruct(t) {
		return true
	}
	switch t.Underlying().(type) {
	case *types.Slice, *types.Struct, *types.Array:
		return true
	}
	return false
}

// writtenAggregates lists the aggregate parameters among params that body
// may write: assigned (also as the root of a field or element target),
// incremented, ranged into, or addressed (explicitly, or by a method call
// with a pointer receiver).
func writtenAggregates(info *types.Info, body *ast.BlockStmt, params []*types.Var) []types.Object {
	if body == nil {
		return nil
	}
	candidates := map[types.Object]bool{}
	for _, p := range params {
		if p != nil && p.Name() != "_" && byvalAggregate(p.Type()) {
			candidates[p] = true
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	written := map[types.Object]bool{}
	var root func(ast.Expr)
	root = func(e ast.Expr) {
		switch e := ast.Unparen(e).(type) {
		case *ast.Ident:
			if o := info.Uses[e]; candidates[o] {
				written[o] = true
			}
		case *ast.SelectorExpr:
			// A field of a struct value is part of it; a field reached
			// through a pointer is not.
			if _, ok := info.Types[e.X].Type.Underlying().(*types.Struct); ok {
				root(e.X)
			}
		case *ast.IndexExpr:
			// An element of an array value is part of it; slice, string
			// and map elements are not (the parameter is their header).
			if _, ok := info.Types[e.X].Type.Underlying().(*types.Array); ok {
				root(e.X)
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				for _, lhs := range n.Lhs {
					root(lhs)
				}
			}
		case *ast.IncDecStmt:
			root(n.X)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				root(n.Key)
				if n.Value != nil {
					root(n.Value)
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				root(n.X)
			}
		case *ast.SelectorExpr:
			// x.M with a pointer receiver takes &x.
			if s := info.Selections[n]; s != nil && s.Kind() == types.MethodVal {
				if fn, ok := s.Obj().(*types.Func); ok {
					if _, ptr := fn.Type().(*types.Signature).Recv().Type().(*types.Pointer); ptr {
						if _, isPtr := s.Recv().(*types.Pointer); !isPtr {
							root(n.X)
						}
					}
				}
			}
		case *ast.SliceExpr:
			// Slicing an array parameter takes its address.
			if _, ok := info.Types[n.X].Type.Underlying().(*types.Array); ok {
				root(n.X)
			}
		}
		return true
	})
	var out []types.Object
	for _, p := range params {
		if p != nil && written[p] {
			out = append(out, p)
		}
	}
	return out
}

// copiedParameter is the C name a copied parameter arrives under.
func (g *generator) copiedParameter(o types.Object) string { return g.rawName(o) + "_in" }

// noteCopiedParams records, for a function or literal, the written
// aggregate parameters that are not moved to the heap.
func (g *generator) noteCopiedParams(node ast.Node, body *ast.BlockStmt, params []*types.Var) {
	for _, o := range writtenAggregates(g.info, body, params) {
		if !g.moved[o] {
			g.copied[o] = true
			g.copiedParams[node] = append(g.copiedParams[node], o)
		}
	}
}

// copyPrologue copies a function's copied parameters into their locals.
func (g *generator) copyPrologue(node ast.Node) {
	for _, o := range g.copiedParams[node] {
		g.line("%s %s = %s;", g.variable(node, o.Type()), g.rawName(o), g.copiedParameter(o))
	}
}
