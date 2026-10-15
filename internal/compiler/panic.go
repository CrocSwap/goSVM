package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
)

// panic(v). A panic aborts the transaction: it logs one line describing v
// with the VM's sol_log_ syscall and then calls the abort syscall, like
// every other trap. Deferred calls do not run and there is no recover; the
// instruction's effects roll back, so the transaction fails exactly as an
// unrecovered Go panic would make it fail. See docs/ERROR_CONTRACT.md#panic.

// PanicLineBytes bounds a panic's log line; a longer line is truncated to
// its first PanicLineBytes bytes.
const PanicLineBytes = 512

// PanicValue says how a panic's log line shows its argument.
type PanicValue int

const (
	// PanicText: the line is Text; the argument (if any) is evaluated for
	// its effects only. Constants, nil, and values Go would print by
	// address or through a method use it.
	PanicText PanicValue = iota + 1
	// PanicString: Prefix, the string's bytes, then Suffix.
	PanicString
	// PanicSigned and PanicUnsigned: Prefix, the integer in decimal (with a
	// leading '-' when negative), then Suffix.
	PanicSigned
	PanicUnsigned
	// PanicBool: Prefix, "true" or "false", then Suffix.
	PanicBool
	// PanicInterface: an interface value: the line is NilPanicLine when it
	// is nil (Go then panics with *runtime.PanicNilError), else Text.
	PanicInterface
)

// NilPanicLine is the line of panic(nil), and of a nil interface value.
const NilPanicLine = "panic: panic called with nil argument"

// PanicSite is one call of the panic builtin. The log line is
// Prefix+value+Suffix (or Text), truncated to PanicLineBytes bytes.
type PanicSite struct {
	Call   *ast.CallExpr
	Value  PanicValue
	Text   string
	Prefix string
	Suffix string
}

// panicRuntime is emitted for programs that call panic: the log syscall
// (static hash on v3, the legacy import on v0) and the line builders.
const panicRuntime = `
#ifdef GOSVM_SBF_V3
#define gosvm_log ((void (*)(const u8 *,u64))0x207559bdULL)
#else
extern void sol_log_(const u8 *,u64);
#define gosvm_log sol_log_
#endif
/* panic(v): log one line (at most 512 bytes), then abort. sol_log_ requires
   UTF-8 (it fails otherwise), so each byte that does not start a valid UTF-8
   sequence, including a sequence cut by the 512-byte limit, becomes '?'. */
static __attribute__((noreturn,noinline)) void gosvm_panic(const u8 *a,u64 an,const u8 *b,u64 bn,const u8 *c,u64 cn) {
    u8 line[512];
    u64 n=0;
    for (u64 i=0;i<an && n<512;i++) line[n++]=a[i];
    for (u64 i=0;i<bn && n<512;i++) line[n++]=b[i];
    for (u64 i=0;i<cn && n<512;i++) line[n++]=c[i];
    for (u64 i=0;i<n;) {
        u8 x=line[i];
        u64 w=x<0x80 ? 1 : x>=0xC2 && x<=0xDF ? 2 : x>=0xE0 && x<=0xEF ? 3 : x>=0xF0 && x<=0xF4 ? 4 : 0;
        boolean ok=w!=0 && w<=n-i;
        if (ok && w>1) {
            u8 lo=x==0xE0 ? 0xA0 : x==0xF0 ? 0x90 : 0x80, hi=x==0xED ? 0x9F : x==0xF4 ? 0x8F : 0xBF;
            if (line[i+1]<lo || line[i+1]>hi) ok=0;
            for (u64 k=2;k<w;k++) if (line[i+k]<0x80 || line[i+k]>0xBF) ok=0;
        }
        if (!ok) { line[i]='?'; i++; } else i+=w;
    }
    gosvm_log(line,n);
    abort();
}
static __attribute__((noreturn,noinline)) void gosvm_panic_int(const u8 *a,u64 an,u64 v,boolean negative,const u8 *c,u64 cn) {
    u8 digits[21];
    u64 n=21;
    do { digits[--n]=(u8)('0'+v%10); v/=10; } while (v);
    if (negative) digits[--n]='-';
    gosvm_panic(a,an,digits+n,21-n,c,cn);
}
`

const recoverRule = "recover unsupported: a panic aborts the transaction and rolls back its effects, so there is nothing to recover"

// panicCall checks panic(v) and records how its log line shows v; recover
// is rejected.
func (c *checker) panicCall(e *ast.CallExpr, name string) {
	if name == "recover" {
		c.fail(e, recoverRule)
	}
	arg := e.Args[0]
	tv := c.info.Types[arg]
	site := &PanicSite{Call: e, Value: PanicText}
	switch {
	case tv.IsNil():
		// Go 1.21 and later panic with *runtime.PanicNilError.
		site.Text = NilPanicLine
	case tv.Value != nil:
		if tv.Value.Kind() != constant.String {
			c.typeRule(arg, tv.Type)
			c.constantRule(arg, tv.Type, tv.Value)
		}
		site.Text = constantPanicLine(tv.Type, tv.Value)
	default:
		c.expr(arg)
		c.classifyPanic(site, tv.Type)
	}
	if len(site.Text) > PanicLineBytes {
		site.Text = site.Text[:PanicLineBytes]
	}
	if c.panics == nil {
		c.panics = map[*ast.CallExpr]*PanicSite{}
	}
	c.panics[e] = site
}

// panicTypeName spells a named type as Go's runtime does in panic messages:
// package name, then type name (with type arguments).
func panicTypeName(t types.Type) string { return relativeTypeString(t, nil) }

// printsValue reports whether Go's runtime prints a panic value of type t by
// its value: a string, integer or bool type without Error or String methods
// (Go prints their results instead).
func printsValue(t types.Type) bool {
	if isIface(t) {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok || b.Info()&(types.IsString|types.IsInteger|types.IsBoolean) == 0 {
		return false
	}
	set := types.NewMethodSet(t)
	for i := 0; i < set.Len(); i++ {
		if n := set.At(i).Obj().Name(); n == "Error" || n == "String" {
			return false
		}
	}
	return true
}

// decoration is the prefix and suffix Go prints around a value of named
// type t: T( and ), or T(" and ") for strings.
func decoration(t types.Type) (string, string) {
	if _, named := t.(*types.Named); !named {
		return "panic: ", ""
	}
	if isString(t) {
		return "panic: " + panicTypeName(t) + `("`, `")`
	}
	return "panic: " + panicTypeName(t) + "(", ")"
}

func (c *checker) classifyPanic(site *PanicSite, t types.Type) {
	if isIface(t) {
		site.Value, site.Text = PanicInterface, "panic: ("+panicTypeName(t)+")"
		return
	}
	if !printsValue(t) {
		site.Text = "panic: (" + panicTypeName(t) + ")"
		return
	}
	site.Prefix, site.Suffix = decoration(t)
	b := t.Underlying().(*types.Basic)
	switch {
	case b.Info()&types.IsString != 0:
		site.Value = PanicString
	case b.Info()&types.IsBoolean != 0:
		site.Value = PanicBool
	case b.Info()&types.IsUnsigned != 0:
		site.Value = PanicUnsigned
	default:
		site.Value = PanicSigned
	}
}

// constantPanicLine is the complete line for a constant argument, whose
// type is the constant's default type when it is untyped.
func constantPanicLine(t types.Type, v constant.Value) string {
	if !printsValue(t) {
		return "panic: (" + panicTypeName(t) + ")"
	}
	prefix, suffix := decoration(t)
	switch v.Kind() {
	case constant.String:
		return prefix + constant.StringVal(v) + suffix
	case constant.Bool:
		return prefix + fmt.Sprint(constant.BoolVal(v)) + suffix
	}
	return prefix + v.ExactString() + suffix
}

// cBytes is a C string literal holding s's bytes.
func cBytes(s string) string {
	var b strings.Builder
	b.WriteString(`(const u8 *)"`)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' && c != '?' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "\\%03o", c)
		}
	}
	b.WriteString(`"`)
	return b.String()
}

// panicCall lowers panic(v): the argument is evaluated, the line logged and
// the abort syscall called.
func (g *generator) panicCall(e *ast.CallExpr) {
	site := g.panics[e]
	if site == nil {
		g.unchecked(e)
	}
	arg := e.Args[0]
	if site.Value == PanicText {
		if tv := g.info.Types[arg]; !tv.IsNil() && tv.Value == nil {
			g.expr(arg) // evaluated for its effects only
		}
		g.line("gosvm_panic(%s,%dULL,0,0,0,0);", cBytes(site.Text), len(site.Text))
		return
	}
	v := g.expr(arg)
	prefix, suffix := cBytes(site.Prefix), cBytes(site.Suffix)
	switch site.Value {
	case PanicInterface:
		g.line("if (%s.type == 0) gosvm_panic(%s,%dULL,0,0,0,0);", v, cBytes(NilPanicLine), len(NilPanicLine))
		g.line("gosvm_panic(%s,%dULL,0,0,0,0);", cBytes(site.Text), len(site.Text))
	case PanicString:
		g.line("gosvm_panic(%s,%dULL,%s.ptr,(u64)%s.len,%s,%dULL);", prefix, len(site.Prefix), v, v, suffix, len(site.Suffix))
	case PanicBool:
		g.line("gosvm_panic(%s,%dULL,%s ? (const u8 *)\"true\" : (const u8 *)\"false\",%s ? 4ULL : 5ULL,%s,%dULL);", prefix, len(site.Prefix), v, v, suffix, len(site.Suffix))
	case PanicUnsigned:
		g.line("gosvm_panic_int(%s,%dULL,(u64)%s,0,%s,%dULL);", prefix, len(site.Prefix), v, suffix, len(site.Suffix))
	case PanicSigned:
		g.line("gosvm_panic_int(%s,%dULL,(i64)%s < 0 ? -(u64)(i64)%s : (u64)(i64)%s,(i64)%s < 0,%s,%dULL);", prefix, len(site.Prefix), v, v, v, v, suffix, len(site.Suffix))
	default:
		g.unchecked(e)
	}
}
