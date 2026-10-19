package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
)

// Invocation heap budget. The SBF runtime maps a zero-filled heap region of
// 32 KiB at MM_HEAP_START for every invocation; a transaction can request up
// to 256 KiB, in 1 KiB steps, with the ComputeBudget RequestHeapFrame
// instruction. See docs/GO_ALLOCATION.md.
const (
	DefaultHeapBytes = 32 * 1024
	MinHeapBytes     = 32 * 1024
	MaxHeapBytes     = 256 * 1024
)

// ValidateHeapBytes accepts 0 (the default) or a multiple of 1 KiB from
// 32 KiB to 256 KiB, the frame sizes RequestHeapFrame can request.
func ValidateHeapBytes(n int) error {
	if n != 0 && (n%1024 != 0 || n < MinHeapBytes || n > MaxHeapBytes) {
		return fmt.Errorf("heap_bytes %d must be a multiple of 1024 from %d to %d", n, MinHeapBytes, MaxHeapBytes)
	}
	return nil
}

// allocBuiltins are the builtins added with heap allocation; len and cap
// remain the only other supported builtins.
var allocBuiltins = map[string]bool{"new": true, "make": true, "append": true, "copy": true}

const builtinRule = "only len, cap, new, make, append, copy, delete, clear and panic builtins supported"

// sliceRule admits a slice of any storable element: unsigned scalars, bool,
// value structs, declared scalar arrays, pointers and slices. Elements must
// have a nonzero size without zero-length arrays or empty structs, so the Go
// size used for append growth equals the element's C size.
func (f *frontend) sliceRule(n ast.Node, s *types.Slice) {
	if isContext(s.Elem()) {
		f.fail(n, "slices of Context unsupported")
	}
	if _, ok := s.Elem().(*types.Tuple); ok {
		f.fail(n, "unsupported type %s", s)
	}
	f.variableRule(n, s.Elem())
	if zeroSizePart(s.Elem()) {
		f.fail(n, "slice elements must not contain zero-length arrays or empty structs")
	}
}

// zeroSizePart reports whether t stores a zero-length array or an empty
// struct by value, where Go and C sizes may differ.
func zeroSizePart(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Array:
		return u.Len() == 0 || zeroSizePart(u.Elem())
	case *types.Struct:
		if u.NumFields() == 0 {
			return true
		}
		for i := 0; i < u.NumFields(); i++ {
			if zeroSizePart(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// containsReferences reports whether values of t hold pointers (Go's
// et.PtrBytes != 0), which selects the malloc-header rounding in growslice.
func containsReferences(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Interface, *types.Map: // a map is a pointer to its header
		return true
	case *types.Basic:
		return u.Info()&types.IsString != 0 // a string holds a data pointer
	case *types.Array:
		return containsReferences(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if containsReferences(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

func isByteSlice(t types.Type) bool {
	s, ok := t.(*types.Slice)
	return ok && types.Identical(s.Elem(), types.Typ[types.Uint8])
}

// literalLength is the length of a slice literal: one past its largest index.
func literalLength(info *types.Info, e *ast.CompositeLit) int64 {
	index, length := int64(0), int64(0)
	for _, elt := range e.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			index, _ = constant.Int64Val(info.Types[kv.Key].Value)
		}
		index++
		if index > length {
			length = index
		}
	}
	return length
}

// allocBuiltin checks new, make, append and copy. Their operands are
// evaluated in source order, like call arguments.
func (c *checker) allocBuiltin(e *ast.CallExpr, name string, tv types.TypeAndValue) {
	switch name {
	case "new":
		c.typeRule(e, tv.Type)
	case "make":
		if _, ok := tv.Type.(*types.Slice); !ok && !isMap(tv.Type) {
			c.fail(e, "make supports only slices and maps")
		}
		c.typeRule(e, tv.Type)
		for _, size := range e.Args[1:] {
			c.expr(size)
		}
	case "append":
		c.typeRule(e, tv.Type)
		s := tv.Type.(*types.Slice)
		c.exprAs(e.Args[0], s)
		if e.Ellipsis.IsValid() {
			c.exprAs(e.Args[1], s)
			return
		}
		for _, value := range e.Args[1:] {
			c.exprAs(value, s.Elem())
		}
	case "copy":
		for _, operand := range e.Args {
			c.exprAs(operand, c.info.Types[e.Args[0]].Type)
		}
		c.typeRule(e, tv.Type)
	}
}

// sliceLiteral checks []T{...}: elements in source order with constant keys.
func (c *checker) sliceLiteral(e *ast.CompositeLit, s *types.Slice) {
	c.typeRule(e, s)
	for _, elt := range e.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if index, valid := constant.Int64Val(c.info.Types[kv.Key].Value); !valid || index < 0 {
				c.fail(kv, "invalid slice literal index")
			}
			elt = kv.Value
		}
		c.exprAs(elt, s.Elem())
	}
}

// heapRuntime is the invocation allocator and the slice helpers for element
// types other than byte. On the modern SBF profiles gosvm_move hands moves
// from a threshold to sol_memmove_ (memops.go). It is emitted only for programs that use them; see
// docs/GO_ALLOCATION.md for the contract it implements.
const heapRuntime = `
#ifndef GOSVM_HEAP_BASE
#define GOSVM_HEAP_BASE ((u8 *)0x300000000ULL)
#endif
#define GOSVM_HEAP_BYTES @HEAP@ULL
/* Bump allocator over the invocation heap, which the runtime zero-fills for
   every invocation. The first word counts the bytes in use (0: none yet).
   Allocations are 8-byte aligned, zero, never freed; exhaustion aborts. A
   budget above the default 32 KiB frame touches its last byte on the first
   allocation, so a transaction without a large enough RequestHeapFrame fails
   there deterministically (an access violation). */
static u8 *gosvm_alloc(u64 bytes) {
    u64 *used = (u64 *)GOSVM_HEAP_BASE;
    if (GOSVM_HEAP_BYTES > 32768 && *used == 0) (void)*(volatile u8 *)(GOSVM_HEAP_BASE + GOSVM_HEAP_BYTES - 1);
    u64 offset = *used ? *used : 8;
    if (bytes > GOSVM_HEAP_BYTES - offset) abort();
    *used = offset + ((bytes + 7) & ~7ULL);
    return GOSVM_HEAP_BASE + offset;
}
/* memmove: forward or backward, by words when everything is aligned. */
static void gosvm_move(u8 *dst, const u8 *src, u64 n) {
    __UINTPTR_TYPE__ d = (__UINTPTR_TYPE__)dst, s = (__UINTPTR_TYPE__)src;
    if (n == 0 || d == s) return;
#ifdef GOSVM_MEMORY_SYSCALL_MOVE
    if (n >= GOSVM_MEMORY_SYSCALL_MIN) { GOSVM_MEMORY_SYSCALL_MOVE(dst, src, n); return; }
#endif
    if (((d | s | n) & 7) == 0) {
        volatile u64 *dw = (volatile u64 *)dst;
        const volatile u64 *sw = (const volatile u64 *)src;
        if (d < s) { for (u64 i = 0; i < n / 8; i++) dw[i] = sw[i]; }
        else { for (u64 i = n / 8; i > 0; i--) dw[i - 1] = sw[i - 1]; }
        return;
    }
    volatile u8 *db = dst;
    const volatile u8 *sb = src;
    if (d < s) { for (u64 i = 0; i < n; i++) db[i] = sb[i]; }
    else { for (u64 i = n; i > 0; i--) db[i - 1] = sb[i - 1]; }
}
/* Go 1.22 runtime.nextslicecap. */
static u64 gosvm_nextcap(u64 newlen, u64 oldcap) {
    u64 newcap = oldcap, doublecap = oldcap + oldcap;
    if (newlen > doublecap) return newlen;
    if (oldcap < 256) return doublecap;
    while (newcap < newlen) newcap += (newcap + 768) >> 2;
    return newcap;
}
/* Go 1.22 malloc size classes (runtime/sizeclasses.go class_to_size). */
static const u32 gosvm_size_classes[68] = {0,8,16,24,32,48,64,80,96,112,128,144,160,176,192,208,224,240,256,288,320,352,384,416,448,480,512,576,640,704,768,896,1024,1152,1280,1408,1536,1792,2048,2304,2688,3072,3200,3456,4096,4864,5376,6144,6528,6784,6912,8192,9472,9728,10240,10880,12288,13568,14336,16384,18432,19072,20480,21760,24576,27264,28672,32768};
/* Go 1.22 runtime.roundupsize with malloc headers: objects holding pointers
   above 512 bytes carry an 8-byte header inside their size class. */
static u64 gosvm_roundupsize(u64 size, u64 scan) {
    u64 req = size;
    if (req <= 32768 - 8) {
        if (scan && req > 512) req += 8;
        for (u64 k = 0; k < 68; k++) if (gosvm_size_classes[k] >= req) return gosvm_size_classes[k] - (req - size);
    }
    req += 8191;
    if (req < size) return size;
    return req & ~8191ULL;
}
/* append's growth: Go 1.22 runtime.growslice for an element of size bytes.
   s is never written: on SBF it may be the caller's variable (byval). */
static slice gosvm_grow(slice s, u64 num, u64 size, u64 scan) {
    u64 oldlen = (u64)s.len, newlen = oldlen + num;
    if (newlen <= (u64)s.cap) return (slice){s.ptr, (i64)newlen, s.cap};
    u64 newcap = gosvm_nextcap(newlen, (u64)s.cap);
    if (newcap > GOSVM_HEAP_BYTES / size) abort();
    newcap = gosvm_roundupsize(newcap * size, scan) / size;
    u8 *p = gosvm_alloc(newcap * size);
    gosvm_move(p, s.ptr, oldlen * size);
    return (slice){p, (i64)newlen, (i64)newcap};
}
/* make: len > cap, or a capacity beyond Go's 2^48-byte maxAlloc, is out of
   range; a capacity that does not fit the remaining heap is out of memory. */
static slice gosvm_make(u64 len, u64 cap, u64 size) {
    if (len > cap || cap > (1ULL << 48) / size) abort();
    return (slice){gosvm_alloc(cap * size), (i64)len, (i64)cap};
}
static i64 gosvm_copy(slice d, slice s, u64 size) {
    u64 n = (u64)(d.len < s.len ? d.len : s.len);
    gosvm_move(d.ptr, s.ptr, n * size);
    return (i64)n;
}
static inline u8 *gosvm_at(slice s, u64 i, u64 size) { if (i >= (u64)s.len) abort(); return s.ptr + i * size; }
static inline slice gosvm_view_n(slice s, u64 lo, u64 hi, u64 maximum, u64 size) {
    if (lo > hi || hi > maximum || maximum > (u64)s.cap) abort();
    return (slice){s.ptr ? s.ptr + lo * size : 0, hi - lo, maximum - lo};
}
`

func heapRuntimeFor(heapBytes int) string {
	if heapBytes == 0 {
		heapBytes = DefaultHeapBytes
	}
	return memorySyscallDecls() + strings.Replace(heapRuntime, "@HEAP@", fmt.Sprint(heapBytes), 1)
}

// usesAllocation reports whether the program needs the heap runtime: it
// calls new/make/append/copy, writes a slice literal or an elided &T{...},
// moves a local to the heap, or has a slice of non-byte elements or a
// pointer/slice struct field, in non-generic code or in an instance of a
// generic declaration. Other programs emit byte-identical C.
func usesAllocation(program *CheckedProgram) bool {
	for _, fn := range program.Functions {
		if len(fn.HeapVars) > 0 || len(fn.HeapLiterals) > 0 {
			return true
		}
	}
	info := program.Info
	seen := map[types.Type]bool{}
	var mentions func(types.Type) bool
	mentions = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Slice:
			return !types.Identical(t.Elem(), types.Typ[types.Uint8]) || mentions(t.Elem())
		case *types.Named:
			if pkg := t.Obj().Pkg(); pkg != nil && pkg.Path() == "gosvm/solana" {
				return false // SDK types such as Context are opaque runtime values
			}
			return mentions(t.Underlying())
		case *types.Pointer:
			return mentions(t.Elem())
		case *types.Array:
			return mentions(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				switch t.Field(i).Type().Underlying().(type) {
				case *types.Pointer, *types.Slice:
					return true
				}
				if mentions(t.Field(i).Type()) {
					return true
				}
			}
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				if mentions(t.At(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			return mentions(t.Params()) || mentions(t.Results()) || t.Recv() != nil && mentions(t.Recv().Type())
		}
		return false
	}
	found := false
	// Generic declarations are judged by their instances, never by their
	// type-parameter bodies.
	for _, file := range program.lowered {
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			switch n := n.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok {
					if b, ok := info.Uses[id].(*types.Builtin); ok && allocBuiltins[b.Name()] {
						found = true
					}
				}
			case *ast.CompositeLit:
				switch info.Types[n].Type.(type) {
				case *types.Slice, *types.Pointer:
					found = true
				}
			case *ast.Ident:
				if o := info.Defs[n]; o != nil && mentions(o.Type()) {
					found = true
				}
			}
			if e, ok := n.(ast.Expr); ok && !found {
				if tv, ok := info.Types[e]; ok && mentions(tv.Type) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}

// element is the storage of a non-byte slice's element i, checked against
// the length when the expression is evaluated.
func (g *generator) element(n ast.Node, t types.Type, x, i string) string {
	elem := g.variable(n, t.(*types.Slice).Elem())
	return fmt.Sprintf("(*(%s *)gosvm_at(%s,%s,sizeof(%s)))", elem, x, i, elem)
}

// length converts a make operand to u64, aborting for a negative signed
// value as Go's makeslice panics.
func (g *generator) length(e ast.Expr) string {
	v := g.expr(e)
	if b, ok := g.info.Types[e].Type.Underlying().(*types.Basic); ok && b.Info()&types.IsUnsigned == 0 && b.Info()&types.IsUntyped == 0 {
		g.line("if ((i64)%s < 0) abort();", v)
	}
	return "(u64)" + v
}

// box moves a value of type t to a new heap allocation and returns the
// pointer.
func (g *generator) box(n ast.Node, t types.Type, value string) string {
	elem := g.variable(n, t)
	p := g.temp(n, types.NewPointer(t), fmt.Sprintf("(%s *)gosvm_alloc(sizeof(%s))", elem, elem))
	g.line("*%s = %s;", p, value)
	return p
}

func (g *generator) allocBuiltin(e *ast.CallExpr, name string) string {
	tv := g.info.Types[e]
	switch name {
	case "new":
		elem := g.variable(e, tv.Type.(*types.Pointer).Elem())
		return g.temp(e, tv.Type, fmt.Sprintf("(%s *)gosvm_alloc(sizeof(%s))", elem, elem))
	case "make":
		if isMap(tv.Type) {
			return g.mapMake(e, tv.Type)
		}
		elem := g.variable(e, tv.Type.(*types.Slice).Elem())
		length := g.length(e.Args[1])
		capacity := length
		if len(e.Args) > 2 {
			capacity = g.length(e.Args[2])
		}
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_make(%s,%s,sizeof(%s))", length, capacity, elem))
	case "append":
		s := tv.Type.(*types.Slice)
		elem := g.variable(e, s.Elem())
		scan := 0
		if containsReferences(s.Elem()) {
			scan = 1
		}
		base := g.exprAs(e.Args[0], s)
		if e.Ellipsis.IsValid() {
			extra := g.exprAs(e.Args[1], s)
			if isString(g.info.Types[e.Args[1]].Type) {
				extra = g.temp(e.Args[1], s, asSlice(extra)) // append(b, s...)
			}
			r := g.temp(e, s, fmt.Sprintf("gosvm_grow(%s,(u64)%s.len,sizeof(%s),%d)", base, extra, elem, scan))
			g.line("gosvm_move(%s.ptr+((u64)%s.len-(u64)%s.len)*sizeof(%s),%s.ptr,(u64)%s.len*sizeof(%s));", r, r, extra, elem, extra, extra, elem)
			return r
		}
		var values []string
		for _, value := range e.Args[1:] {
			values = append(values, g.exprAs(value, s.Elem()))
		}
		if len(values) == 0 {
			return base
		}
		r := g.temp(e, s, fmt.Sprintf("gosvm_grow(%s,%dULL,sizeof(%s),%d)", base, len(values), elem, scan))
		for j, v := range values {
			g.line("((%s *)%s.ptr)[(u64)%s.len-%dULL] = %s;", elem, r, r, len(values)-j, v)
		}
		return r
	case "copy":
		t := g.info.Types[e.Args[0]].Type
		dst := g.exprAs(e.Args[0], t)
		src := g.exprAs(e.Args[1], t)
		if isString(g.info.Types[e.Args[1]].Type) {
			src = asSlice(src) // copy(b, s)
		}
		elem := g.variable(e, t.(*types.Slice).Elem())
		return g.temp(e, tv.Type, fmt.Sprintf("gosvm_copy(%s,%s,sizeof(%s))", dst, src, elem))
	}
	g.unchecked(e)
	return ""
}

// sliceLiteral allocates the literal's array, then evaluates and stores the
// elements in source order; omitted elements stay zero.
func (g *generator) sliceLiteral(e *ast.CompositeLit, s *types.Slice) string {
	elem := g.variable(e, s.Elem())
	n := literalLength(g.info, e)
	r := g.temp(e, s, fmt.Sprintf("gosvm_make(%dULL,%dULL,sizeof(%s))", n, n, elem))
	index := int64(0)
	for _, elt := range e.Elts {
		value := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			index, _ = constant.Int64Val(g.info.Types[kv.Key].Value)
			value = kv.Value
		}
		v := g.exprAs(value, s.Elem())
		g.line("((%s *)%s.ptr)[%d] = %s;", elem, r, index, v)
		index++
	}
	return r
}

// declareMoved declares a heap-moved local: storage is allocated at the
// declaration, so every execution (each loop iteration) gets fresh storage.
func (g *generator) declareMoved(n ast.Node, o types.Object, value string) {
	t := g.variable(n, o.Type())
	raw := g.rawName(o)
	g.line("%s *%s = (%s *)gosvm_alloc(sizeof(%s));", t, raw, t, t)
	if value != "{0}" {
		g.line("*%s = %s;", raw, value)
	}
}

// movedParameter is the C name of an incoming value that the prologue
// copies to the heap.
func (g *generator) movedParameter(o types.Object) string { return g.rawName(o) + "_arg" }

func isParameter(sig *types.Signature, v *types.Var) bool {
	for i := 0; i < sig.Params().Len(); i++ {
		if sig.Params().At(i) == v {
			return true
		}
	}
	return false
}

// movedPrologue copies heap-moved parameters and receivers to the heap at
// function entry.
func (g *generator) movedPrologue(fn *ast.FuncDecl) {
	for _, o := range g.movedParams[fn] {
		g.declareMoved(fn, o, g.movedParameter(o))
	}
}
