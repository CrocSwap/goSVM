package compiler

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Read-only pointer parameters (docs/GO_GLOBALS.md#read-only-pointer-parameters). A
// package-level variable whose element or field address is passed only to
// parameters that never write through it, store it or let it escape stays
// read-only data:
//
//	var pows = [60]Int{...}          // holiman/uint256, 1,920 bytes
//	func (z *Int) Lt(x *Int) bool    // reads x[0..3] only
//	... z.Lt(&pows[t-20]) ...        // pows stays read-only (zero CU)
//
// A pointer parameter p is read-only when, in its function's body, p is
// never assigned, addressed or captured by a function literal, and every use
// of p is one of:
//
//   - a read of the storage it points to (*p, p[i] and p.f through Go's
//     automatic dereference, and the field and element paths below them),
//     classified like a package-level variable's storage (globalStorageWrites):
//     no assignment, ++/--, range assignment, address (unless itself passed
//     to a read-only parameter), slicing or pointer-receiver call (unless
//     its receiver is read-only);
//   - a comparison with == or !=;
//   - an argument of len or cap, or of a static call of a declared function
//     or method whose corresponding parameter is read-only;
//   - the receiver of a static call of a method whose pointer receiver is
//     read-only, or of a value-receiver method on plain data (a copy).
//
// Anything else (returning p, storing it, converting it, a dynamic call,
// a generic or variadic callee, a function without a Go body or an
// intrinsic) makes p not read-only. Recursion is rejected elsewhere; a cycle
// here is treated as not read-only. The analysis is sound in the sense of
// globalWrites: every pointer to a variable's storage is created at an
// analyzed address operation and flows only into read-only parameters, which
// neither write through it nor let it outlive the call.

// roParam identifies a parameter: index -1 is the receiver.
type roParam struct {
	fn    *types.Func
	index int
}

// readOnlyPointers holds the declarations the analysis may follow and its
// memoized results.
type readOnlyPointers struct {
	decls map[*types.Func]*ast.FuncDecl
	memo  map[roParam]int8 // 1 read-only, -1 not, 0 in progress
}

func (f *frontend) newReadOnlyPointers(roots []ast.Node) *readOnlyPointers {
	r := &readOnlyPointers{decls: map[*types.Func]*ast.FuncDecl{}, memo: map[roParam]int8{}}
	for _, root := range roots {
		fn, ok := root.(*ast.FuncDecl)
		if !ok || fn.Body == nil || stdlibIntrinsic(f.fset, fn) != "" {
			continue
		}
		if o, ok := f.info.Defs[fn.Name].(*types.Func); ok {
			r.decls[o] = fn
		}
	}
	return r
}

// readOnlyParam reports whether parameter index (-1: the receiver) of fn is
// a read-only pointer parameter.
func (f *frontend) readOnlyParam(fn *types.Func, index int) bool {
	r := f.roPointers
	if r == nil || fn == nil {
		return false
	}
	key := roParam{fn, index}
	if v, ok := r.memo[key]; ok {
		return v == 1
	}
	r.memo[key] = 0
	ok := f.analyzeReadOnlyParam(fn, index)
	r.memo[key] = -1
	if ok {
		r.memo[key] = 1
	}
	return ok
}

func (f *frontend) analyzeReadOnlyParam(fn *types.Func, index int) bool {
	decl := f.roPointers.decls[fn]
	if decl == nil {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		return false
	}
	var v *types.Var
	if index < 0 {
		if sig.Recv() == nil {
			return false
		}
		v = sig.Recv()
	} else {
		if index >= sig.Params().Len() || sig.Variadic() && index == sig.Params().Len()-1 {
			return false
		}
		v = sig.Params().At(index)
	}
	if _, ok := v.Type().Underlying().(*types.Pointer); !ok {
		return false
	}
	if v.Name() == "" || v.Name() == "_" {
		return true // never used
	}
	ok := true
	var stack []ast.Node
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if !ok {
			return false
		}
		stack = append(stack, n)
		id, isIdent := n.(*ast.Ident)
		if !isIdent || f.info.Uses[id] != v {
			return true
		}
		for _, outer := range stack {
			if _, isLit := outer.(*ast.FuncLit); isLit {
				ok = false // captured by a closure
				return false
			}
		}
		if !f.pointerUseReads(stack) {
			ok = false
		}
		return true
	})
	return ok
}

// pointerUseReads classifies a use of a pointer-typed parameter whose
// identifier ends stack.
func (f *frontend) pointerUseReads(stack []ast.Node) bool {
	cur := stack[len(stack)-1]
	for i := len(stack) - 2; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.StarExpr:
			return !f.globalStorageWrites(stack[:i+1], i-1, p, true)
		case *ast.IndexExpr:
			if p.X != cur {
				return false
			}
			return !f.globalStorageWrites(stack[:i+1], i-1, p, true)
		case *ast.SelectorExpr:
			if p.X != cur {
				return false
			}
			s := f.info.Selections[p]
			if s == nil {
				return false
			}
			if s.Kind() == types.FieldVal {
				return !f.globalStorageWrites(stack[:i+1], i-1, p, true)
			}
			method, _ := f.info.Uses[p.Sel].(*types.Func)
			if s.Kind() != types.MethodVal || method == nil || i == 0 {
				return false
			}
			call, isCall := stack[i-1].(*ast.CallExpr)
			if !isCall || call.Fun != p {
				return false // a method value
			}
			recv := method.Type().(*types.Signature).Recv()
			if _, pointerRecv := recv.Type().(*types.Pointer); pointerRecv {
				return f.readOnlyParam(method, -1)
			}
			return plainData(recv.Type()) // copies the plain-data value
		case *ast.BinaryExpr:
			return p.Op == token.EQL || p.Op == token.NEQ
		case *ast.CallExpr:
			return f.readOnlyArgument(p, cur)
		}
		return false
	}
	return false
}

// readOnlyArgument reports whether arg, an argument of call, is passed to
// len or cap or to a read-only pointer parameter of a static callee.
func (f *frontend) readOnlyArgument(call *ast.CallExpr, arg ast.Node) bool {
	index := -1
	for j, a := range call.Args {
		if a == arg {
			index = j
		}
	}
	if index < 0 || call.Ellipsis.IsValid() {
		return false
	}
	var callee types.Object
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		callee = f.info.Uses[fun]
	case *ast.SelectorExpr:
		if s := f.info.Selections[fun]; s != nil && s.Kind() != types.MethodVal {
			return false // a field holding a function or a method expression
		}
		callee = f.info.Uses[fun.Sel]
	}
	if b, ok := callee.(*types.Builtin); ok {
		return b.Name() == "len" || b.Name() == "cap"
	}
	fn, ok := callee.(*types.Func)
	if !ok {
		return false
	}
	return f.readOnlyParam(fn, index)
}

// addressRead reports whether the address operation &x at stack[i] only
// reaches read-only pointer parameters: it is an argument of a static call
// whose parameter is read-only.
func (f *frontend) addressRead(stack []ast.Node, i int) bool {
	if f.roPointers == nil {
		return false
	}
	var cur ast.Node = stack[i]
	for j := i - 1; j >= 0; j-- {
		switch p := stack[j].(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.CallExpr:
			return f.readOnlyArgument(p, cur)
		}
		return false
	}
	return false
}

// pointerReceiverRead reports whether the pointer-receiver method selector
// at stack[i] is called directly and its receiver is read-only.
func (f *frontend) pointerReceiverRead(stack []ast.Node, i int, method *types.Func) bool {
	if f.roPointers == nil || i == 0 {
		return false
	}
	call, ok := stack[i-1].(*ast.CallExpr)
	if !ok || call.Fun != stack[i] {
		return false
	}
	return f.readOnlyParam(method, -1)
}
