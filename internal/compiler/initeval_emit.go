package compiler

import (
	"encoding/binary"
	"fmt"
	"go/types"
	"strings"
)

// C emission of the initialization image (initeval.go): the heap's first
// bytes as they are after the evaluated prefix, in the C backend's layout,
// copied into place at entry; then the pointers to read-only data are
// stored, and the remaining initialization steps run.

// imageCopyRuntime is the copy routine: the VM's sol_memcpy_ syscall on SBF
// (static hash on v3, the legacy import on v0), a loop elsewhere.
const imageCopyRuntime = `
#ifdef __bpf__
#ifdef GOSVM_SBF_V3
#define gosvm_image_copy ((void (*)(u8 *,const u8 *,u64))0x717cc4a3ULL)
#else
extern void sol_memcpy_(u8 *,const u8 *,u64);
#define gosvm_image_copy sol_memcpy_
#endif
#else
static void gosvm_image_copy(u8 *d,const u8 *s,u64 n) { for (u64 i = 0; i < n; i++) d[i] = s[i]; }
#endif
`

// imageStoreWords is the largest image stored word by word rather than
// copied with sol_memcpy_ (which costs at least 10 CU).
const imageStoreWords = 4

// imageWord is one 8-byte word of the image: a number, or the address of
// heap storage (absolute: the heap is at a fixed address on SBF).
type imageWord struct {
	bits    uint64
	heap    bool
	heapOff int64
}

// imageFixup is a pointer to read-only data, stored after the copy.
type imageFixup struct {
	off   int64  // heap offset of the pointer word
	value string // C expression of the address
}

type imageBuilder struct {
	g      *generator
	p      *CheckedProgram
	words  []imageWord
	fixups []imageFixup
}

func (b *imageBuilder) ensure(end int64) {
	for int64(len(b.words))*8 < end {
		b.words = append(b.words, imageWord{})
	}
}

// putBytes stores raw bytes at heap offset off.
func (b *imageBuilder) putBytes(off int64, data []byte) {
	b.ensure(off + int64(len(data)))
	for i, c := range data {
		w := &b.words[(off+int64(i))/8]
		shift := uint(((off + int64(i)) % 8) * 8)
		w.bits = w.bits&^(0xff<<shift) | uint64(c)<<shift
	}
}

func (b *imageBuilder) putInt(off int64, v uint64, size int64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	b.putBytes(off, buf[:size])
}

// putHeap stores the address of heap offset target at off (8-aligned).
func (b *imageBuilder) putHeap(off, target int64) {
	b.ensure(off + 8)
	b.words[off/8] = imageWord{heap: true, heapOff: target}
}

func (b *imageBuilder) putFixup(off int64, expr string) {
	b.ensure(off + 8)
	b.words[off/8] = imageWord{}
	b.fixups = append(b.fixups, imageFixup{off, expr})
}

// cFieldOffset is the C offset of field i of struct st.
func cFieldOffset(st *types.Struct, i int) int64 {
	sizes := &types.StdSizes{WordSize: 8, MaxAlign: 8}
	offset := int64(0)
	for j := 0; j <= i; j++ {
		ft := st.Field(j).Type()
		a := sizes.Alignof(ft)
		offset = (offset + a - 1) / a * a
		if j == i {
			return offset
		}
		offset += cSizeof(ft)
	}
	return offset
}

// pathOffset is the byte offset and type of the storage path selects in a
// value of type t.
func pathOffset(t types.Type, path []int) (int64, types.Type) {
	off := int64(0)
	for _, i := range path {
		switch u := t.Underlying().(type) {
		case *types.Struct:
			off += cFieldOffset(u, i)
			t = u.Field(i).Type()
		case *types.Array:
			off += int64(i) * cSizeof(u.Elem())
			t = u.Elem()
		default:
			panic("path into " + t.String())
		}
	}
	return off, t
}

// putRef stores the address of ref plus addend bytes at off.
func (b *imageBuilder) putRef(off int64, ref *InitRef, addend int64) {
	switch {
	case ref.HeapBase:
		b.putHeap(off, addend)
	case ref.Object != nil:
		o := ref.Object
		var root types.Type
		switch o.Kind {
		case InitObjectValue:
			root = o.Type
		case InitObjectArray:
			root = types.NewArray(o.Type, int64(len(o.Value.Elems)))
		}
		at := int64(0)
		if len(ref.Path) > 0 {
			at, _ = pathOffset(root, ref.Path)
		}
		b.putHeap(off, o.Offset+at+addend)
	case ref.Global != nil && !ref.Global.ReadOnly:
		at, _ := pathOffset(ref.Global.Var.Type(), ref.Path)
		b.putHeap(off, 8+ref.Global.Offset+at+addend)
	case ref.Global != nil:
		index := -1
		for i, g := range b.p.Globals {
			if g == ref.Global {
				index = i
			}
		}
		t := ref.Global.Var.Type()
		if s, ok := t.(*types.Slice); ok {
			// A read-only slice's backing array.
			t = types.NewArray(s.Elem(), 1<<20)
		}
		at, _ := pathOffset(t, ref.Path)
		b.putFixup(off, fmt.Sprintf("(u64)((const u8 *)&gosvm_ro%d + %d)", index, at+addend))
	default:
		panic("empty reference")
	}
}

// putValue stores v (of type t) at heap offset off in C layout.
func (b *imageBuilder) putValue(off int64, v *InitValue, t types.Type) {
	switch v.Kind {
	case InitInt:
		b.putInt(off, v.Bits, cSizeof(t))
	case InitBool:
		b.putInt(off, v.Bits, 1)
	case InitString:
		if len(v.Bytes) > 0 {
			if v.Ref != nil {
				b.putRef(off, v.Ref, v.Low)
			} else {
				b.putFixup(off, "(u64)"+stringLiteralBytes(v.Bytes))
			}
		}
		b.putInt(off+8, uint64(len(v.Bytes)), 8)
	case InitPointer:
		if v.Ref != nil {
			b.putRef(off, v.Ref, 0)
		}
	case InitSlice:
		if v.Ref != nil {
			b.putRef(off, v.Ref, v.Low*cSizeof(t.Underlying().(*types.Slice).Elem()))
		}
		b.putInt(off+8, uint64(v.Len), 8)
		b.putInt(off+16, uint64(v.Cap), 8)
	case InitInterface:
		d := v.Dynamic
		if d == nil {
			return
		}
		b.putInt(off, d.ID, 8)
		switch {
		case d.Direct:
			b.putValue(off+8, v.Elem, d.Type)
		case d.Size == 0:
			b.putHeap(off+8, 0)
		default:
			b.putRef(off+8, v.Ref, 0)
		}
	case InitFuncValue:
		switch {
		case v.Target == nil:
		case v.Ref == nil:
			b.putInt(off, v.Target.ID, 8)
		default:
			b.putRef(off, v.Ref, 0)
		}
	case InitMapValue:
		if v.Ref != nil {
			b.putRef(off, v.Ref, 0)
		}
	case InitStruct:
		st := t.Underlying().(*types.Struct)
		for i, f := range v.Elems {
			b.putValue(off+cFieldOffset(st, i), f, st.Field(i).Type())
		}
	case InitArray:
		elem := t.Underlying().(*types.Array).Elem()
		size := cSizeof(elem)
		for i, e := range v.Elems {
			b.putValue(off+int64(i)*size, e, elem)
		}
	default:
		panic(fmt.Sprintf("image value kind %d", v.Kind))
	}
}

func (b *imageBuilder) putObject(o *InitObject) {
	switch o.Kind {
	case InitObjectValue:
		b.putValue(o.Offset, o.Value, o.Type)
	case InitObjectArray:
		b.putValue(o.Offset, o.Value, types.NewArray(o.Type, int64(len(o.Value.Elems))))
	case InitObjectBytes:
		b.putBytes(o.Offset, o.Bytes)
	case InitObjectMap:
		m := o.Map
		b.putInt(o.Offset, uint64(len(m.Entries)), 8)
		b.putInt(o.Offset+8, uint64(m.Cap), 8)
		if m.Slots != nil {
			b.putHeap(o.Offset+16, m.Slots.Offset)
		}
	case InitObjectMapSlots:
		b.putSlots(o)
	case InitObjectFunc:
		f := o.Func
		b.putInt(o.Offset, f.Target.ID, 8)
		for i, c := range f.Captures {
			b.putRef(o.Offset+8*int64(i+1), c, 0)
		}
		if f.Receiver != nil {
			b.putValue(o.Offset+8, f.Receiver, f.Target.Func.Type().(*types.Signature).Recv().Type())
		}
	default:
		panic(fmt.Sprintf("image object kind %d", o.Kind))
	}
}

// putSlots lays out a map's slot array: each live entry, in insertion
// order, at the first empty slot from its hash's home slot (the table
// gosvm_mapN_set builds; any such table answers lookups identically).
func (b *imageBuilder) putSlots(o *InitObject) {
	m := o.Map
	mt := m.Type
	mask := uint64(m.Cap - 1)
	used := make([]bool, m.Cap)
	for _, e := range m.Entries {
		h := mapHash(e.Key, mt.Type.Key())
		i := h & mask
		for used[i] {
			i = (i + 1) & mask
		}
		used[i] = true
		slot := o.Offset + int64(i)*mt.SlotSize
		b.putInt(slot, h, 8)
		b.putValue(slot+8, e.Key, mt.Type.Key())
		b.putValue(slot+8+roundUp8(mt.KeySize), e.Elem, mt.Type.Elem())
	}
}

// mapHash is gosvm_mapN_hash for a key of plainKey type t.
func mapHash(k *InitValue, t types.Type) uint64 {
	h := uint64(0x243F6A8885A308D3)
	h = hashValue(h, k, t)
	h ^= h >> 31
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 29
	return h | 1<<63
}

func hmix(h, w uint64) uint64 {
	h = (h ^ w) * 0x9E3779B97F4A7C15
	return h ^ (h >> 32)
}

func hashValue(h uint64, v *InitValue, t types.Type) uint64 {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch v.Kind {
		case InitString:
			h = hmix(h, uint64(len(v.Bytes)))
			w, n := uint64(0), 0
			for _, c := range v.Bytes {
				w |= uint64(c) << (8 * n)
				if n++; n == 8 {
					h, w, n = hmix(h, w), 0, 0
				}
			}
			if n > 0 {
				h = hmix(h, w)
			}
			return h
		case InitBool:
			return hmix(h, v.Bits)
		}
		return hmix(h, widen(v.Bits, t))
	case *types.Array:
		for _, e := range v.Elems {
			h = hashValue(h, e, u.Elem())
		}
		return h
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if u.Field(i).Name() != "_" {
				h = hashValue(h, v.Elems[i], u.Field(i).Type())
			}
		}
		return h
	}
	panic("unhashable image key")
}

// stringLiteralBytes spells bytes as a C string literal (stringLiteral's
// escaping) of type u8 *.
func stringLiteralBytes(data []byte) string {
	var s strings.Builder
	for _, c := range data {
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' && c != '?' {
			s.WriteByte(c)
		} else {
			fmt.Fprintf(&s, "\\%03o", c)
		}
	}
	return fmt.Sprintf("((u8 *)\"%s\")", s.String())
}

// buildImage lays out the image of p.InitImage.
func buildImage(g *generator, p *CheckedProgram) *imageBuilder {
	img := p.InitImage
	b := &imageBuilder{g: g, p: p}
	b.putInt(0, uint64(img.HeapUsed), 8)
	for i, global := range p.Globals {
		if v := img.Values[i]; v != nil {
			b.putValue(8+global.Offset, v, global.Var.Type())
		}
	}
	for _, o := range img.Objects {
		b.putObject(o)
	}
	// Trailing zero words need no copy: the heap starts zeroed.
	for len(b.words) > 1 {
		w := b.words[len(b.words)-1]
		if w.heap || w.bits != 0 {
			break
		}
		b.words = b.words[:len(b.words)-1]
	}
	return b
}

// emitImage defines the image data and returns the C statements that
// install it.
func (g *generator) emitImage(p *CheckedProgram) []string {
	b := buildImage(g, p)
	var items []string
	for _, w := range b.words {
		if w.heap {
			items = append(items, fmt.Sprintf("(u64)(GOSVM_HEAP_BASE+%d)", w.heapOff))
		} else {
			items = append(items, fmt.Sprintf("%dULL", w.bits))
		}
	}
	g.line("/* The heap after the compile-time evaluated initialization (docs/GO_GLOBALS.md#compile-time-evaluation): %d steps of %d, %d bytes in use. */", p.InitImage.Steps, p.InitImage.Total, p.InitImage.HeapUsed)
	var out []string
	if heapBytes(p) > 32768 {
		// The first allocation touches the budget's last byte (gosvm_alloc).
		out = append(out, "(void)*(volatile u8 *)(GOSVM_HEAP_BASE + GOSVM_HEAP_BYTES - 1);")
	}
	if len(items) <= imageStoreWords {
		// A few words are cheaper to store than to copy.
		for i, item := range items {
			if item != "0ULL" {
				out = append(out, fmt.Sprintf("((u64 *)GOSVM_HEAP_BASE)[%d] = %s;", i, item))
			}
		}
	} else {
		g.WriteString(imageCopyRuntime)
		g.line("static const u64 gosvm_image[%d] = {%s};", len(items), strings.Join(items, ","))
		out = append(out, fmt.Sprintf("gosvm_image_copy(GOSVM_HEAP_BASE,(const u8 *)gosvm_image,%dULL);", 8*len(items)))
	}
	for _, f := range b.fixups {
		out = append(out, fmt.Sprintf("*(u64 *)(GOSVM_HEAP_BASE+%d) = %s;", f.off, f.value))
	}
	return out
}

func heapBytes(p *CheckedProgram) int {
	if p.Program.HeapBytes == 0 {
		return DefaultHeapBytes
	}
	return p.Program.HeapBytes
}
