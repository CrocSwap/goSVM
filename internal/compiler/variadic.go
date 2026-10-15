package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Variadic functions (docs/GO_FUNCTIONS.md#variadic-functions). A declared
// function or method may have a final ...T parameter, which is a []T inside
// the function, as in Go. Check lowers every call that does not end in
// "..." into one that does, so later passes, the emitter and alternative
// backends see only calls that pass the final parameter as a slice:
//
//	f(a, x, y)  becomes  f(a, []T{x, y}...)   // a new slice: len == cap == 2
//	f(a)        becomes  f(a, nil...)         // a nil slice, as in Go
//
// The arguments are evaluated in the same order, and the slice literal is
// allocated like any slice literal (docs/GO_ALLOCATION.md): Go's escape
// analysis would put a non-escaping one on the stack, so gc may allocate
// less (heap budgets only). Function values, function literals and
// interface methods with variadic signatures stay unsupported.

// lowerVariadicCalls rewrites, in place, every call of a declared variadic
// function or method in files that does not use "...". Calls into the SDK
// and math/bits (which have no Go bodies) are left as they are.
func lowerVariadicCalls(info *types.Info, files []*ast.File) {
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() {
				return true
			}
			fn := variadicCallee(info, call)
			if fn == nil {
				return true
			}
			sig, ok := info.Types[call.Fun].Type.(*types.Signature)
			if !ok || !sig.Variadic() {
				return true
			}
			fixed := sig.Params().Len() - 1
			if len(call.Args) < fixed {
				return true // a single multiple-value argument: rejected by Check
			}
			last := sig.Params().At(fixed).Type()
			// A value-mode TypeAndValue to copy: the callee expression's.
			tv := info.Types[call.Fun]
			tv.Value = nil
			var arg ast.Expr
			if extra := call.Args[fixed:]; len(extra) == 0 {
				id := &ast.Ident{NamePos: call.Rparen, Name: "nil"}
				info.Uses[id] = types.Universe.Lookup("nil")
				tv.Type = types.Typ[types.UntypedNil]
				info.Types[id] = tv
				arg = id
			} else {
				lit := &ast.CompositeLit{Lbrace: extra[0].Pos(), Elts: append([]ast.Expr(nil), extra...), Rbrace: extra[len(extra)-1].End()}
				tv.Type = last
				info.Types[lit] = tv
				arg = lit
			}
			call.Args = append(call.Args[:fixed:fixed], arg)
			call.Ellipsis = call.Rparen
			return true
		})
	}
}

// variadicCallee is the declared function or method a call runs directly
// when it is variadic and has a Go body (not gosvm/solana or math/bits).
func variadicCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	case *ast.IndexExpr:
		id = indexIdent(fun.X)
	case *ast.IndexListExpr:
		id = indexIdent(fun.X)
	}
	if id == nil {
		return nil
	}
	fn, ok := info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || intrinsic(fn) {
		return nil
	}
	if !fn.Type().(*types.Signature).Variadic() {
		return nil
	}
	return fn
}

func indexIdent(e ast.Expr) *ast.Ident {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

var _ = token.NoPos
