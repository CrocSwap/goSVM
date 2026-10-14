package compiler

import (
	"go/ast"
	"go/types"
)

// uint16Prelude is emitted only for programs that mention uint16, so the C
// of every program without it stays byte-identical to earlier compilers.
// Arithmetic is still computed in u64 and truncated by the store to u16.
const uint16Prelude = "typedef unsigned short u16;\n"

// usesUint16 reports whether any declared object or expression in the program
// files has a type that contains uint16.
func usesUint16(files []*ast.File, info *types.Info) bool {
	seen := map[types.Type]bool{}
	var mentions func(types.Type) bool
	mentions = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.Basic:
			return t.Kind() == types.Uint16
		case *types.Named:
			return mentions(t.Underlying())
		case *types.Pointer:
			return mentions(t.Elem())
		case *types.Array:
			return mentions(t.Elem())
		case *types.Slice:
			return mentions(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
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
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			if id, ok := n.(*ast.Ident); ok {
				if o := info.Defs[id]; o != nil && mentions(o.Type()) {
					found = true
				}
			}
			if e, ok := n.(ast.Expr); ok {
				if tv, ok := info.Types[e]; ok && mentions(tv.Type) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}
