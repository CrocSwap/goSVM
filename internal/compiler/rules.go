package compiler

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// frontend is the state shared by the subset checker and the C emitter: the
// type-checked syntax plus the value types declared so far. Each pass owns its
// own frontend so that "declared before use" follows that pass's traversal.
type frontend struct {
	fset    *token.FileSet
	info    *types.Info
	arrays  []arrayType  // array value types declared so far, in declaration order
	results []resultType // multiple-result tuples declared so far
}

func newFrontend(fset *token.FileSet, info *types.Info) *frontend {
	return &frontend{fset: fset, info: info}
}

type compileError string

func (e compileError) Error() string { return string(e) }

// fail aborts the current pass with a source-located diagnostic. Callers
// recover compileError at the public API boundary.
func (f *frontend) fail(n ast.Node, format string, args ...any) {
	panic(compileError(fmt.Sprintf("%s: %s", f.fset.Position(n.Pos()), fmt.Sprintf(format, args...))))
}

// recoverCompileError converts a pass abort into an ordinary error.
func recoverCompileError(err *error) {
	if r := recover(); r != nil {
		e, ok := r.(compileError)
		if !ok {
			panic(r)
		}
		*err = e
	}
}

// The type rules below are the single definition of which Go types have an
// on-chain value representation. Check applies them; the emitter only maps
// already-certified types to C.

// typeRule admits unsigned scalars (uint8, uint16, uint32, uint64), bool,
// untyped integer/bool constants, int (only as the result of len/cap or a
// range key; variableRule keeps it out of all other storage), byte slices,
// solana.Context, value structs, declared scalar arrays, declared
// multiple-result tuples and pointers to admitted elements.
func (f *frontend) typeRule(n ast.Node, t types.Type) {
	if tuple, ok := t.(*types.Tuple); ok && tuple.Len() > 1 {
		f.lookupResult(n, tuple)
		return
	}
	if p, ok := pointer(t); ok {
		f.pointerRule(n, p)
		return
	}
	if isContext(t) {
		return
	}
	if s, ok := t.(*types.Slice); ok && types.Identical(s.Elem(), types.Typ[types.Uint8]) {
		return
	}
	if a, ok := t.Underlying().(*types.Array); ok {
		f.lookupArray(n, a)
		return
	}
	if named, ok := t.(*types.Named); ok {
		if named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "gosvm/solana" {
			f.fail(n, "only Context is available as an on-chain SDK type")
		}
		if _, ok := named.Underlying().(*types.Struct); ok {
			return
		}
	}
	if b, ok := t.Underlying().(*types.Basic); ok {
		switch b.Kind() {
		case types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.UntypedInt, types.Bool, types.UntypedBool, types.Int:
			return
		}
	}
	f.fail(n, "unsupported type %s", t)
}

// variableRule applies to storage: variables, parameters, results, fields,
// conversions and array elements. Signed int is never storage.
func (f *frontend) variableRule(n ast.Node, t types.Type) {
	if types.Identical(t.Underlying(), types.Typ[types.Int]) {
		f.fail(n, "use explicitly typed unsigned integers; int variables are unsupported")
	}
	f.typeRule(n, t)
}

// pointerRule admits pointers to scalars, value structs and scalar arrays.
func (f *frontend) pointerRule(n ast.Node, p *types.Pointer) {
	t := p.Elem()
	if isContext(t) {
		f.fail(n, "pointers to Context unsupported")
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Signature, *types.Interface:
		f.fail(n, "pointer elements must be unsigned/bool scalars, value structs or scalar arrays")
	}
	f.variableRule(n, t)
}

// arrayRule bounds an array value type before it is declared.
func (f *frontend) arrayRule(node ast.Node, a *types.Array) {
	b, ok := a.Elem().Underlying().(*types.Basic)
	if !ok || (b.Kind() != types.Uint8 && b.Kind() != types.Uint16 && b.Kind() != types.Uint32 && b.Kind() != types.Uint64 && b.Kind() != types.Bool) {
		f.fail(node, "array elements must be byte, uint16, uint32, uint64, bool, or named versions of those scalars")
	}
	f.variableRule(node, a.Elem())
	size := (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(a.Elem())
	if a.Len() > maxArrayBytes/size {
		f.fail(node, "array storage exceeds experimental %d-byte per-value limit", maxArrayBytes)
	}
}

// constantRule requires integer constants to be representable as uint64.
func (f *frontend) constantRule(e ast.Expr, v constant.Value) {
	if v.Kind() == constant.Bool {
		return
	}
	if _, ok := constant.Uint64Val(v); !ok {
		f.fail(e, "constant is outside uint64")
	}
}

func (f *frontend) lookupArray(node ast.Node, a *types.Array) arrayType {
	for _, entry := range f.arrays {
		if types.Identical(entry.typ, a) {
			return entry
		}
	}
	f.fail(node, "array type was not declared before use")
	return arrayType{}
}

func (f *frontend) declaredArray(a *types.Array) bool {
	for _, entry := range f.arrays {
		if types.Identical(entry.typ, a) {
			return true
		}
	}
	return false
}

func (f *frontend) lookupResult(node ast.Node, t *types.Tuple) resultType {
	for _, r := range f.results {
		if types.Identical(r.typ, t) {
			return r
		}
	}
	f.fail(node, "result type was not declared before use")
	return resultType{}
}

func (f *frontend) declaredResult(t *types.Tuple) bool {
	for _, r := range f.results {
		if types.Identical(r.typ, t) {
			return true
		}
	}
	return false
}
