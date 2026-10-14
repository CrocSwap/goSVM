package compiler

import (
	"go/ast"
	"go/types"
)

// Checker rules for function values, closures and defer
// (docs/GO_FUNCTIONS.md).

// Diagnostics for function values and defer.
const (
	ifaceMethodValueRule = "method values of interface types unsupported; use a function literal that calls the method"
	intrinsicValueRule   = "%s functions cannot be used as values; call them in a function literal"
	deferIntrinsicRule   = "defer of %s calls unsupported; defer a function literal that makes the call"
)

// funcValue checks an expression that makes a function value.
func (c *checker) funcValue(e ast.Expr, t *FuncTarget) {
	switch t.Kind {
	case TargetLiteral:
		c.funcLit(e.(*ast.FuncLit))
	case TargetMethodValue:
		c.methodValue(e.(*ast.SelectorExpr), t)
	default:
		c.typeRule(e, c.info.Types[e].Type)
	}
}

// funcLit checks a function literal: its signature, then its body with its
// own results and no enclosing loop.
func (c *checker) funcLit(lit *ast.FuncLit) {
	sig := c.info.Types[lit].Type.(*types.Signature)
	if sig.Variadic() {
		c.fail(lit, "variadics are unsupported")
	}
	c.typeRule(lit, sig)
	for _, tuple := range []*types.Tuple{sig.Params(), sig.Results()} {
		for i := 0; i < tuple.Len(); i++ {
			c.variableRule(lit, tuple.At(i).Type())
		}
	}
	if sig.Results().Len() > 1 {
		c.lookupResult(lit, sig.Results())
	}
	current, loops := c.current, c.loops
	c.current, c.loops = sig.Results(), 0
	c.block(lit.Body)
	c.current, c.loops = current, loops
}

// methodValue checks x.M: the receiver is evaluated (its address taken for
// a pointer-receiver method, or a copy made, dereferencing a pointer) when
// the value is made.
func (c *checker) methodValue(e *ast.SelectorExpr, t *FuncTarget) {
	x := c.info.Types[e.X].Type
	receiver := t.Func.Type().(*types.Signature).Recv().Type()
	if t.PointerReceiver {
		if _, isPointer := pointer(x); isPointer {
			c.expr(e.X)
		} else {
			c.address(e.X)
			c.typeRule(e.X, receiver)
		}
	} else {
		if p, isPointer := pointer(x); isPointer {
			c.expr(e.X)
			c.checkedPointer(e.X, p)
		} else {
			c.expr(e.X)
		}
		c.typeRule(e.X, receiver)
		if (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(receiver) != 0 && zeroSizePart(receiver) {
			c.fail(e, "method values must not bind %s: it contains zero-length arrays or empty structs", receiver)
		}
	}
	c.typeRule(e, c.info.Types[e].Type)
}

// indirectCall checks a call through a function value: the value, then the
// arguments.
func (c *checker) indirectCall(e *ast.CallExpr, tv types.TypeAndValue) {
	c.expr(e.Fun)
	c.callValues(e)
	if tup, ok := tv.Type.(*types.Tuple); ok && tup.Len() == 0 {
		return
	}
	c.typeRule(e, tv.Type)
}

// deferStmt checks a defer statement: the deferred call is checked like any
// call; calls into packages without Go bodies are rejected.
func (c *checker) deferStmt(s *ast.DeferStmt) {
	if sel, ok := c.callFun(s.Call.Fun).(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			if pkg, ok := c.info.Uses[id].(*types.PkgName); ok {
				if path := pkg.Imported().Path(); path == "gosvm/solana" || path == bitsPath {
					c.fail(s, deferIntrinsicRule, path)
				}
			}
		}
	}
	c.expr(s.Call)
}

// declareFuncResults declares the multiple-result tuples of literals and of
// signatures called through function values.
func (c *checker) declareFuncResults() {
	for _, cl := range c.funcs.closures {
		cl := cl
		c.within(cl.Owner.Decl, func() { c.declareResults(cl.Lit, cl.Target.Signature.Results()) })
	}
	for _, class := range c.funcs.classes {
		c.declareResults(posNode(class.Site), class.Signature.Results())
	}
}

// intrinsicValue reports the package of a gosvm/solana or math/bits
// function named by e outside a call, or "".
func (c *checker) intrinsicValue(e *ast.SelectorExpr) string {
	id, ok := e.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if _, ok := c.info.Uses[id].(*types.PkgName); !ok {
		return ""
	}
	if fn, ok := c.info.Uses[e.Sel].(*types.Func); ok && intrinsic(fn) {
		return fn.Pkg().Path()
	}
	return ""
}
