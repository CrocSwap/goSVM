package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"
)

// signedPrelude is emitted only for programs that mention int8, int16 or
// int32, so the C of every other program stays byte-identical to earlier
// compilers. int64 and int use the prelude's i64; uint uses u64.
//
// Signed values follow Go exactly. Arithmetic is computed in u64 (wrapping,
// with no C signed overflow) and the store to the typed temporary truncates
// to the width; Clang converts out-of-range integers to signed types modulo
// 2^width, which is Go's two's-complement result. Ordered comparisons,
// division and right shifts use the sign-extended i64 value.
const signedPrelude = "typedef signed char i8;\ntypedef short i16;\ntypedef int i32;\n"

// signedDivision is emitted only for programs with a signed / or %. SBF has
// no signed divide (LLVM would call __divdi3/__moddi3, which the runtime
// does not provide), so both divide the magnitudes as u64 and restore the
// sign: the quotient truncates toward zero and the remainder takes the
// dividend's sign, as in Go. |MinInt64| is 2^63 as a u64, so MinInt / -1 is
// 2^63, which converts back to MinInt, and MinInt % -1 is 0, with no trap.
// The caller has already trapped a zero divisor.
const signedDivision = `static inline u64 gosvm_magnitude64(i64 x) { return x < 0 ? 0 - (u64)x : (u64)x; }
static inline i64 gosvm_quo64(i64 x, i64 y) {
    u64 q = gosvm_magnitude64(x) / gosvm_magnitude64(y);
    return (i64)((x < 0) != (y < 0) ? 0 - q : q);
}
static inline i64 gosvm_rem64(i64 x, i64 y) {
    u64 r = gosvm_magnitude64(x) % gosvm_magnitude64(y);
    return (i64)(x < 0 ? 0 - r : r);
}
`

// usesSignedDivision reports whether any program file divides or takes the
// remainder of signed integers (outside constant expressions).
func usesSignedDivision(files []*ast.File, info *types.Info) bool {
	found := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if e, ok := n.(*ast.BinaryExpr); ok && !found && (e.Op.String() == "/" || e.Op.String() == "%") {
				found = info.Types[e].Value == nil && isSigned(info.Types[e.X].Type)
			}
			if s, ok := n.(*ast.AssignStmt); ok && !found && (s.Tok == token.QUO_ASSIGN || s.Tok == token.REM_ASSIGN) && len(s.Lhs) == 1 {
				found = isSigned(info.Types[s.Lhs[0]].Type) // x /= y and x %= y
			}
			return !found
		})
	}
	return found
}

func usesNarrowSigned(files []*ast.File, info *types.Info) bool {
	return usesBasic(files, info, func(b *types.Basic) bool {
		return b.Kind() == types.Int8 || b.Kind() == types.Int16 || b.Kind() == types.Int32
	})
}

// signedConstant spells a negative constant of a signed type. Non-negative
// constants keep the historical %dULL spelling, which C converts to the
// typed destination unchanged.
func signedConstant(v constant.Value) (string, bool) {
	n, ok := constant.Int64Val(v)
	if !ok || n >= 0 {
		return "", false
	}
	if n == math.MinInt64 {
		return "(-9223372036854775807LL-1)", true
	}
	return fmt.Sprintf("(%dLL)", n), true
}

// nonNegative reports whether an integer expression is known to be >= 0
// without evaluating it: an unsigned value, a non-negative constant, or a
// len, cap, copy or math/bits count result. Such int operands keep the unsigned
// comparison, shift-count and range lowering they have always had (so
// earlier programs' C is unchanged), which is exact for values that are
// never negative.
func (f *frontend) nonNegative(e ast.Expr) bool {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			break
		}
		e = p.X
	}
	tv := f.info.Types[e]
	if tv.Value != nil {
		return tv.Value.Kind() == constant.Int && constant.Sign(tv.Value) >= 0
	}
	if !isSigned(tv.Type) {
		return true
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if id, ok := call.Fun.(*ast.Ident); ok {
		if _, builtin := f.info.Uses[id].(*types.Builtin); builtin {
			return id.Name == "len" || id.Name == "cap" || id.Name == "copy"
		}
	}
	if fn, ok := isBitsFunc(f.info, call); ok {
		// The int results of math/bits are counts in 0..64.
		return types.Identical(fn.Type().(*types.Signature).Results().At(0).Type(), types.Typ[types.Int])
	}
	return false
}

// signedBinary lowers the operators whose C spelling differs for signed
// operands: ordered comparisons, division, remainder and right shift. It
// returns false for every other operator or operand type, which keep the
// unsigned (u64) lowering, exact for wrapping arithmetic and equality.
func (g *generator) signedBinary(e *ast.BinaryExpr, x, y string) (string, bool) {
	operand := g.info.Types[e.X].Type
	if !isSigned(operand) {
		return "", false
	}
	switch e.Op.String() {
	case "<", "<=", ">", ">=":
		if g.nonNegative(e.X) && g.nonNegative(e.Y) {
			return "", false
		}
		return fmt.Sprintf("((i64)%s %s (i64)%s)", x, e.Op, y), true
	case "/", "%":
		g.line("if (%s == 0) abort();", y)
		helper := "gosvm_quo64"
		if e.Op.String() == "%" {
			helper = "gosvm_rem64"
		}
		return fmt.Sprintf("%s((i64)%s,(i64)%s)", helper, x, y), true
	case ">>":
		// Arithmetic shift of the sign-extended value. A count of 63 or more
		// fills with the sign bit (0 or -1), as Go does for any count at or
		// beyond the width; the typed store truncates narrow results.
		return fmt.Sprintf("((i64)%s >> (%s >= 63 ? 63 : %s))", x, y, y), true
	}
	return "", false
}

// shiftCount traps a negative signed shift count, where Go panics. Counts
// that cannot be negative get no check.
func (g *generator) shiftCount(e *ast.BinaryExpr, y string) {
	if !g.nonNegative(e.Y) {
		g.line("if (%s < 0) abort();", y)
	}
}

// incDec lowers ++/-- on a signed target without C signed overflow: the
// update wraps in u64 and the store truncates, as for + and -.
func (g *generator) incDec(n ast.Node, t types.Type, target, tok string) {
	op := "+"
	if tok == "--" {
		op = "-"
	}
	g.line("%s = (%s)((u64)%s %s 1ULL);", target, g.ctype(n, t), target, op)
}
