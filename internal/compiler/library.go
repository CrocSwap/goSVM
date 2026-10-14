package compiler

import (
	"go/ast"
	"gosvm/internal/library"
)

func (g *generator) emitLibraryBody(fn *ast.FuncDecl, b library.Binding) {
	// The generated native wrapper has this exact signature. The synthetic source
	// participates in ordinary type/borrow checking; only its body is replaced.
	in := g.name(g.info.Defs[fn.Type.Params.List[0].Names[0]])
	out := g.name(g.info.Defs[fn.Type.Params.List[0].Names[1]])
	g.line("{")
	g.line("extern u32 %s(const u8 *,u64,u8 *,u64,u64 *);", b.Symbol)
	g.line("u64 length=0;")
	g.line("u32 status=%s(%s.ptr,(u64)%s.len,%s.ptr,(u64)%s.len,&length);", b.Symbol, in, in, out, out)
	g.line("return (%s){.r0=status,.r1=length};", g.resultName(fn, g.currentResults))
	g.line("}")
}

// The frontend already owns memcpy/memset. Pure foreign libraries can additionally
// need overlap-safe memmove; provide it locally instead of linking a second
// three-helper object and importing unqualified memory syscalls.
const libraryMemory = `
__attribute__((visibility("hidden"))) void *memmove(void *dst,const void *src,__SIZE_TYPE__ n) {
 u8 *d=(u8*)dst;const u8 *s=(const u8*)src;
 if ((u64)d<(u64)s) {for(__SIZE_TYPE__ i=0;i<n;i++) d[i]=s[i];}
 else {while(n>0) {n--;d[n]=s[n];}}
 return dst;
}
`
