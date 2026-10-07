package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Multiple Go results travel in an internal C aggregate; they are not wire data.
type resultType struct {
	typ  *types.Tuple
	name string
}

func (g *generator) emitResults(node ast.Node, t *types.Tuple) {
	if t.Len() < 2 {
		return
	}
	for _, r := range g.results {
		if types.Identical(r.typ, t) {
			return
		}
	}
	g.next++
	name := fmt.Sprintf("results%d", g.next)
	g.results = append(g.results, resultType{t, name})
	g.line("typedef struct {")
	for i := 0; i < t.Len(); i++ {
		g.line("%s r%d;", g.variable(node, t.At(i).Type()), i)
	}
	g.line("} %s;", name)
}
func (g *generator) resultName(node ast.Node, t *types.Tuple) string {
	for _, r := range g.results {
		if types.Identical(r.typ, t) {
			return r.name
		}
	}
	g.fail(node, "result type was not declared before use")
	return ""
}
func (g *generator) expressionValues(expressions []ast.Expr) []string {
	return g.expressionValuesAs(expressions, nil)
}
func (g *generator) exprAs(e ast.Expr, expected types.Type) string {
	if g.info.Types[e].IsNil() {
		if _, ok := expected.(*types.Slice); ok {
			return "(slice){0}"
		}
	}
	return g.expr(e)
}
func (g *generator) callValues(call *ast.CallExpr) []string {
	var expected []types.Type
	if sig, ok := g.info.Types[call.Fun].Type.(*types.Signature); ok {
		for i := 0; i < sig.Params().Len(); i++ {
			expected = append(expected, sig.Params().At(i).Type())
		}
	}
	return g.expressionValuesAs(call.Args, expected)
}
func (g *generator) expressionValuesAs(expressions []ast.Expr, expected []types.Type) []string {
	var values []string
	for i, expression := range expressions {
		var want types.Type
		if i < len(expected) {
			want = expected[i]
		}
		value := g.exprAs(expression, want)
		if t, ok := g.info.Types[expression].Type.(*types.Tuple); ok && t.Len() > 1 {
			for i := 0; i < t.Len(); i++ {
				values = append(values, fmt.Sprintf("%s.r%d", value, i))
			}
		} else {
			values = append(values, value)
		}
	}
	return values
}
func (g *generator) multiReturn(s *ast.ReturnStmt) {
	expected := make([]types.Type, g.currentResults.Len())
	for i := range expected {
		expected[i] = g.currentResults.At(i).Type()
	}
	values := g.expressionValuesAs(s.Results, expected)
	fields := make([]string, len(values))
	for i, value := range values {
		fields[i] = fmt.Sprintf(".r%d=%s", i, value)
	}
	g.line("return (%s){%s};", g.resultName(s, g.currentResults), strings.Join(fields, ","))
}

// captureTarget evaluates the left-hand operands before any RHS. Bounds checks
// happen at the store, in left-to-right order, so an earlier store can precede a
// later failing index just as in native Go. Internal C pointers never escape.
func (g *generator) captureTarget(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		if id.Name == "_" {
			return ""
		}
		return g.name(g.info.Uses[id])
	}
	g.next++
	targetPointer := fmt.Sprintf("target%d", g.next)
	switch e := e.(type) {
	case *ast.SelectorExpr:
		return g.fieldTarget(e)
	case *ast.StarExpr:
		return g.fieldTarget(e)
	case *ast.IndexExpr:
		if _, ok := pointer(g.info.Types[e.X].Type); ok {
			if a, ok := arrayValue(g.info.Types[e.X].Type); ok {
				base, index := g.arrayPointer(e.X), g.expr(e.Index)
				return fmt.Sprintf("*%s_at(%s,%s)", g.arrayName(e, a), base, index)
			}
		}
		if a, ok := g.info.Types[e.X].Type.Underlying().(*types.Array); ok {
			target := g.fieldTarget(e.X)
			g.line("%s *%s=&(%s);", g.arrayName(e, a), targetPointer, target)
			index := g.expr(e.Index)
			return fmt.Sprintf("*%s_at(%s,%s)", g.arrayName(e, a), targetPointer, index)
		}
		target, index := g.expr(e.X), g.expr(e.Index)
		return fmt.Sprintf("*at(%s,%s)", target, index)
	default:
		g.fail(e, "unsupported assignment target")
	}
	return ""
}
func (g *generator) assignMany(node ast.Node, lhs []ast.Expr, rhs []ast.Expr, tok token.Token) {
	targets := make([]string, len(lhs))
	defs := make([]types.Object, len(lhs))
	for i, left := range lhs {
		if id, ok := left.(*ast.Ident); ok && tok == token.DEFINE {
			defs[i] = g.info.Defs[id]
			if id.Name == "_" {
				continue
			}
			if defs[i] != nil {
				continue
			}
		}
		targets[i] = g.captureTarget(left)
	}
	expected := make([]types.Type, len(lhs))
	for i, left := range lhs {
		expected[i] = g.info.Types[left].Type
		if defs[i] != nil {
			expected[i] = defs[i].Type()
		} else if id, ok := left.(*ast.Ident); ok && g.info.Uses[id] != nil {
			expected[i] = g.info.Uses[id].Type()
		}
	}
	values := g.expressionValuesAs(rhs, expected)
	if len(values) != len(lhs) {
		g.fail(node, "assignment value count mismatch")
	}
	for i, value := range values {
		if defs[i] != nil {
			if id, ok := lhs[i].(*ast.Ident); ok && id.Name == "_" {
				continue
			}
			if _, ok := defs[i].Type().Underlying().(*types.Array); ok {
				g.names[defs[i]] = value
				continue
			}
			g.line("%s %s=%s;", g.variable(lhs[i], defs[i].Type()), g.name(defs[i]), value)
		} else if targets[i] != "" {
			g.line("%s=%s;", targets[i], value)
		}
	}
}
func (g *generator) multiDeclaration(v *ast.ValueSpec) {
	if len(v.Values) > 0 {
		lhs := make([]ast.Expr, len(v.Names))
		for i, id := range v.Names {
			lhs[i] = id
		}
		g.assignMany(v, lhs, v.Values, token.DEFINE)
		return
	}
	for _, id := range v.Names {
		if id.Name == "_" {
			continue
		}
		o := g.info.Defs[id]
		if isContext(o.Type()) {
			g.fail(v, "Context must come from the entrypoint, not a zero value")
		}
		g.line("%s %s={0};", g.variable(v, o.Type()), g.name(o))
	}
}
