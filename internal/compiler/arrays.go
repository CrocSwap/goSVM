package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
)

// Arrays are C structs so assignment, parameters and results preserve Go's
// by-value semantics. Raw C arrays decay to pointers and cannot do that.
type arrayType struct {
	typ  *types.Array
	name string
}

// This first array slice is bounded to scalar elements and 1 KiB per value.
// This limit does not prove that an entire function fits the SBF stack frame.
const maxArrayBytes = 1024

// LLVM can lower aggregate initialization/copies to these calls even with
// -fno-builtin. Provide them for array, pointer and view programs, keeping historical
// scalar/struct ELFs unchanged. Volatile byte accesses prevent the optimizer
// from transforming the helper loops back into calls to themselves.
const arrayMemory = `
__attribute__((visibility("hidden"))) void *memcpy(void *dst,const void *src,__SIZE_TYPE__ count) {
    volatile u8 *d=(volatile u8 *)dst;
    const volatile u8 *s=(const volatile u8 *)src;
    for (__SIZE_TYPE__ i=0;i<count;i++) d[i]=s[i];
    return dst;
}
__attribute__((visibility("hidden"))) void *memset(void *dst,int value,__SIZE_TYPE__ count) {
    volatile u8 *d=(volatile u8 *)dst;
    for (__SIZE_TYPE__ i=0;i<count;i++) d[i]=(u8)value;
    return dst;
}
`

// Large account bundles otherwise spend most of their CU in volatile byte
// copies. Only use word accesses when both actual addresses are aligned; the
// may_alias type permits accessing any object representation without violating
// C's effective-type rules. Unaligned buffers and tails retain byte accesses.
// Volatile accesses still prevent LLVM from lowering these loops to themselves.
const arrayMemoryWords = `
typedef u64 gosvm_memory_word __attribute__((may_alias));
__attribute__((visibility("hidden"))) void *memcpy(void *dst,const void *src,__SIZE_TYPE__ count) {
    volatile u8 *d=(volatile u8 *)dst;
    const volatile u8 *s=(const volatile u8 *)src;
    __SIZE_TYPE__ i=0;
    if ((((__UINTPTR_TYPE__)dst | (__UINTPTR_TYPE__)src)&7)==0) {
        volatile gosvm_memory_word *dw=(volatile gosvm_memory_word *)dst;
        const volatile gosvm_memory_word *sw=(const volatile gosvm_memory_word *)src;
        for (;count-i>=8;i+=8) dw[i/8]=sw[i/8];
    }
    for (;i<count;i++) d[i]=s[i];
    return dst;
}
__attribute__((visibility("hidden"))) void *memset(void *dst,int value,__SIZE_TYPE__ count) {
    volatile u8 *d=(volatile u8 *)dst;
    __SIZE_TYPE__ i=0;
    if (((__UINTPTR_TYPE__)dst&7)==0) {
        volatile gosvm_memory_word *dw=(volatile gosvm_memory_word *)dst;
        u64 word=(u64)(u8)value*0x0101010101010101ULL;
        for (;count-i>=8;i+=8) dw[i/8]=word;
    }
    for (;i<count;i++) d[i]=(u8)value;
    return dst;
}
`

func (g *generator) ensureMemory() {
	if !g.memory {
		if g.memoryWords {
			g.WriteString(arrayMemoryWords)
		} else {
			g.WriteString(arrayMemory)
		}
		g.memory = true
	}
}

func (g *generator) arrayName(node ast.Node, a *types.Array) string {
	for _, entry := range g.arrays {
		if types.Identical(entry.typ, a) {
			return entry.name
		}
	}
	g.fail(node, "array type was not declared before use")
	return ""
}

func (g *generator) emitArray(node ast.Node, a *types.Array) {
	for _, entry := range g.arrays {
		if types.Identical(entry.typ, a) {
			return
		}
	}
	b, ok := a.Elem().Underlying().(*types.Basic)
	if !ok || (b.Kind() != types.Uint8 && b.Kind() != types.Uint32 && b.Kind() != types.Uint64 && b.Kind() != types.Bool) {
		g.fail(node, "array elements must be byte, uint32, uint64, bool, or named versions of those scalars")
	}
	element := g.variable(node, a.Elem())
	size := (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(a.Elem())
	if a.Len() > maxArrayBytes/size {
		g.fail(node, "array storage exceeds experimental %d-byte per-value limit", maxArrayBytes)
	}
	g.ensureMemory()
	g.next++
	name := fmt.Sprintf("array%d", g.next)
	g.arrays = append(g.arrays, arrayType{a, name})
	storage := a.Len()
	if storage == 0 {
		storage = 1 // C11 requires positive storage; no element is accessible.
	}
	g.line("typedef struct { %s items[%d]; } %s;", element, storage, name)
	g.line("static inline %s *%s_at(%s *a,u64 i) { if (i >= %dULL) abort(); return &a->items[i]; }", element, name, name, a.Len())
	g.line("static inline boolean %s_equal(%s a,%s b) {", name, name, name)
	g.line("for (u64 i=0;i<%dULL;i++) { if (a.items[i] != b.items[i]) return 0; }", a.Len())
	g.line("return 1;\n}")
}

func (g *generator) arrayLiteral(e *ast.CompositeLit, a *types.Array) string {
	r := g.temp(e, g.info.Types[e].Type, "{0}")
	index := int64(0)
	for _, elt := range e.Elts {
		value := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			var valid bool
			index, valid = constant.Int64Val(g.info.Types[kv.Key].Value)
			if !valid || index < 0 || index >= a.Len() {
				g.fail(kv, "invalid array literal index")
			}
			value = kv.Value
		}
		v := g.expr(value)
		g.line("%s.items[%d] = %s;", r, index, v)
		index++
	}
	return r
}
