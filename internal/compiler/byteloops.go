package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// Hand-written byte compare loops on SBF
// (docs/GO_ARRAYS.md#hand-written-byte-loops).
//
//	for i := k; i < n; i++ { if a[i] != b[i] { ... } }
//
// walks bytes one at a time, about 11 SBF instructions per byte with the
// bounds checks; clang keeps the loop because each byte may return. On SBF
// builds (__bpf__) the loop gets a word-at-a-time path in front of it, and
// the loop itself is emitted unchanged (GOSVM_BYTE_LOOP_WORDS turns the path
// on in host C for tests):
//
//	#if defined(__bpf__) || defined(GOSVM_BYTE_LOOP_WORDS)
//	if (!({ ...; same; }))
//	#endif
//	{ the loop exactly as before }
//
// The path checks that a and b hold bytes k..n-1 (array pointers must be
// non-nil), XORs them a word at a time (a tail shorter than a word as the
// last 8 bytes, overlapping the word before) and skips the loop when they are
// all equal: the loop would then run to the end without entering the if or
// trapping, and its variable is scoped to it. Otherwise the loop runs as
// before, so a difference, a short operand or a nil pointer behaves exactly as
// in Go: the same byte decides, and the same access traps.
//
// The operands must be variables (nothing to evaluate that could have an
// effect or trap) of type []E, [N]E or *[N]E, with E a one-byte integer, and
// k and n constants. Little-endian loads (v |= T(b[i]) << (8*i)) need nothing:
// clang already combines them into one load. Check's corpus test removes the
// path and requires the rest of the C to be unchanged.

// byteOperand is a variable holding bytes: a slice, an array or a pointer to
// an array.
type byteOperand struct {
	id    *ast.Ident
	elem  types.Type
	array int64 // array length; -1 for a slice
	ptr   bool  // *[N]E
}

func (g *generator) byteOperand(e ast.Expr) (byteOperand, bool) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return byteOperand{}, false
	}
	v, ok := g.info.Uses[id].(*types.Var)
	if !ok || v.IsField() {
		return byteOperand{}, false
	}
	o := byteOperand{id: id, array: -1}
	t := v.Type().Underlying()
	if p, ok := t.(*types.Pointer); ok {
		o.ptr, t = true, p.Elem().Underlying()
		if _, ok := t.(*types.Array); !ok {
			return byteOperand{}, false
		}
	}
	switch t := t.(type) {
	case *types.Slice:
		o.elem = t.Elem()
	case *types.Array:
		o.elem, o.array = t.Elem(), t.Len()
	default:
		return byteOperand{}, false
	}
	b, ok := o.elem.Underlying().(*types.Basic)
	if !ok || (b.Kind() != types.Uint8 && b.Kind() != types.Int8) {
		return byteOperand{}, false
	}
	return o, true
}

// operandBytes returns the C pointer to the operand's first byte and the
// condition that it holds n bytes, emitting any temporaries.
func (g *generator) operandBytes(o byteOperand, n int64) (string, string) {
	if o.ptr {
		p := g.expr(o.id)
		return fmt.Sprintf("(%s)->items", p), fmt.Sprintf("%s != 0", p)
	}
	if o.array >= 0 {
		return fmt.Sprintf("(%s).items", g.fieldTarget(o.id)), "1"
	}
	s := g.expr(o.id)
	return s + ".ptr", fmt.Sprintf("(u64)%s.len >= %dULL", s, n)
}

// constInt is e's constant integer value.
func (g *generator) constInt(e ast.Expr) (int64, bool) {
	tv, ok := g.info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(tv.Value)
}

// byteLoopHeader matches `i := k; i < n; i++` with constants 0 <= k < n and
// returns i, k and n.
func (g *generator) byteLoopHeader(s *ast.ForStmt) (types.Object, int64, int64, bool) {
	if s.Init == nil || s.Cond == nil || s.Post == nil {
		return nil, 0, 0, false
	}
	init, ok := s.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 1 || len(init.Rhs) != 1 {
		return nil, 0, 0, false
	}
	id, ok := init.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, 0, 0, false
	}
	i := g.info.Defs[id]
	if i == nil || g.moved[i] {
		return nil, 0, 0, false
	}
	if b, ok := i.Type().Underlying().(*types.Basic); !ok || b.Info()&types.IsInteger == 0 {
		return nil, 0, 0, false
	}
	k, ok := g.constInt(init.Rhs[0])
	if !ok {
		return nil, 0, 0, false
	}
	cond, ok := ast.Unparen(s.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.LSS || !g.isObject(cond.X, i) {
		return nil, 0, 0, false
	}
	n, ok := g.constInt(cond.Y)
	if !ok || k < 0 || k >= n {
		return nil, 0, 0, false
	}
	post, ok := s.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !g.isObject(post.X, i) {
		return nil, 0, 0, false
	}
	return i, k, n, true
}

func (g *generator) isObject(e ast.Expr, o types.Object) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && g.info.Uses[id] == o
}

// indexedBy matches operand[i].
func (g *generator) indexedBy(e ast.Expr, i types.Object) (byteOperand, bool) {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	if !ok || !g.isObject(ix.Index, i) {
		return byteOperand{}, false
	}
	return g.byteOperand(ix.X)
}

// fits reports whether an array operand statically holds n bytes.
func (o byteOperand) fits(n int64) bool { return o.array < 0 || n <= o.array }

// byteLoop emits the word path in front of a matching loop; the caller then
// emits the loop unchanged. The statement expression scopes the temporaries,
// and the counter is restored so the loop gets the same names as without the
// path.
func (g *generator) byteLoop(s *ast.ForStmt) {
	i, k, n, ok := g.byteLoopHeader(s)
	if !ok {
		return
	}
	a, b, ok := g.compareLoop(s, i, n)
	if !ok {
		return
	}
	next := g.next
	g.line("#if defined(__bpf__) || defined(GOSVM_BYTE_LOOP_WORDS)")
	g.line("if (!({")
	pa, fitsA := g.operandBytes(a, n)
	pb, fitsB := g.operandBytes(b, n)
	g.line("boolean gosvm_same = %s && %s;", fitsA, fitsB)
	g.line("if (gosvm_same) {")
	g.line("u64 gosvm_d = 0;")
	word := func(at string) {
		g.line("{ u64 x, y; __builtin_memcpy(&x,%s+%s,8); __builtin_memcpy(&y,%s+%s,8); gosvm_d |= x ^ y; }", pa, at, pb, at)
	}
	switch {
	case n-k < 8:
		g.line("for (u64 j=%dULL;j<%dULL;j++) gosvm_d |= (u8)(%s[j] ^ %s[j]);", k, n, pa, pb)
	case n-k <= 256:
		for j := k; j+8 <= n; j += 8 {
			word(fmt.Sprint(j))
		}
	default:
		g.line("for (u64 j=%dULL;j+8<=%dULL;j+=8)", k, n)
		word("j")
	}
	if n-k >= 8 && (n-k)%8 != 0 {
		// The last 8 bytes, overlapping the previous word: for equality
		// that covers the tail with one more compare.
		word(fmt.Sprint(n - 8))
	}
	g.line("gosvm_same = gosvm_d == 0;")
	g.line("}")
	g.line("gosvm_same;")
	g.line("}))")
	g.line("#endif")
	g.next = next
}

// compareLoop matches a body that is only `if a[i] != b[i] { ... }`, with
// operands that can hold bytes 0..n-1.
func (g *generator) compareLoop(s *ast.ForStmt, i types.Object, n int64) (byteOperand, byteOperand, bool) {
	if len(s.Body.List) != 1 {
		return byteOperand{}, byteOperand{}, false
	}
	branch, ok := s.Body.List[0].(*ast.IfStmt)
	if !ok || branch.Init != nil || branch.Else != nil {
		return byteOperand{}, byteOperand{}, false
	}
	cond, ok := ast.Unparen(branch.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return byteOperand{}, byteOperand{}, false
	}
	a, okA := g.indexedBy(cond.X, i)
	b, okB := g.indexedBy(cond.Y, i)
	if !okA || !okB || !a.fits(n) || !b.fits(n) || !types.Identical(a.elem, b.elem) {
		return byteOperand{}, byteOperand{}, false
	}
	return a, b, true
}
