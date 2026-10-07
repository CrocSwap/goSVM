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
	g.line("for (;;) {")
	if s.Cond != nil {
		c := g.expr(s.Cond)
		g.line("if (!(%s)) break;", c)
	}
	g.block(s.Body)
	if label != "" {
		g.line("%s: ;", label)
	}
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
		if _, ok := g.info.Types[s.Tag].Type.Underlying().(*types.Struct); ok {
			g.fail(s.Tag, "struct switch comparisons unsupported; compare fields explicitly")
		}
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
			if s.Tag != nil {
				if a, ok := g.info.Types[s.Tag].Type.Underlying().(*types.Array); ok {
					g.line("%s=%s_equal(%s,%s);", matched, g.arrayName(s.Tag, a), tag, value)
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
