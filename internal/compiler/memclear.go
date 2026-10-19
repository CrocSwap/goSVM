package compiler

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// Zeroing loops (docs/GO_ALLOCATION.md#zeroing-loops). Two loop forms that
// only store zero into every element of a slice from some index to its end
// are emitted, on the modern SBF profiles, as one memset of those bytes,
// which the memory routines hand to sol_memset_ (memops.go):
//
//	for i := range s { s[i] = 0 }                  // gc's own memclr idiom
//	for j := e; j < uint64(len(s)); j++ { s[j] = 0 } // also j < len(s); j uint64 or uint
//
// s is a variable of slice type whose elements are integers or bools, i and j
// are declared by the loop, and the body is exactly that one assignment of
// the constant zero (false). Neither loop can trap: every index is below
// len(s), which the body cannot change, and an unsigned j never wraps before
// len(s). The three-clause loop's e is evaluated once, as before, and stores
// nothing when it is at least len(s). Host C and target-less legacy builds
// compile the original loop (the #else branch), so their code is unchanged.

// zeroLoopSlice returns x as a variable of slice type and its element type
// when the element's zero value is all zero bytes (integers and bools).
func (g *generator) zeroLoopSlice(x ast.Expr) (*ast.Ident, types.Type, bool) {
	id, ok := x.(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	if _, ok := g.info.Uses[id].(*types.Var); !ok {
		return nil, nil, false
	}
	s, ok := g.info.Types[id].Type.Underlying().(*types.Slice)
	if !ok {
		return nil, nil, false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	if !ok || b.Info()&(types.IsInteger|types.IsBoolean) == 0 {
		return nil, nil, false
	}
	return id, s.Elem(), true
}

// zeroStore reports whether body is exactly `x[i] = 0` (or false) for the
// slice variable x and the loop variable i.
func (g *generator) zeroStore(body *ast.BlockStmt, x *ast.Ident, i types.Object) bool {
	if len(body.List) != 1 {
		return false
	}
	a, ok := body.List[0].(*ast.AssignStmt)
	if !ok || a.Tok != token.ASSIGN || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
		return false
	}
	ix, ok := a.Lhs[0].(*ast.IndexExpr)
	if !ok {
		return false
	}
	xs, ok := ix.X.(*ast.Ident)
	if !ok || g.info.Uses[xs] != g.info.Uses[x] {
		return false
	}
	is, ok := ix.Index.(*ast.Ident)
	if !ok || g.info.Uses[is] != i {
		return false
	}
	v := g.info.Types[a.Rhs[0]].Value
	if v == nil {
		return false
	}
	switch v.Kind() {
	case constant.Int:
		return constant.Sign(v) == 0
	case constant.Bool:
		return !constant.BoolVal(v)
	}
	return false
}

// zeroRange emits `for i := range s { s[i] = 0 }` as a memset on SBF and
// returns true, or returns false for any other range loop.
func (g *generator) zeroRange(s *ast.RangeStmt) bool {
	if !g.memory || s.Tok != token.DEFINE || rangeTarget(s.Value) != nil || (s.Value != nil && !isBlank(s.Value)) {
		return false
	}
	key := rangeTarget(s.Key)
	if key == nil {
		return false
	}
	i := g.info.Defs[key]
	if i == nil || g.moved[i] {
		return false
	}
	x, elem, ok := g.zeroLoopSlice(s.X)
	if !ok || !g.zeroStore(s.Body, x, i) {
		return false
	}
	// The memset branch reuses the temporary numbers the loop takes, so the
	// #else branch is exactly the loop earlier compilers emitted.
	next := g.next
	g.line("#ifdef GOSVM_MEMORY_SYSCALL_SET")
	g.line("{")
	v := g.expr(x)
	g.line("(void)memset(%s.ptr,0,(u64)%s.len*sizeof(%s));", v, v, g.variable(x, elem))
	g.line("}")
	g.line("#else")
	g.next = next
	g.rangeLoopPlain(s)
	g.line("#endif")
	return true
}

// zeroFor emits `for j := e; j < uint64(len(s)); j++ { s[j] = 0 }` as a
// memset on SBF and returns true, or returns false for any other loop.
func (g *generator) zeroFor(s *ast.ForStmt) bool {
	if !g.memory || s.Init == nil || s.Cond == nil || s.Post == nil {
		return false
	}
	init, ok := s.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 1 || len(init.Rhs) != 1 {
		return false
	}
	jd, ok := init.Lhs[0].(*ast.Ident)
	if !ok {
		return false
	}
	j := g.info.Defs[jd]
	if j == nil || g.moved[j] {
		return false
	}
	jt, ok := j.Type().Underlying().(*types.Basic)
	if !ok || (jt.Kind() != types.Uint64 && jt.Kind() != types.Uint) {
		return false
	}
	post, ok := s.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC {
		return false
	}
	if pj, ok := post.X.(*ast.Ident); !ok || g.info.Uses[pj] != j {
		return false
	}
	cond, ok := s.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LSS {
		return false
	}
	if cj, ok := cond.X.(*ast.Ident); !ok || g.info.Uses[cj] != j {
		return false
	}
	n := ast.Unparen(cond.Y)
	if conv, ok := n.(*ast.CallExpr); ok && g.info.Types[conv.Fun].IsType() && len(conv.Args) == 1 {
		if b, ok := g.info.Types[conv.Fun].Type.Underlying().(*types.Basic); !ok || (b.Kind() != types.Uint64 && b.Kind() != types.Uint) {
			return false
		}
		n = ast.Unparen(conv.Args[0])
	}
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if fn, ok := ast.Unparen(call.Fun).(*ast.Ident); !ok || g.info.Uses[fn] != types.Universe.Lookup("len") {
		return false
	}
	x, elem, ok := g.zeroLoopSlice(ast.Unparen(call.Args[0]))
	if !ok || !g.zeroStore(s.Body, x, j) {
		return false
	}
	g.continueLabels = append(g.continueLabels, "")
	defer func() { g.continueLabels = g.continueLabels[:len(g.continueLabels)-1] }()
	g.line("{")
	g.stmt(s.Init)
	next := g.next
	g.line("#ifdef GOSVM_MEMORY_SYSCALL_SET")
	g.line("{")
	v := g.expr(x)
	size := g.variable(x, elem)
	start := g.name(j)
	g.line("if ((u64)%s < (u64)%s.len) (void)memset(%s.ptr+(u64)%s*sizeof(%s),0,((u64)%s.len-(u64)%s)*sizeof(%s));", start, v, v, start, size, v, start, size)
	g.line("}")
	g.line("#else")
	g.next = next
	g.loopBody(s, "")
	g.line("#endif")
	g.line("}")
	return true
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}
