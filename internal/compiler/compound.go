package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// Compound assignment on integers (docs/GO_INTEGERS.md#compound-assignment):
// x op= y for every arithmetic, bitwise and shift operator, on every integer
// type and every addressable target (locals, fields, array and slice
// elements, pointer indirections and map elements). += on strings has its own
// lowering (strings.go). Programs without compound assignment take none of
// these paths, so their C is byte-identical to earlier compilers.

// compoundOps maps each compound assignment token to its binary operator.
var compoundOps = map[token.Token]token.Token{
	token.ADD_ASSIGN:     token.ADD,
	token.SUB_ASSIGN:     token.SUB,
	token.MUL_ASSIGN:     token.MUL,
	token.QUO_ASSIGN:     token.QUO,
	token.REM_ASSIGN:     token.REM,
	token.AND_ASSIGN:     token.AND,
	token.OR_ASSIGN:      token.OR,
	token.XOR_ASSIGN:     token.XOR,
	token.SHL_ASSIGN:     token.SHL,
	token.SHR_ASSIGN:     token.SHR,
	token.AND_NOT_ASSIGN: token.AND_NOT,
}

// isInteger reports whether t is a typed integer type (or a named one).
func isInteger(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUntyped == 0
}

// compoundAssign admits x op= y on an integer target: the same targets as
// ++ and --, and any integer right operand (a shift count of any integer
// type, as Go allows).
func (c *checker) compoundAssign(s *ast.AssignStmt) {
	lhs := ast.Unparen(s.Lhs[0])
	c.variableRule(s, c.info.Types[lhs].Type)
	if _, ok := lhs.(*ast.Ident); !ok {
		c.captureTarget(lhs)
	}
	c.expr(s.Rhs[0])
}

// compoundAssign lowers x op= y with Go's semantics: the target's index and
// pointer operands are evaluated once, before the right operand (as for
// =); then the right operand; then the target is read, combined with the
// right operand exactly as x op y would be (wrapping, truncating division,
// division and modulo by zero and negative shift counts aborting), and
// stored back. A map element m[k] is looked up (and inserted when absent,
// aborting on a nil map) after the right operand, and updated in place.
func (g *generator) compoundAssign(s *ast.AssignStmt) {
	op := compoundOps[s.Tok]
	lhs := ast.Unparen(s.Lhs[0])
	t := g.info.Types[lhs].Type
	if ix, ok := g.isMapIndex(lhs); ok {
		mt := g.mapFor(ix, g.info.Types[ix.X].Type)
		m := g.expr(ix.X)
		k := g.mapKeyAs(ix.Index, mt)
		y := g.expr(s.Rhs[0])
		elem := mt.Type.Elem()
		p := g.temp(ix, types.NewPointer(elem), fmt.Sprintf("(%s *)gosvm_map%d_set(%s,&%s)", g.variable(ix, elem), mt.Index, m, k))
		g.line("*%s = %s;", p, g.compoundValue(s, op, t, "(*"+p+")", y))
		return
	}
	target := g.captureTarget(lhs)
	y := g.expr(s.Rhs[0])
	x := g.temp(lhs, t, target)
	g.line("%s = %s;", target, g.compoundValue(s, op, t, x, y))
}

// compoundValue computes x op y for a compound assignment to a target of
// type t, as the binary operator does (compiler.go, signed.go): u64
// arithmetic truncated by the typed temporary, signed division and right
// shift on the sign-extended value, and the zero-divisor and negative-shift
// traps.
func (g *generator) compoundValue(s *ast.AssignStmt, op token.Token, t types.Type, x, y string) string {
	switch op {
	case token.SHL, token.SHR:
		if !g.nonNegative(s.Rhs[0]) {
			g.line("if (%s < 0) abort();", y)
		}
		if op == token.SHR && isSigned(t) {
			return g.temp(s, t, fmt.Sprintf("((i64)%s >> (%s >= 63 ? 63 : %s))", x, y, y))
		}
		return g.temp(s, t, fmt.Sprintf("%s >= %d ? 0 : ((u64)%s %s %s)", y, intWidth(t), x, op, y))
	case token.QUO, token.REM:
		g.line("if (%s == 0) abort();", y)
		if isSigned(t) {
			helper := "gosvm_quo64"
			if op == token.REM {
				helper = "gosvm_rem64"
			}
			return g.temp(s, t, fmt.Sprintf("%s((i64)%s,(i64)%s)", helper, x, y))
		}
	}
	operator := op.String()
	if op == token.AND_NOT {
		operator = "& ~"
	}
	return g.temp(s, t, fmt.Sprintf("((u64)%s %s (u64)%s)", x, operator, y))
}
