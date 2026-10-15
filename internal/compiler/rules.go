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
	// instantiated resolves generic callees to their instances
	// (CheckedProgram.Instantiated); empty for programs without generics.
	instantiated map[*ast.Ident]*Instance
	// ifaces is the program's interface information (conversions, dynamic
	// types and calls); empty for programs without interface values.
	ifaces       *ifaceProgram
	ifaceChecked map[*types.Interface]bool // interface types interfaceRule admitted
	// usesStrings is set when typeRule admits a string type: the program
	// needs the string representation (docs/GO_STRINGS.md).
	usesStrings bool
	// funcs is the program's function-value information (targets, indirect
	// calls, closures and defer sites); empty for programs without them.
	funcs       *funcProgram
	funcChecked map[*types.Signature]bool // function types funcTypeRule admitted
	// maps records the map types typeRule admitted, by key
	// (docs/GO_MAPS.md); nil for programs without maps.
	maps map[string]*MapType
	// usesEmpty is set when typeRule admits the unnamed empty struct type
	// struct{}.
	usesEmpty bool
	// panics records each panic call's log line (CheckedProgram.Panics);
	// nil without panics.
	panics map[*ast.CallExpr]*PanicSite
}

func newFrontend(fset *token.FileSet, info *types.Info) *frontend {
	return &frontend{fset: fset, info: info, ifaces: &ifaceProgram{}, funcs: &funcProgram{}}
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

// typeRule admits integer scalars (uint8, uint16, uint32, uint64, uint, int8,
// int16, int32, int64, int), bool, strings, untyped integer/bool/string
// constants, slices of
// admitted elements (sliceRule), maps (mapRule), solana.Context, value structs, declared
// scalar arrays, declared multiple-result tuples and pointers to admitted
// elements.
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
	if s, ok := t.(*types.Slice); ok {
		if !types.Identical(s.Elem(), types.Typ[types.Uint8]) {
			f.sliceRule(n, s)
		}
		return
	}
	if a, ok := t.Underlying().(*types.Array); ok {
		f.lookupArray(n, a)
		return
	}
	if m, ok := t.Underlying().(*types.Map); ok {
		f.mapRule(n, m)
		return
	}
	if isEmptyStruct(t) {
		// struct{}: a zero-size value, as in map[K]struct{} sets.
		f.usesEmpty = true
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
		case types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uint, types.UntypedInt, types.Bool, types.UntypedBool,
			types.Int8, types.Int16, types.Int32, types.Int64, types.Int:
			return
		case types.String, types.UntypedString:
			f.usesStrings = true
			return
		}
	}
	if isIface(t) {
		f.interfaceRule(n, t)
		return
	}
	if isFuncValue(t) {
		f.funcTypeRule(n, t.Underlying().(*types.Signature))
		return
	}
	f.fail(n, "unsupported type %s", t)
}

// variableRule applies to storage: variables, parameters, results, fields,
// conversions and array elements. Every value type is also a storage type.
func (f *frontend) variableRule(n ast.Node, t types.Type) {
	f.typeRule(n, t)
}

// pointerRule admits pointers to scalars, value structs and scalar arrays.
func (f *frontend) pointerRule(n ast.Node, p *types.Pointer) {
	t := p.Elem()
	if isContext(t) {
		f.fail(n, "pointers to Context unsupported")
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Signature:
		// Pointers to interface values are admitted (the message predates them).
		f.fail(n, "pointer elements must be integer/bool scalars, value structs or scalar arrays")
	}
	f.variableRule(n, t)
}

// arrayRule bounds an array value type before it is declared. Elements may
// have any storable type (docs/GO_ARRAYS.md): scalars, strings, value
// structs, arrays, pointers, slices, maps, interfaces and function values.
func (f *frontend) arrayRule(node ast.Node, a *types.Array) {
	if isContext(a.Elem()) {
		f.fail(node, "arrays of Context unsupported")
	}
	// Array elements (and arrays behind pointer elements) are bounded here
	// too; they are declared before the array that holds them.
	f.typeArgRule(node, a.Elem())
	size := (&types.StdSizes{WordSize: 8, MaxAlign: 8}).Sizeof(a.Elem())
	if size > 0 && a.Len() > maxArrayBytes/size {
		f.fail(node, "array storage exceeds experimental %d-byte per-value limit", maxArrayBytes)
	}
}

// compositeElem reports whether an array of elem needs the composite-element
// lowering (docs/GO_ARRAYS.md): struct, array, pointer or slice elements.
// Arrays of scalars, strings, interfaces, maps and function values keep the
// original lowering, so their programs' C is unchanged.
func compositeElem(elem types.Type) bool {
	switch elem.Underlying().(type) {
	case *types.Struct, *types.Array, *types.Pointer, *types.Slice:
		return true
	}
	return false
}

// constantRule requires integer constants to be representable as uint64,
// except constants of a signed type, which go/types has already checked
// against that type's range.
func (f *frontend) constantRule(e ast.Expr, t types.Type, v constant.Value) {
	if v.Kind() == constant.Bool || v.Kind() == constant.String || isSigned(t) {
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

// isSigned reports whether t is a typed signed integer type (int8..int64,
// int, or a named version).
func isSigned(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&(types.IsUnsigned|types.IsUntyped) == 0
}

// intWidth is the width in bits of an admitted integer type.
func intWidth(t types.Type) int {
	switch t.Underlying().(*types.Basic).Kind() {
	case types.Uint8, types.Int8:
		return 8
	case types.Uint16, types.Int16:
		return 16
	case types.Uint32, types.Int32:
		return 32
	}
	return 64
}
