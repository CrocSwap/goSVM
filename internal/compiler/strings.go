package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// Strings. A string value is two words, a pointer to immutable bytes and a
// length (C: gosvm_string). The bytes are read-only data (literals and
// constant expressions), the invocation heap (concatenations and
// conversions) or a slice of either; they are never on the stack and never
// SDK storage, so strings need no borrow tracking. See docs/GO_STRINGS.md.

// isString reports whether t is a string type (string, a named string type,
// or an untyped string constant).
func isString(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// runeType is the type of a string range's value variable.
var runeType = types.Universe.Lookup("rune").Type()

// stringConversion classifies a conversion T(x) that involves strings.
type stringConversion int

const (
	notStringConversion stringConversion = iota
	stringFromString                     // string(s), Name(s): copies the two words
	stringFromBytes                      // string(b) for a []byte-like b: allocates
	stringFromRunes                      // string(r) for a []rune-like r: allocates
	stringFromInteger                    // string(i): the UTF-8 encoding of rune i; allocates
	bytesFromString                      // []byte(s): allocates
	runesFromString                      // []rune(s): allocates
)

// elemKind is the underlying basic kind of a slice's element, or -1.
func elemKind(t types.Type) types.BasicKind {
	s, ok := t.Underlying().(*types.Slice)
	if !ok {
		return -1
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	if !ok {
		return -1
	}
	return b.Kind()
}

// classifyConversion classifies the conversion call e (whose Fun is a type).
func classifyConversion(info *types.Info, e *ast.CallExpr) stringConversion {
	if len(e.Args) != 1 || !info.Types[e.Fun].IsType() {
		return notStringConversion
	}
	to, from := info.Types[e].Type, info.Types[e.Args[0]].Type
	switch {
	case isString(to) && isString(from):
		return stringFromString
	case isString(to) && elemKind(from) == types.Uint8:
		return stringFromBytes
	case isString(to) && elemKind(from) == types.Int32:
		return stringFromRunes
	case isString(to):
		if b, ok := from.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
			return stringFromInteger
		}
	case isString(from) && elemKind(to) == types.Uint8:
		return bytesFromString
	case isString(from) && elemKind(to) == types.Int32:
		return runesFromString
	}
	return notStringConversion
}

// stringAllocates reports whether any non-constant expression in files
// allocates string or byte storage on the heap: a concatenation, += on a
// string, or a conversion between strings and slices or from an integer.
// Such programs carry the heap runtime.
func stringAllocates(info *types.Info, files []*ast.File) bool {
	found := false
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if n.Op == token.ADD && info.Types[n].Value == nil && isString(info.Types[n].Type) {
					found = true
				}
			case *ast.AssignStmt:
				if n.Tok == token.ADD_ASSIGN && len(n.Lhs) == 1 && isString(info.Types[n.Lhs[0]].Type) {
					found = true
				}
			case *ast.CallExpr:
				if info.Types[n].Value == nil {
					switch classifyConversion(info, n) {
					case notStringConversion, stringFromString:
					default:
						found = true
					}
				}
			}
			return !found
		})
	}
	return found
}

// Checker rules.

// stringConversionRule checks a conversion involving strings.
func (c *checker) stringConversionRule(e *ast.CallExpr, tv types.TypeAndValue) {
	c.expr(e.Args[0])
	c.typeRule(e, tv.Type)
}

// stringSlice checks s[lo:hi] on a string.
func (c *checker) stringSlice(e *ast.SliceExpr) {
	if e.Slice3 {
		c.fail(e, "3-index slice of string")
	}
	c.expr(e.X)
	for _, bound := range []ast.Expr{e.Low, e.High} {
		if bound != nil {
			c.expr(bound)
		}
	}
	c.typeRule(e, c.info.Types[e].Type)
}

// stringAddAssign checks s += t: the target's operands, then the operand.
func (c *checker) stringAddAssign(s *ast.AssignStmt) {
	t := c.info.Types[s.Lhs[0]].Type
	if _, ok := s.Lhs[0].(*ast.Ident); !ok {
		c.captureTarget(s.Lhs[0])
	}
	c.expr(s.Rhs[0])
	c.typeRule(s, t)
}

// Emitter.

// stringRuntime is emitted for programs with string values: the two-word
// representation, comparison, indexing, slicing and UTF-8 decoding.
const stringRuntime = `
/* Go string: immutable bytes (read-only data or the invocation heap) and a
   length. */
typedef struct { u8 *ptr; i64 len; } gosvm_string;
static boolean gosvm_str_eq(gosvm_string a, gosvm_string b) {
    if (a.len != b.len) return 0;
    for (u64 i = 0; i < (u64)a.len; i++) if (a.ptr[i] != b.ptr[i]) return 0;
    return 1;
}
/* Bytewise comparison: -1, 0 or 1. */
static i64 gosvm_str_cmp(gosvm_string a, gosvm_string b) {
    u64 n = (u64)(a.len < b.len ? a.len : b.len);
    for (u64 i = 0; i < n; i++) if (a.ptr[i] != b.ptr[i]) return a.ptr[i] < b.ptr[i] ? -1 : 1;
    return a.len < b.len ? -1 : a.len > b.len ? 1 : 0;
}
static inline u8 gosvm_str_at(gosvm_string s, u64 i) { if (i >= (u64)s.len) abort(); return s.ptr[i]; }
static inline gosvm_string gosvm_str_slice(gosvm_string s, u64 lo, u64 hi) {
    if (lo > hi || hi > (u64)s.len) abort();
    return (gosvm_string){s.ptr ? s.ptr + lo : 0, (i64)(hi - lo)};
}
/* Go's runtime.decoderune: the rune starting at s[k] (k < len) and the index
   after it; an invalid, overlong, surrogate or truncated sequence is U+FFFD
   with width 1. */
static u64 gosvm_decoderune(gosvm_string s, u64 k, i64 *r) {
    const u8 *p = s.ptr + k;
    u64 n = (u64)s.len - k;
    u8 c = p[0];
    if (c < 0x80) { *r = c; return k + 1; }
    if (c >= 0xC0 && c < 0xE0) {
        if (n > 1 && (p[1] & 0xC0) == 0x80) {
            i64 v = (i64)(c & 0x1F) << 6 | (i64)(p[1] & 0x3F);
            if (v > 0x7F) { *r = v; return k + 2; }
        }
    } else if (c >= 0xE0 && c < 0xF0) {
        if (n > 2 && (p[1] & 0xC0) == 0x80 && (p[2] & 0xC0) == 0x80) {
            i64 v = (i64)(c & 0x0F) << 12 | (i64)(p[1] & 0x3F) << 6 | (i64)(p[2] & 0x3F);
            if (v > 0x7FF && !(v >= 0xD800 && v <= 0xDFFF)) { *r = v; return k + 3; }
        }
    } else if (c >= 0xF0 && c < 0xF8) {
        if (n > 3 && (p[1] & 0xC0) == 0x80 && (p[2] & 0xC0) == 0x80 && (p[3] & 0xC0) == 0x80) {
            i64 v = (i64)(c & 0x07) << 18 | (i64)(p[1] & 0x3F) << 12 | (i64)(p[2] & 0x3F) << 6 | (i64)(p[3] & 0x3F);
            if (v > 0xFFFF && v <= 0x10FFFF) { *r = v; return k + 4; }
        }
    }
    *r = 0xFFFD;
    return k + 1;
}
`

// stringHeapRuntime is emitted for string programs that carry the heap
// runtime: concatenation and the allocating conversions. Allocation follows
// docs/GO_STRINGS.md (heap accounting is contracted behaviour).
const stringHeapRuntime = `
/* Go's encoderune for r as uint32(rune): invalid runes encode U+FFFD. */
static u64 gosvm_runelen(u64 r) {
    if (r <= 0x7F) return 1;
    if (r <= 0x7FF) return 2;
    if (r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF) || r <= 0xFFFF) return 3;
    return 4;
}
static u64 gosvm_encoderune(u8 *p, u64 r) {
    if (r <= 0x7F) { p[0] = (u8)r; return 1; }
    if (r <= 0x7FF) { p[0] = (u8)(0xC0 | r >> 6); p[1] = (u8)(0x80 | (r & 0x3F)); return 2; }
    if (r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF)) r = 0xFFFD;
    if (r <= 0xFFFF) { p[0] = (u8)(0xE0 | r >> 12); p[1] = (u8)(0x80 | (r >> 6 & 0x3F)); p[2] = (u8)(0x80 | (r & 0x3F)); return 3; }
    p[0] = (u8)(0xF0 | r >> 18); p[1] = (u8)(0x80 | (r >> 12 & 0x3F)); p[2] = (u8)(0x80 | (r >> 6 & 0x3F)); p[3] = (u8)(0x80 | (r & 0x3F));
    return 4;
}
/* Go's concatstrings: no operand bytes is "", exactly one non-empty operand
   is that operand (no allocation), otherwise one allocation of the total. */
static gosvm_string gosvm_concat(const gosvm_string *a, u64 n) {
    u64 total = 0, count = 0, idx = 0;
    for (u64 i = 0; i < n; i++) {
        if (a[i].len == 0) continue;
        total += (u64)a[i].len;
        count++;
        idx = i;
    }
    if (count == 0) return (gosvm_string){0, 0};
    if (count == 1) return a[idx];
    u8 *p = gosvm_alloc(total);
    u64 o = 0;
    for (u64 i = 0; i < n; i++) { gosvm_move(p + o, a[i].ptr, (u64)a[i].len); o += (u64)a[i].len; }
    return (gosvm_string){p, (i64)total};
}
/* string(b): a copy of len(b) bytes; nothing for an empty slice. */
static gosvm_string gosvm_str_bytes(slice b) {
    if (b.len == 0) return (gosvm_string){0, 0};
    u8 *p = gosvm_alloc((u64)b.len);
    gosvm_move(p, b.ptr, (u64)b.len);
    return (gosvm_string){p, b.len};
}
/* string(r) for an integer r: its UTF-8 encoding (U+FFFD outside the valid
   runes) in an 8-byte allocation. */
static gosvm_string gosvm_str_rune(i64 v) {
    u64 r = v < 0 || v > 0x10FFFF ? 0xFFFD : (u64)v;
    u8 *p = gosvm_alloc(8);
    return (gosvm_string){p, (i64)gosvm_encoderune(p, r)};
}
/* string(a) for a []rune: the runes' UTF-8 encodings; nothing when empty. */
static gosvm_string gosvm_str_runes(slice a) {
    u64 n = 0;
    for (u64 j = 0; j < (u64)a.len; j++) n += gosvm_runelen(((const u32 *)a.ptr)[j]);
    if (n == 0) return (gosvm_string){0, 0};
    u8 *p = gosvm_alloc(n);
    u64 o = 0;
    for (u64 j = 0; j < (u64)a.len; j++) o += gosvm_encoderune(p + o, ((const u32 *)a.ptr)[j]);
    return (gosvm_string){p, (i64)n};
}
/* []byte(s): Go 1.22's rawbyteslice, capacity roundupsize(len(s)); a
   non-nil empty slice without allocating for "". */
static slice gosvm_bytes_str(gosvm_string s) {
    if (s.len == 0) return (slice){GOSVM_HEAP_BASE, 0, 0};
    if ((u64)s.len > GOSVM_HEAP_BYTES) abort();
    u64 cap = gosvm_roundupsize((u64)s.len, 0);
    u8 *p = gosvm_alloc(cap);
    gosvm_move(p, s.ptr, (u64)s.len);
    return (slice){p, s.len, (i64)cap};
}
/* []byte(c) for a constant c: Go copies it into a [len(c)]byte, so the
   capacity is the length. */
static slice gosvm_bytes_const(gosvm_string s) {
    if (s.len == 0) return (slice){GOSVM_HEAP_BASE, 0, 0};
    u8 *p = gosvm_alloc((u64)s.len);
    gosvm_move(p, s.ptr, (u64)s.len);
    return (slice){p, s.len, s.len};
}
/* []rune(s): Go 1.22's rawruneslice, capacity roundupsize(4n)/4 for n runes. */
static slice gosvm_runes_str(gosvm_string s) {
    u64 n = 0;
    i64 r;
    for (u64 k = 0; k < (u64)s.len; n++) k = gosvm_decoderune(s, k, &r);
    if (n == 0) return (slice){GOSVM_HEAP_BASE, 0, 0};
    if (n > GOSVM_HEAP_BYTES / 4) abort();
    u64 mem = gosvm_roundupsize(n * 4, 0);
    u32 *p = (u32 *)gosvm_alloc(mem);
    u64 j = 0;
    for (u64 k = 0; k < (u64)s.len; j++) { k = gosvm_decoderune(s, k, &r); p[j] = (u32)r; }
    return (slice){(u8 *)p, (i64)n, (i64)(mem / 4)};
}
`

// stringLiteral spells a constant string as read-only data. Every byte that
// is not a plain printable character is an octal escape (no hex digits can
// run on, and no trigraph can form).
func stringLiteral(v constant.Value) string {
	s := constant.StringVal(v)
	if s == "" {
		return "((gosvm_string){0,0})"
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' && c != '?' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "\\%03o", c)
		}
	}
	return fmt.Sprintf("((gosvm_string){(u8 *)\"%s\",%d})", b.String(), len(s))
}

// concatOperands appends the leaves of a maximal tree of non-constant string
// additions, left to right (as Go's compiler flattens x + y + (z + w) into
// one concatenation).
func (g *generator) concatOperands(e ast.Expr, out []ast.Expr) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD && g.info.Types[b].Value == nil && isString(g.info.Types[b].Type) {
		return g.concatOperands(b.Y, g.concatOperands(b.X, out))
	}
	return append(out, e)
}

// concat concatenates evaluated operand values into a value of type t.
func (g *generator) concat(n ast.Node, t types.Type, values []string) string {
	return g.temp(n, t, fmt.Sprintf("gosvm_concat((gosvm_string[]){%s},%dULL)", strings.Join(values, ","), len(values)))
}

// stringBinary lowers +, comparisons and equality of strings.
func (g *generator) stringBinary(e *ast.BinaryExpr, tv types.TypeAndValue) string {
	if e.Op == token.ADD {
		var values []string
		for _, operand := range g.concatOperands(e, nil) {
			values = append(values, g.expr(operand))
		}
		return g.concat(e, tv.Type, values)
	}
	x, y := g.expr(e.X), g.expr(e.Y)
	switch e.Op {
	case token.EQL:
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_str_eq(%s,%s)", x, y))
	case token.NEQ:
		return g.temp(e, tv.Type, fmt.Sprintf("!gosvm_str_eq(%s,%s)", x, y))
	case token.LSS, token.LEQ, token.GTR, token.GEQ:
		return g.temp(e, tv.Type, fmt.Sprintf("(gosvm_str_cmp(%s,%s) %s 0)", x, y, e.Op))
	}
	g.unchecked(e)
	return ""
}

// stringAddAssign lowers s += t: the target's operands are evaluated, then
// the operand, then the target is read and the concatenation stored.
func (g *generator) stringAddAssign(s *ast.AssignStmt) {
	lhs := s.Lhs[0]
	t := g.info.Types[lhs].Type
	target := g.captureTarget(lhs)
	var values []string
	for _, operand := range g.concatOperands(s.Rhs[0], nil) {
		values = append(values, g.expr(operand))
	}
	current := g.temp(lhs, t, target)
	result := g.concat(s, t, append([]string{current}, values...))
	g.line("%s = %s;", target, result)
}

// stringSlice lowers s[lo:hi]: the result shares s's bytes.
func (g *generator) stringSlice(e *ast.SliceExpr) string {
	x := g.expr(e.X)
	lo := "0ULL"
	if e.Low != nil {
		lo = g.expr(e.Low)
	}
	hi := "(" + x + ").len"
	if e.High != nil {
		hi = g.expr(e.High)
	}
	return g.temp(e, g.info.Types[e].Type, fmt.Sprintf("gosvm_str_slice(%s,(u64)%s,(u64)%s)", x, lo, hi))
}

// stringConversion lowers a conversion Check classified as one involving
// strings.
func (g *generator) stringConversion(e *ast.CallExpr, kind stringConversion) string {
	tv := g.info.Types[e]
	x := g.expr(e.Args[0])
	switch kind {
	case stringFromString:
		return g.temp(e, tv.Type, x)
	case stringFromBytes:
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_str_bytes(%s)", x))
	case stringFromRunes:
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_str_runes(%s)", x))
	case stringFromInteger:
		// Sign- or zero-extend by the operand's type, as Go converts to int64.
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_str_rune((i64)%s)", x))
	case bytesFromString:
		if g.info.Types[e.Args[0]].Value != nil {
			// Go copies a constant into an array of exactly its length.
			return g.temp(e, tv.Type, fmt.Sprintf("gosvm_bytes_const(%s)", x))
		}
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_bytes_str(%s)", x))
	case runesFromString:
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_runes_str(%s)", x))
	}
	g.unchecked(e)
	return ""
}

// asSlice views a C string value as a byte slice (for append(b, s...) and
// copy(b, s)); the view is only read.
func asSlice(value string) string {
	return fmt.Sprintf("GOSVM_SLICE((%s).ptr,(%s).len)", value, value)
}
