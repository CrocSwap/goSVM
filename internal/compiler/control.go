package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

func containsContinue(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if b, ok := n.(*ast.BranchStmt); ok && b.Tok == token.CONTINUE {
			found = true
		}
		return true
	})
	return found
}

// Go continue executes a three-clause loop's post statement. A direct C
// continue in our lowered loop would skip it, so target a label before the post.
func (g *generator) loop(s *ast.ForStmt) {
	label := ""
	if containsContinue(s.Body) {
		g.next++
		label = fmt.Sprintf("continue%d", g.next)
	}
	g.continueLabels = append(g.continueLabels, label)
	defer func() { g.continueLabels = g.continueLabels[:len(g.continueLabels)-1] }()
	g.line("{")
	if s.Init != nil {
		g.stmt(s.Init)
	}
	fresh := g.freshLoopVariables(s)
	g.line("for (;;) {")
	if s.Cond != nil {
		c := g.expr(s.Cond)
		g.line("if (!(%s)) break;", c)
	}
	g.block(s.Body)
	if label != "" {
		g.line("%s: ;", label)
	}
	g.copyLoopVariables(s, fresh)
	if s.Post != nil {
		g.stmt(s.Post)
	}
	g.line("}\n}")
}

// Cases may contain calls. Test them in source order and stop evaluating once
// a case matches; default is chosen only after every other case fails.
func (g *generator) switchStatement(s *ast.SwitchStmt) {
	g.line("{")
	if s.Init != nil {
		g.stmt(s.Init)
	}
	tag := "1"
	if s.Tag != nil {
		tag = g.expr(s.Tag)
	}
	g.line("do {")
	var fallback *ast.CaseClause
	for _, statement := range s.Body.List {
		clause := statement.(*ast.CaseClause)
		if clause.List == nil {
			fallback = clause
			continue
		}
		g.next++
		matched := fmt.Sprintf("case%d", g.next)
		g.line("boolean %s=0;", matched)
		for _, test := range clause.List {
			g.line("if (!%s) {", matched)
			value := g.expr(test)
			if s.Tag != nil && (isIface(g.info.Types[s.Tag].Type) || isIface(g.info.Types[test].Type)) {
				g.line("%s=%s;", matched, g.ifaceEqual(test, s.Tag, tag, test, value))
			} else if s.Tag != nil {
				if a, ok := g.info.Types[s.Tag].Type.Underlying().(*types.Array); ok {
					g.line("%s=%s_equal(%s,%s);", matched, g.arrayName(s.Tag, a), tag, value)
				} else if isString(g.info.Types[s.Tag].Type) {
					g.line("%s=gosvm_str_eq(%s,%s);", matched, tag, value)
				} else {
					g.line("%s=(%s==%s);", matched, tag, value)
				}
			} else {
				g.line("%s=(%s==%s);", matched, tag, value)
			}
			g.line("}")
		}
		g.line("if (%s) {", matched)
		for _, statement := range clause.Body {
			g.stmt(statement)
		}
		g.line("break;\n}")
	}
	if fallback != nil {
		g.line("{")
		for _, statement := range fallback.Body {
			g.stmt(statement)
		}
		g.line("}")
	}
	g.line("} while (0);\n}")
}

// rangeLoop lowers a Go 1.22 range loop that Check admitted. The operand is
// evaluated once before the loop and a hidden u64 counter drives iteration,
// so assigning to the iteration variables never changes it. Iteration
// variables are declared inside the C loop body: fresh in every iteration.
//
//   - integers: the count is fixed at loop start; a negative count runs
//     no iterations.
//   - arrays with a value variable: the array is copied once (Go ranges over
//     a copy); pointers to arrays are read through, checking nil per element.
//   - arrays without a value variable: len is constant and the array is never
//     loaded; only calls on the operand path run (no copy, no nil check).
//   - slices: the header (and so the length) is fixed at loop start;
//     elements are read from the shared storage in each iteration.
func (g *generator) rangeLoop(s *ast.RangeStmt) {
	label := ""
	if containsContinue(s.Body) {
		g.next++
		label = fmt.Sprintf("continue%d", g.next)
	}
	g.continueLabels = append(g.continueLabels, label)
	defer func() { g.continueLabels = g.continueLabels[:len(g.continueLabels)-1] }()
	g.line("{")
	xt := g.info.Types[s.X].Type
	key, value := rangeTarget(s.Key), rangeTarget(s.Value)
	var bound string
	var element func(string) string
	str, next, r := "", "", ""
	if isString(xt) {
		// The string is evaluated once; each iteration decodes the rune at
		// the counter and steps to the next one (U+FFFD, width 1, for an
		// invalid encoding), whether or not a value variable is present.
		str = g.expr(s.X)
		g.next++
		next, r = fmt.Sprintf("next%d", g.next), fmt.Sprintf("rune%d", g.next)
		element = func(string) string { return r }
	} else if a, ok := arrayValue(xt); ok {
		bound = fmt.Sprintf("%dULL", a.Len())
		if value == nil {
			g.rangeOperandCalls(s.X)
		} else if p, ok := pointer(xt); ok {
			x := g.expr(s.X)
			element = func(i string) string { return fmt.Sprintf("%s->items[%s]", g.checkedPointer(s.X, p, x), i) }
		} else {
			x := g.expr(s.X) // an array-valued expression is a fresh by-value copy
			element = func(i string) string { return fmt.Sprintf("%s.items[%s]", x, i) }
		}
	} else if _, ok := xt.Underlying().(*types.Slice); ok {
		x := g.expr(s.X)
		bound = "(u64)" + x + ".len"
		element = func(i string) string { return fmt.Sprintf("%s.ptr[%s]", x, i) }
		if !isByteSlice(xt) {
			elem := g.variable(s.X, xt.(*types.Slice).Elem())
			element = func(i string) string { return fmt.Sprintf("((%s *)%s.ptr)[%s]", elem, x, i) }
		}
	} else if _, ok := xt.Underlying().(*types.Basic); ok {
		n := g.expr(s.X)
		bound = "(u64)" + n
		if !g.nonNegative(s.X) {
			// A negative signed count runs no iterations.
			bound = fmt.Sprintf("(%s < 0 ? 0ULL : (u64)%s)", n, n)
		}
	} else {
		g.unchecked(s.X)
	}
	g.next++
	counter := fmt.Sprintf("range%d", g.next)
	if str != "" {
		g.line("for (u64 %s=0,%s=0;%s<(u64)%s.len;%s=%s) {", counter, next, counter, str, counter, next)
		g.line("i64 %s;", r)
		g.line("%s = gosvm_decoderune(%s,%s,&%s);", next, str, counter, r)
	} else {
		g.line("for (u64 %s=0;%s<%s;%s++) {", counter, counter, bound, counter)
	}
	object := func(id *ast.Ident) types.Object {
		if s.Tok == token.DEFINE {
			return g.info.Defs[id]
		}
		return g.info.Uses[id]
	}
	assign := func(id *ast.Ident, v string) {
		o := object(id)
		if conv := g.ifaces.byExpr[id]; conv != nil && conv.rng {
			v = g.boxValue(id, conv, v) // an element assigned to an interface variable
		}
		if s.Tok == token.DEFINE && g.moved[o] {
			g.declareMoved(id, o, v)
		} else if s.Tok == token.DEFINE {
			g.line("%s %s = %s;", g.variable(id, o.Type()), g.name(o), v)
		} else {
			g.line("%s = %s;", g.name(o), v)
		}
	}
	// The element (or key) type; an interface variable converts it.
	elementType := func(id *ast.Ident) types.Type {
		if conv := g.ifaces.byExpr[id]; conv != nil && conv.rng {
			return conv.From
		}
		return object(id).Type()
	}
	read := ""
	if value != nil {
		// Read the element before any store, as an assignment would.
		read = g.temp(value, elementType(value), element(counter))
	}
	if key != nil {
		assign(key, "("+g.variable(key, elementType(key))+")"+counter)
	}
	if value != nil {
		assign(value, read)
	}
	g.block(s.Body)
	if label != "" {
		g.line("%s: ;", label)
	}
	g.line("}\n}")
}

// rangeOperandCalls runs the calls on an index-only array range operand
// without loading the array or dereferencing a pointer, as Go does.
func (g *generator) rangeOperandCalls(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
	case *ast.ParenExpr:
		g.rangeOperandCalls(e.X)
	case *ast.SelectorExpr:
		g.rangeOperandCalls(e.X)
	case *ast.StarExpr:
		g.rangeOperandCalls(e.X)
	case *ast.CallExpr:
		if g.info.Types[e.Fun].IsType() {
			g.unchecked(e)
		}
		g.expr(e)
	default:
		g.unchecked(e)
	}
}
