package compiler

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// packedStoreWidth recognizes only a complete, side-effect-free little-endian
// encoding loop over a scalar passed by value. It never removes the original
// loop: the fast path requires the entire range to be valid, and the fallback
// preserves byte-by-byte writes before a panic, including offset wraparound.
func (g *generator) packedStoreWidth(fn *ast.FuncDecl) uint64 {
	if !g.memoryWords { // Keep legacy/SDK-1 code generation unchanged.
		return 0
	}
	sig := g.info.Defs[fn.Name].Type().(*types.Signature)
	if sig.Recv() != nil || sig.Variadic() || sig.Params().Len() != 3 || sig.Results().Len() != 0 || len(fn.Body.List) != 1 {
		return 0
	}
	p := sig.Params()
	if !types.Identical(p.At(0).Type(), types.NewSlice(types.Typ[types.Uint8])) || !types.Identical(p.At(1).Type(), types.Typ[types.Uint64]) {
		return 0
	}
	var width uint64
	switch {
	case types.Identical(p.At(2).Type(), types.Typ[types.Uint32]):
		width = 4
	case types.Identical(p.At(2).Type(), types.Typ[types.Uint64]):
		width = 8
	default:
		return 0
	}
	loop, ok := fn.Body.List[0].(*ast.ForStmt)
	if !ok || len(loop.Body.List) != 1 {
		return 0
	}
	init, ok := loop.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 1 || len(init.Rhs) != 1 || !g.packedConstant(init.Rhs[0], 0) {
		return 0
	}
	index, ok := init.Lhs[0].(*ast.Ident)
	if !ok {
		return 0
	}
	j, ok := g.info.Defs[index].(*types.Var)
	if !ok || !types.Identical(j.Type(), types.Typ[types.Uint64]) {
		return 0
	}
	cond, ok := loop.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.LSS || !g.packedVariable(cond.X, j) || !g.packedConstant(cond.Y, width) {
		return 0
	}
	post, ok := loop.Post.(*ast.IncDecStmt)
	if !ok || post.Tok != token.INC || !g.packedVariable(post.X, j) {
		return 0
	}
	store, ok := loop.Body.List[0].(*ast.AssignStmt)
	if !ok || store.Tok != token.ASSIGN || len(store.Lhs) != 1 || len(store.Rhs) != 1 {
		return 0
	}
	target, ok := store.Lhs[0].(*ast.IndexExpr)
	if !ok || !g.packedVariable(target.X, p.At(0)) {
		return 0
	}
	offset, ok := target.Index.(*ast.BinaryExpr)
	if !ok || offset.Op != token.ADD || !g.packedVariable(offset.X, p.At(1)) || !g.packedVariable(offset.Y, j) {
		return 0
	}
	cast, ok := store.Rhs[0].(*ast.CallExpr)
	if !ok || len(cast.Args) != 1 || cast.Ellipsis.IsValid() || !g.info.Types[cast.Fun].IsType() || !types.Identical(g.info.Types[cast].Type, types.Typ[types.Uint8]) {
		return 0
	}
	shift, ok := cast.Args[0].(*ast.BinaryExpr)
	if !ok || shift.Op != token.SHR || !g.packedVariable(shift.X, p.At(2)) {
		return 0
	}
	bits, ok := shift.Y.(*ast.ParenExpr)
	if !ok {
		return 0
	}
	mul, ok := bits.X.(*ast.BinaryExpr)
	if !ok || mul.Op != token.MUL || !g.packedConstant(mul.X, 8) || !g.packedVariable(mul.Y, j) {
		return 0
	}
	return width
}

func (g *generator) packedVariable(e ast.Expr, want types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && g.info.Uses[id] == want
}

func (g *generator) packedConstant(e ast.Expr, want uint64) bool {
	v := g.info.Types[e].Value
	if v == nil || v.Kind() != constant.Int {
		return false
	}
	n, ok := constant.Uint64Val(v)
	return ok && n == want
}

func (g *generator) emitPackedStore(fn *ast.FuncDecl) bool {
	width := g.packedStoreWidth(fn)
	if width == 0 {
		return false
	}
	p := g.info.Defs[fn.Name].Type().(*types.Signature).Params()
	b, offset, value := g.name(p.At(0)), g.name(p.At(1)), g.name(p.At(2))
	g.line("{")
	g.line("#if defined(__BYTE_ORDER__) && defined(__ORDER_LITTLE_ENDIAN__) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__")
	// Builtin memcpy has defined unaligned and aliasing semantics. LLVM may
	// select a word store or legal byte stores; no typed unaligned cast occurs.
	g.line("if (%s <= (u64)%s.len && %dULL <= (u64)%s.len-%s) {", offset, b, width, b, offset)
	g.line("__builtin_memcpy(%s.ptr+%s,&%s,%d); return;", b, offset, value, width)
	g.line("}\n#endif")
	g.block(fn.Body)
	g.line("}")
	return true
}
