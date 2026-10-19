package compiler

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

// Evaluation order (docs/ERROR_CONTRACT.md#evaluation-order).
//
// goSVM evaluates expressions in gc's (Go 1.22.0) order. The spec fixes the
// order of calls, method calls and receive operations but leaves open when
// plain operands (variable reads, field and element reads, indirections) are
// evaluated relative to them. gc's order pass (cmd/compile/internal/walk/
// order.go) evaluates every "sequenced" expression — a call, builtin call,
// &&/||, slice expression, and a few runtime operations listed in
// sequenced — into a temporary where it appears, after the sequenced
// expressions inside it, left to right; every other operand is read later,
// when the enclosing sequenced expression or the statement itself is
// evaluated.
//
// The emitter evaluates operands left to right, which is gc's order
// wherever no sequenced expression runs after a plain operand that it could
// observably change (by writing the operand's storage, or by logging before
// the operand's trap). Check finds the evaluation owners where it could and
// records, for each, the sequenced expressions to evaluate first
// (CheckedProgram.Hoisted); the emitter hoists exactly those, so programs
// where the order is not observable compile to the same C as before.

// evalOrder is Check's result; see the exported CheckedProgram fields.
type evalOrder struct {
	// analysis keeps the call summaries (mayWrite) for byval.go.
	analysis  *evalAnalysis
	hoisted   map[ast.Node][]ast.Expr
	converted map[ast.Expr]bool
	mapOrder  map[*ast.CompositeLit][]int
	shared    map[ast.Expr]ast.Expr
	sequenced map[ast.Expr]bool
}

// gcSizes are gc's sizes and alignments on 64-bit targets, which decide
// gc's runtime conversion and map-access paths.
var gcSizes = types.SizesFor("gc", "amd64")

// sdkPure are the gosvm/solana functions that write nothing a later operand
// read could observe; sdkLogs are those that log (a CPI logs the callee's
// invocation) or set return data.
var (
	sdkPure = map[string]bool{"Count": true, "Data": true, "Executable": true, "Instruction": true, "IsPDA": true,
		"IsTokenProgram": true, "Key": true, "Lamports": true, "Owner": true, "ProgramID": true, "Signer": true, "Writable": true}
	sdkLogs = map[string]bool{"Invoke": true, "Transfer": true, "TransferSigned": true, "SetReturnData": true}
)

type evalEvent struct {
	end      token.Pos
	writable bool // reads storage a call may write
	trap     bool // may abort
}

type evalOwner struct {
	key    ast.Node
	seq    []ast.Expr
	events []evalEvent
}

type evalEffect struct{ writes, logs bool }

type evalAnalysis struct {
	f         *frontend
	byObject  map[types.Object]*Function
	pinned    map[types.Object]bool
	targets   map[ast.Expr]bool
	early     map[ast.Expr]bool // map-append left-hand operands copied early
	logs      map[types.Object]int
	writes    map[types.Object]int
	effects   map[ast.Expr]evalEffect
	litsDone  map[*ast.FuncLit]bool
	appendOf  map[*ast.AssignStmt]*ast.CallExpr
	out       *evalOrder
	keyConvOf map[ast.Expr]bool
	written   map[types.Object]*funcSummary // funcWrites (byval.go)
}

// analyzeEvalOrder computes the evaluation-order lowering of every function
// body (including closures and package initializers).
func (f *frontend) analyzeEvalOrder(checked *CheckedProgram) *evalOrder {
	a := &evalAnalysis{f: f, byObject: map[types.Object]*Function{}, pinned: map[types.Object]bool{}, targets: map[ast.Expr]bool{},
		early: map[ast.Expr]bool{}, logs: map[types.Object]int{}, writes: map[types.Object]int{}, effects: map[ast.Expr]evalEffect{}, litsDone: map[*ast.FuncLit]bool{},
		appendOf: map[*ast.AssignStmt]*ast.CallExpr{}, keyConvOf: map[ast.Expr]bool{}, written: map[types.Object]*funcSummary{},
		out: &evalOrder{hoisted: map[ast.Node][]ast.Expr{}, converted: map[ast.Expr]bool{}, mapOrder: map[*ast.CompositeLit][]int{},
			shared: map[ast.Expr]ast.Expr{}, sequenced: map[ast.Expr]bool{}}}
	for _, fn := range checked.Functions {
		a.byObject[fn.Object] = fn
		for _, v := range fn.HeapVars {
			a.pinned[v] = true
		}
	}
	for _, cl := range f.funcs.closures {
		for _, v := range cl.Captures {
			a.pinned[v] = true
		}
	}
	var bodies []*ast.BlockStmt
	for _, fn := range checked.Functions {
		if fn.Library == nil && fn.Decl.Body != nil {
			bodies = append(bodies, fn.Decl.Body)
		}
	}
	for _, body := range bodies {
		a.prepare(body)
	}
	for _, conv := range f.ifaces.conversions {
		a.conversionOperand(conv)
	}
	for _, body := range bodies {
		a.body(body.List)
	}
	a.out.analysis = a
	return a.out
}

// prepare records assignment targets, address-taken locals and the
// sequenced expressions of one body (closures included).
func (a *evalAnalysis) prepare(body *ast.BlockStmt) {
	info := a.f.info
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				a.targets[ast.Unparen(l)] = true
			}
			a.mapAppend(n)
		case *ast.IncDecStmt:
			a.targets[ast.Unparen(n.X)] = true
		case *ast.RangeStmt:
			if n.Key != nil {
				a.targets[ast.Unparen(n.Key)] = true
			}
			if n.Value != nil {
				a.targets[ast.Unparen(n.Value)] = true
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				a.pin(n.X)
			}
		case *ast.SliceExpr:
			if _, ok := info.Types[n.X].Type.Underlying().(*types.Array); ok {
				a.pin(n.X)
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[n]; sel != nil && sel.Kind() == types.MethodVal {
				if fn, ok := sel.Obj().(*types.Func); ok {
					if _, wantPointer := pointer(fn.Type().(*types.Signature).Recv().Type()); wantPointer {
						if _, isPointer := pointer(info.Types[n.X].Type); !isPointer {
							a.pin(n.X)
						}
					}
				}
			}
		}
		if e, ok := n.(ast.Expr); ok && a.sequencedKind(e) {
			a.out.sequenced[e] = true
		}
		return true
	})
	// Map keys gc copies where they appear (slow map paths).
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IndexExpr:
			if !isMap(info.Types[n.X].Type) {
				return true
			}
			if t, ok := info.Types[n].Type.(*types.Tuple); ok && t != nil {
				return true // v, ok := m[k]: unobservable, the key is last
			}
			a.slowKey(mapOf(info.Types[n.X].Type), n.Index)
		case *ast.CompositeLit:
			if m, ok := info.Types[n].Type.Underlying().(*types.Map); ok {
				for _, elt := range n.Elts {
					kv := elt.(*ast.KeyValueExpr)
					if !a.staticEntry(kv) {
						a.slowKey(m, kv.Key)
					}
				}
			}
		}
		return true
	})
}

// pin marks the local variable at the root of an addressable operand whose
// address is taken: calls may write it.
func (a *evalAnalysis) pin(e ast.Expr) {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.Ident:
			if v, ok := a.f.info.Uses[x].(*types.Var); ok {
				a.pinned[v] = true
			}
		case *ast.SelectorExpr:
			if _, ok := pointer(a.f.info.Types[x.X].Type); !ok && a.f.info.Selections[x] != nil {
				e = x.X
				continue
			}
		case *ast.IndexExpr:
			if _, ok := a.f.info.Types[x.X].Type.Underlying().(*types.Array); ok {
				e = x.X
				continue
			}
		case *ast.CompositeLit:
		}
		return
	}
}

// sequencedKind reports whether gc evaluates e into a temporary where it
// appears (order.go's copyExpr cases), apart from the context-dependent map
// keys, conversion operands and map-append operands recorded separately.
func (a *evalAnalysis) sequencedKind(e ast.Expr) bool {
	info := a.f.info
	tv := info.Types[e]
	if tv.Value != nil {
		return false
	}
	switch e := e.(type) {
	case *ast.CallExpr:
		if info.Types[e.Fun].IsType() {
			k := classifyConversion(info, e)
			return k == bytesFromString || k == runesFromString
		}
		if id, ok := ast.Unparen(e.Fun).(*ast.Ident); ok {
			if b, ok := info.Uses[id].(*types.Builtin); ok {
				switch b.Name() {
				case "len", "cap", "append", "copy", "make", "new":
					return true
				}
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return e.Op == token.LAND || e.Op == token.LOR
	case *ast.SliceExpr:
		return true
	case *ast.TypeAssertExpr:
		if e.Type == nil {
			return false
		}
		if _, ok := tv.Type.(*types.Tuple); ok {
			return false
		}
		t := info.Types[e.Type].Type
		return isIface(t) || !directIface(t)
	case *ast.IndexExpr:
		// A map read whose key converts a byte slice to a string (gc
		// reuses the bytes, so it reads the map where the index appears).
		if !isMap(info.Types[e.X].Type) || a.targets[e] {
			return false
		}
		if _, ok := tv.Type.(*types.Tuple); ok {
			return false
		}
		return a.bytesKey(e.Index)
	case *ast.CompositeLit:
		if _, ok := tv.Type.Underlying().(*types.Map); ok {
			for _, elt := range e.Elts {
				if !a.staticEntry(elt.(*ast.KeyValueExpr)) {
					return true
				}
			}
		}
	}
	return false
}

// bytesKey is gc's mapKeyReplaceStrConv: string(b) of a byte slice, directly
// or inside struct and array literals.
func (a *evalAnalysis) bytesKey(e ast.Expr) bool {
	e = ast.Unparen(e)
	switch e := e.(type) {
	case *ast.CallExpr:
		return a.f.info.Types[e.Fun].IsType() && classifyConversion(a.f.info, e) == stringFromBytes
	case *ast.CompositeLit:
		switch a.f.info.Types[e].Type.Underlying().(type) {
		case *types.Struct, *types.Array:
			found := false
			for _, elt := range e.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					elt = kv.Value
				}
				if a.bytesKey(elt) {
					found = true
				}
			}
			return found
		}
	}
	return false
}

// staticEntry is gc's isStaticCompositeLiteral for a map literal entry,
// after ordering: constant keys and values (and composite literals of
// them), with no conversion to an interface other than of untyped nil.
func (a *evalAnalysis) staticEntry(kv *ast.KeyValueExpr) bool {
	return a.static(kv.Key) && a.static(kv.Value)
}

func (a *evalAnalysis) static(e ast.Expr) bool {
	if conv := a.f.ifaces.byExpr[e]; conv != nil {
		return conv.From == types.Typ[types.UntypedNil]
	}
	e = ast.Unparen(e)
	if conv := a.f.ifaces.byExpr[e]; conv != nil {
		return conv.From == types.Typ[types.UntypedNil]
	}
	tv := a.f.info.Types[e]
	if tv.Value != nil || tv.IsNil() {
		return true
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	switch tv.Type.Underlying().(type) {
	case *types.Struct, *types.Array:
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			if !a.static(elt) {
				return false
			}
		}
		return true
	}
	return false
}

// constantOperand reports an operand with nothing to read: a constant, nil
// or a static composite literal.
func (a *evalAnalysis) constantOperand(e ast.Expr) bool {
	e = ast.Unparen(e)
	tv := a.f.info.Types[e]
	if tv.Value != nil || tv.IsNil() {
		return true
	}
	if lit, ok := e.(*ast.CompositeLit); ok {
		switch tv.Type.Underlying().(type) {
		case *types.Struct, *types.Array:
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					elt = kv.Value
				}
				if a.f.ifaces.byExpr[elt] != nil || !a.constantOperand(elt) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// conversionOperand marks the operand of a conversion to an interface that
// gc copies where it appears: its runtime conversion takes the value by
// address, and the operand is neither addressable nor constant.
func (a *evalAnalysis) conversionOperand(conv *Conversion) {
	if conv.rng || conv.Index >= 0 || conv.From == types.Typ[types.UntypedNil] {
		return
	}
	e := conv.Expr
	if a.out.sequenced[e] || a.f.info.Types[e].Addressable() || a.constantOperand(e) || !needsAddr(conv.From) {
		return
	}
	a.out.sequenced[e] = true
}

// slowKey marks a map key gc copies where it appears: the map takes gc's
// generic (slow) access path, whose key is passed by address, and the key
// is neither addressable nor constant. A key converted to an interface key
// type is copied after the conversion.
func (a *evalAnalysis) slowKey(m *types.Map, key ast.Expr) {
	if mapFast(m) != mapSlow || a.out.shared[key] != nil || a.constantOperand(key) {
		return
	}
	conv := a.f.ifaces.byExpr[key]
	if conv == nil && a.f.info.Types[key].Addressable() && types.Identical(a.f.info.Types[key].Type, m.Key()) {
		return
	}
	a.out.sequenced[key] = true
	if conv != nil {
		a.keyConvOf[key] = true
	}
}

// mapAppend records gc's m[k] = append(m[k], ...) special case: the map
// operand and the key of the target, unless gc keeps them as variables, are
// copied before the other append arguments, and the first argument reuses
// those copies.
func (a *evalAnalysis) mapAppend(s *ast.AssignStmt) {
	info := a.f.info
	if s.Tok != token.ASSIGN || len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}
	lhs, ok := ast.Unparen(s.Lhs[0]).(*ast.IndexExpr)
	if !ok || !isMap(info.Types[lhs.X].Type) {
		return
	}
	call, ok := ast.Unparen(s.Rhs[0]).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return
	}
	if b, ok := info.Uses[id].(*types.Builtin); !ok || b.Name() != "append" {
		return
	}
	arg, ok := ast.Unparen(call.Args[0]).(*ast.IndexExpr)
	if !ok || !a.sameSafe(lhs, arg) {
		return
	}
	a.appendOf[s] = call
	m := mapOf(info.Types[lhs.X].Type)
	if !a.bareVariable(lhs.X) {
		a.early[lhs.X] = true
		a.out.sequenced[lhs.X] = true
		a.out.shared[arg.X] = lhs.X
	}
	if !a.constantOperand(lhs.Index) && (!a.bareVariable(lhs.Index) || mapKeyConverts(m, info.Types[lhs.Index].Type)) {
		a.early[lhs.Index] = true
		a.out.sequenced[lhs.Index] = true
		a.out.shared[arg.Index] = lhs.Index
	}
}

// mapKeyConverts reports that gc converts a key of type t to the fast map
// path's key type (a temporary for a variable key).
func mapKeyConverts(m *types.Map, t types.Type) bool {
	if !types.Identical(t, m.Key()) {
		return true
	}
	switch mapFast(m) {
	case mapFast32:
		return !types.Identical(t, types.Typ[types.Uint32])
	case mapFast64:
		return gcHasPointers(t) || !types.Identical(t, types.Typ[types.Uint64])
	case mapFastStr:
		return !types.Identical(t, types.Typ[types.String])
	}
	return false
}

// bareVariable is a variable operand (gc's ONAME).
func (a *evalAnalysis) bareVariable(e ast.Expr) bool {
	e = ast.Unparen(e)
	if v := a.f.packageVar(e); v != nil {
		return true
	}
	if id, ok := e.(*ast.Ident); ok {
		v, ok := a.f.info.Uses[id].(*types.Var)
		return ok && !v.IsField()
	}
	return false
}

// sameSafe is gc's ir.SameSafeExpr on typed syntax.
func (a *evalAnalysis) sameSafe(l, r ast.Expr) bool {
	info := a.f.info
	l, r = ast.Unparen(l), ast.Unparen(r)
	lt, rt := info.Types[l], info.Types[r]
	if lt.Type == nil || rt.Type == nil || !types.Identical(lt.Type, rt.Type) {
		return false
	}
	if lt.Value != nil || rt.Value != nil {
		return lt.Value != nil && rt.Value != nil && constant.Compare(lt.Value, token.EQL, rt.Value)
	}
	if lt.IsNil() || rt.IsNil() {
		return lt.IsNil() && rt.IsNil()
	}
	switch l := l.(type) {
	case *ast.Ident:
		r, ok := r.(*ast.Ident)
		return ok && info.Uses[l] != nil && info.Uses[l] == info.Uses[r]
	case *ast.SelectorExpr:
		r, ok := r.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if v := a.f.packageVar(l); v != nil {
			return v == a.f.packageVar(r)
		}
		return info.Uses[l.Sel] != nil && info.Uses[l.Sel] == info.Uses[r.Sel] && info.Selections[l] != nil && a.sameSafe(l.X, r.X)
	case *ast.StarExpr:
		r, ok := r.(*ast.StarExpr)
		return ok && a.sameSafe(l.X, r.X)
	case *ast.UnaryExpr:
		r, ok := r.(*ast.UnaryExpr)
		return ok && l.Op == r.Op && l.Op != token.AND && a.sameSafe(l.X, r.X)
	case *ast.IndexExpr:
		r, ok := r.(*ast.IndexExpr)
		return ok && a.sameSafe(l.X, r.X) && a.sameSafe(l.Index, r.Index)
	case *ast.BinaryExpr:
		r, ok := r.(*ast.BinaryExpr)
		if !ok || l.Op != r.Op {
			return false
		}
		switch l.Op {
		case token.ADD, token.SUB, token.OR, token.XOR, token.MUL, token.SHL, token.SHR, token.AND, token.AND_NOT, token.QUO, token.REM:
			return a.sameSafe(l.X, r.X) && a.sameSafe(l.Y, r.Y)
		}
	case *ast.CallExpr:
		r, ok := r.(*ast.CallExpr)
		if !ok || !info.Types[l.Fun].IsType() || !info.Types[r.Fun].IsType() || len(l.Args) != 1 || len(r.Args) != 1 {
			return false
		}
		b, ok := lt.Type.Underlying().(*types.Basic)
		return ok && b.Info()&(types.IsNumeric|types.IsBoolean) != 0 && a.sameSafe(l.Args[0], r.Args[0])
	}
	return false
}

// directIface is gc's types.IsDirectIface: values stored in the interface
// data word itself.
func directIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() == 1 && directIface(u.Elem())
	case *types.Struct:
		return u.NumFields() == 1 && directIface(u.Field(0).Type())
	}
	return false
}

func gcHasPointers(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0 || u.Kind() == types.UnsafePointer
	case *types.Array:
		return u.Len() > 0 && gcHasPointers(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if gcHasPointers(u.Field(i).Type()) {
				return true
			}
		}
		return false
	}
	return true
}

// needsAddr is gc's dataWordFuncName needsaddr for a non-interface type:
// the runtime conversion takes the value by address.
func needsAddr(t types.Type) bool {
	size, align := gcSizes.Sizeof(t), gcSizes.Alignof(t)
	switch {
	case size == 2 && align == 2, size == 4 && align == 4 && !gcHasPointers(t), size == 8 && align == 8 && !gcHasPointers(t):
		return false
	}
	for {
		switch u := t.Underlying().(type) {
		case *types.Struct:
			if u.NumFields() != 1 {
				return true
			}
			t = u.Field(0).Type()
			continue
		case *types.Array:
			if u.Len() != 1 {
				return true
			}
			t = u.Elem()
			continue
		case *types.Slice:
			return false
		case *types.Basic:
			return u.Info()&types.IsString == 0
		}
		return true
	}
}

type mapPath int

const (
	mapSlow mapPath = iota
	mapFast32
	mapFast64
	mapFastStr
)

// mapFast is gc's walk.mapfast: the runtime map-access path for m.
func mapFast(m *types.Map) mapPath {
	if gcSizes.Sizeof(m.Elem()) > 128 {
		return mapSlow
	}
	mem, str := gcAlg(m.Key())
	switch {
	case str:
		return mapFastStr
	case mem && gcSizes.Sizeof(m.Key()) == 4 && !gcHasPointers(m.Key()):
		return mapFast32
	case mem && gcSizes.Sizeof(m.Key()) == 8:
		return mapFast64
	}
	return mapSlow
}

// gcAlg is gc's types.AlgType reduced to what mapfast needs: plain memory
// equality, or string equality.
func gcAlg(t types.Type) (mem, str bool) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsString != 0:
			return false, true
		case u.Info()&(types.IsInteger|types.IsBoolean) != 0, u.Kind() == types.UnsafePointer:
			return true, false
		}
		return false, false
	case *types.Pointer, *types.Chan:
		return true, false
	case *types.Array:
		mem, str := gcAlg(u.Elem())
		switch {
		case u.Len() == 0:
			return true, false
		case u.Len() == 1:
			return mem, str
		}
		return mem, false
	case *types.Struct:
		if u.NumFields() == 1 && u.Field(0).Name() != "_" {
			return gcAlg(u.Field(0).Type())
		}
		fields := make([]*types.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offsets := gcSizes.Offsetsof(fields)
		size := gcSizes.Sizeof(u)
		for i, field := range fields {
			mem, _ := gcAlg(field.Type())
			end := offsets[i] + gcSizes.Sizeof(field.Type())
			next := size
			if i+1 < len(fields) {
				next = offsets[i+1]
			}
			if !mem || field.Name() == "_" || end != next {
				return false, false
			}
		}
		return true, false
	}
	return false, false
}

// body analyzes a statement list: every simple statement and root
// expression is an evaluation owner.
func (a *evalAnalysis) body(list []ast.Stmt) {
	for _, s := range list {
		a.stmt(s)
	}
}

func (a *evalAnalysis) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.AssignStmt:
		o := &evalOwner{key: s}
		for _, l := range s.Lhs {
			if id, ok := l.(*ast.Ident); ok && (s.Tok == token.DEFINE || id.Name == "_") {
				continue
			}
			a.target(l, o)
		}
		for _, r := range s.Rhs {
			a.walk(r, o)
		}
		call := a.appendOf[s]
		if call != nil && a.out.hoisted[call] != nil {
			// The first append argument reuses the target's early copies,
			// which the statement evaluates before the append.
			a.out.hoisted[s] = o.seq
		}
		a.finish(o)
		if call != nil && a.out.hoisted[s] == nil {
			// Left to right, the first argument reads the same values.
			arg := ast.Unparen(call.Args[0]).(*ast.IndexExpr)
			delete(a.out.shared, arg.X)
			delete(a.out.shared, arg.Index)
		}
	case *ast.IncDecStmt:
		o := &evalOwner{key: s}
		a.target(s.X, o)
		a.finish(o)
	case *ast.ReturnStmt:
		o := &evalOwner{key: s}
		for _, r := range s.Results {
			a.walk(r, o)
		}
		a.finish(o)
	case *ast.DeferStmt:
		o := &evalOwner{key: s}
		a.callOperands(s.Call, o)
		a.finish(o)
	case *ast.ExprStmt:
		a.ownerExpr(s.X)
	case *ast.DeclStmt:
		if d, ok := s.Decl.(*ast.GenDecl); ok && d.Tok == token.VAR {
			for _, spec := range d.Specs {
				v := spec.(*ast.ValueSpec)
				o := &evalOwner{key: v}
				for _, value := range v.Values {
					a.walk(value, o)
				}
				a.finish(o)
			}
		}
	case *ast.IfStmt:
		if s.Init != nil {
			a.stmt(s.Init)
		}
		a.ownerExpr(s.Cond)
		a.body(s.Body.List)
		if s.Else != nil {
			a.stmt(s.Else)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			a.stmt(s.Init)
		}
		if s.Cond != nil {
			a.ownerExpr(s.Cond)
		}
		if s.Post != nil {
			a.stmt(s.Post)
		}
		a.body(s.Body.List)
	case *ast.RangeStmt:
		if _, ok := arrayValue(a.f.info.Types[s.X].Type); ok && s.Value == nil {
			// Only the operand's calls run (the array is not loaded).
			a.walk(s.X, &evalOwner{key: s.X})
		} else {
			a.ownerExpr(s.X)
		}
		a.body(s.Body.List)
	case *ast.SwitchStmt:
		if s.Init != nil {
			a.stmt(s.Init)
		}
		if s.Tag != nil {
			a.ownerExpr(s.Tag)
		}
		for _, c := range s.Body.List {
			clause := c.(*ast.CaseClause)
			for _, e := range clause.List {
				a.ownerExpr(e)
			}
			a.body(clause.Body)
		}
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			a.stmt(s.Init)
		}
		var guard ast.Expr
		switch g := s.Assign.(type) {
		case *ast.ExprStmt:
			guard = g.X
		case *ast.AssignStmt:
			guard = g.Rhs[0]
		}
		if ta, ok := ast.Unparen(guard).(*ast.TypeAssertExpr); ok {
			a.ownerExpr(ta.X)
		}
		for _, c := range s.Body.List {
			a.body(c.(*ast.CaseClause).Body)
		}
	case *ast.BlockStmt:
		a.body(s.List)
	case *ast.LabeledStmt:
		a.stmt(s.Stmt)
	}
}

// ownerExpr analyzes e as an evaluation owner: a root expression or a
// sequenced expression, whose own sequenced operands are evaluated first.
func (a *evalAnalysis) ownerExpr(e ast.Expr) {
	if e == nil {
		return
	}
	o := &evalOwner{key: e}
	switch x := e.(type) {
	case *ast.ParenExpr:
		a.ownerExpr(x.X)
		return
	case *ast.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			a.ownerExpr(x.X)
			a.ownerExpr(x.Y)
			return
		}
	case *ast.CompositeLit:
		if _, ok := a.f.info.Types[x].Type.Underlying().(*types.Map); ok && a.sequencedKind(x) {
			a.mapLiteral(x)
			return
		}
	}
	a.plain(e, o)
	a.finish(o)
}

// mapLiteral analyzes each dynamic entry as an owner and records gc's
// insertion order: static entries first, at creation, then dynamic ones.
func (a *evalAnalysis) mapLiteral(lit *ast.CompositeLit) {
	var statics, dynamics []int
	for i, elt := range lit.Elts {
		kv := elt.(*ast.KeyValueExpr)
		if a.staticEntry(kv) {
			statics = append(statics, i)
			continue
		}
		dynamics = append(dynamics, i)
		o := &evalOwner{key: kv}
		a.walk(kv.Key, o)
		a.walk(kv.Value, o)
		a.finish(o)
	}
	if len(statics) > 0 && len(dynamics) > 0 && statics[len(statics)-1] > dynamics[0] {
		a.out.mapOrder[lit] = append(statics, dynamics...)
	}
}

// walk visits an operand read as a value, in evaluation order.
func (a *evalAnalysis) walk(e ast.Expr, o *evalOwner) {
	if e == nil {
		return
	}
	if a.out.shared[e] != nil {
		return // the target's early copy
	}
	if a.out.sequenced[e] {
		o.seq = append(o.seq, e)
		a.ownerExpr(e)
		if conv := a.f.ifaces.byExpr[e]; conv != nil && !a.keyConvOf[e] && boxes(conv) {
			o.events = append(o.events, evalEvent{end: e.End(), trap: true})
		}
		return
	}
	a.plain(e, o)
}

// boxes reports a conversion that allocates (and so may run out of memory).
func boxes(conv *Conversion) bool {
	return conv.From != types.Typ[types.UntypedNil] && !directIface(conv.From) && gcSizes.Sizeof(conv.From) != 0
}

func (a *evalAnalysis) event(o *evalOwner, n ast.Node, writable, trap bool) {
	if writable || trap {
		o.events = append(o.events, evalEvent{end: n.End(), writable: writable, trap: trap})
	}
}

// writableVar reports a variable a call may write: a package-level
// variable, or a local whose address is taken or that a closure captures.
func (a *evalAnalysis) writableVar(o types.Object) bool {
	v, ok := o.(*types.Var)
	if !ok || v.IsField() {
		return false
	}
	return isPackageVar(v) || a.pinned[v]
}

// plain visits e's own operation (not sequenced): its operands, then the
// read or trap it performs.
func (a *evalAnalysis) plain(e ast.Expr, o *evalOwner) {
	info := a.f.info
	tv := info.Types[e]
	defer func() {
		if conv := a.f.ifaces.byExpr[e]; conv != nil && !conv.rng && boxes(conv) {
			a.event(o, e, false, true)
		}
	}()
	if tv.Value != nil || tv.IsNil() {
		return
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		a.walk(e.X, o)
	case *ast.Ident:
		if v, ok := info.Uses[e].(*types.Var); ok {
			a.event(o, e, a.writableVar(v), false)
		}
	case *ast.FuncLit:
		a.lit(e)
		if cl := a.f.funcs.byLit[e]; cl != nil && len(cl.Captures) > 0 {
			a.event(o, e, false, true)
		}
	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if _, isMap := tv.Type.Underlying().(*types.Map); isMap {
					a.walk(kv.Key, o)
				}
				elt = kv.Value
			}
			a.walk(elt, o)
		}
		switch tv.Type.Underlying().(type) {
		case *types.Slice, *types.Map, *types.Pointer:
			a.event(o, e, false, true) // allocates
		}
	case *ast.SelectorExpr:
		if v := a.f.packageVar(e); v != nil {
			a.event(o, e, true, false)
			return
		}
		sel := info.Selections[e]
		if sel == nil {
			return // pkg.F
		}
		switch sel.Kind() {
		case types.FieldVal:
			_, isPointer := pointer(info.Types[e.X].Type)
			a.walk(e.X, o)
			if isPointer || sel.Indirect() {
				a.event(o, e, true, true)
			}
		case types.MethodVal:
			a.receiver(e, sel, o)
			a.event(o, e, false, true) // a method value allocates
		}
	case *ast.StarExpr:
		a.walk(e.X, o)
		a.event(o, e, true, true)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			if lit, ok := ast.Unparen(e.X).(*ast.CompositeLit); ok {
				a.plain(lit, o)
				a.event(o, e, false, true)
				return
			}
			a.target(e.X, o)
			return
		}
		a.walk(e.X, o)
	case *ast.BinaryExpr:
		a.walk(e.X, o)
		a.walk(e.Y, o)
		a.event(o, e, false, a.binaryTraps(e))
	case *ast.IndexExpr:
		a.index(e, o, false)
	case *ast.SliceExpr:
		if _, ok := info.Types[e.X].Type.Underlying().(*types.Array); ok {
			a.target(e.X, o) // slicing an array takes its address
		} else {
			a.walk(e.X, o)
		}
		a.walk(e.Low, o)
		a.walk(e.High, o)
		a.walk(e.Max, o)
		a.event(o, e, false, true)
	case *ast.TypeAssertExpr:
		a.walk(e.X, o)
		a.event(o, e, false, true)
	case *ast.CallExpr:
		a.callOperands(e, o)
		if arr, ptr, ok := sliceToArray(info, e); ok {
			// A plain operation, as in gc: a short slice traps after the
			// owner's calls, and [N]T(s) reads the elements there.
			a.event(o, e, !ptr, arr.Len() > 0)
			return
		}
		if info.Types[e.Fun].IsType() {
			switch classifyConversion(info, e) {
			case stringFromBytes, stringFromRunes:
				a.event(o, e, true, true)
			case notStringConversion, stringFromString:
			default:
				a.event(o, e, false, true)
			}
		}
	case *ast.KeyValueExpr:
		a.walk(e.Key, o)
		a.walk(e.Value, o)
	}
}

// binaryTraps reports a binary operation that may abort: division or
// modulo by a non-constant, a signed shift count, a comparison of values
// holding interfaces, or a string concatenation (which allocates).
func (a *evalAnalysis) binaryTraps(e *ast.BinaryExpr) bool {
	info := a.f.info
	switch e.Op {
	case token.QUO, token.REM:
		return info.Types[e.Y].Value == nil || constant.Sign(info.Types[e.Y].Value) == 0
	case token.SHL, token.SHR:
		return info.Types[e.Y].Value == nil && isSigned(info.Types[e.Y].Type)
	case token.EQL, token.NEQ:
		if info.Types[e.X].IsNil() || info.Types[e.Y].IsNil() {
			return false
		}
		return holdsIface(info.Types[e.X].Type) || holdsIface(info.Types[e.Y].Type)
	case token.ADD:
		return isString(info.Types[e].Type)
	}
	return false
}

func holdsIface(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return true
	case *types.Array:
		return holdsIface(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if holdsIface(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// index visits x[i] as a value read or (target) as assignment storage.
func (a *evalAnalysis) index(e *ast.IndexExpr, o *evalOwner, target bool) {
	info := a.f.info
	t := info.Types[e.X].Type
	if _, ok := info.Types[e.X].Type.(*types.Signature); ok {
		return // F[T] as a value
	}
	if isMap(t) {
		// Reads never fail on their own; hashing an interface key can, and
		// so can storing into a nil map.
		a.walk(e.X, o)
		a.walk(e.Index, o)
		a.event(o, e, !target, target || holdsIface(mapOf(t).Key()))
		return
	}
	trap := info.Types[e.Index].Value == nil
	if _, isArray := t.Underlying().(*types.Array); isArray {
		if target {
			a.target(e.X, o)
		} else {
			a.walk(e.X, o)
		}
		a.walk(e.Index, o)
		a.event(o, e, false, trap)
		return
	}
	a.walk(e.X, o)
	a.walk(e.Index, o)
	a.event(o, e, !target && !isString(t), true)
}

// target visits assignment (or address-of) storage: its operands are read,
// the storage itself is not.
func (a *evalAnalysis) target(e ast.Expr, o *evalOwner) {
	info := a.f.info
	switch x := e.(type) {
	case *ast.ParenExpr:
		a.target(x.X, o)
	case *ast.Ident:
	case *ast.SelectorExpr:
		if a.f.packageVar(x) != nil {
			return
		}
		sel := info.Selections[x]
		if sel == nil {
			return
		}
		if _, isPointer := pointer(info.Types[x.X].Type); isPointer || sel.Indirect() {
			a.walk(x.X, o)
			a.event(o, x, false, true)
			return
		}
		a.target(x.X, o)
	case *ast.StarExpr:
		a.walk(x.X, o)
		a.event(o, x, false, true)
	case *ast.IndexExpr:
		a.index(x, o, true)
	case *ast.CompositeLit:
		a.plain(x, o)
	default:
		a.walk(e, o)
	}
}

// receiver visits a method selector's receiver operand.
func (a *evalAnalysis) receiver(sel *ast.SelectorExpr, selection *types.Selection, o *evalOwner) {
	info := a.f.info
	if isIface(info.Types[sel.X].Type) {
		a.walk(sel.X, o)
		return
	}
	fn, ok := a.f.callee(sel.Sel).(*types.Func)
	if !ok {
		a.walk(sel.X, o)
		return
	}
	_, wantPointer := pointer(fn.Type().(*types.Signature).Recv().Type())
	_, isPointer := pointer(info.Types[sel.X].Type)
	// A promoted method reached through an embedded pointer field loads
	// that pointer.
	embedded := len(selection.Index()) > 1 && selection.Indirect()
	switch {
	case wantPointer && !isPointer:
		a.target(sel.X, o)
	case !wantPointer && (isPointer || embedded):
		a.walk(sel.X, o)
		a.event(o, sel.X, true, true) // the receiver is copied from memory
	default:
		a.walk(sel.X, o)
		if embedded {
			a.event(o, sel.X, true, true)
		}
	}
	if wantPointer && !isPointer && embedded {
		a.event(o, sel.X, true, true)
	}
}

// callOperands visits a call's function value or receiver and arguments
// (not the call itself).
func (a *evalAnalysis) callOperands(c *ast.CallExpr, o *evalOwner) {
	info := a.f.info
	if info.Types[c.Fun].IsType() {
		for _, arg := range c.Args {
			a.walk(arg, o)
		}
		return
	}
	switch fun := ast.Unparen(a.f.callFun(c.Fun)).(type) {
	case *ast.FuncLit:
		a.lit(fun)
	case *ast.Ident:
		switch info.Uses[fun].(type) {
		case *types.Builtin:
			args := c.Args
			if fun.Name == "make" || fun.Name == "new" {
				args = args[1:]
			}
			for _, arg := range args {
				a.walk(arg, o)
			}
			return
		case *types.Var:
			a.walk(c.Fun, o)
		}
	case *ast.SelectorExpr:
		if a.f.packageVar(fun) != nil {
			a.walk(fun, o)
		} else if sel := info.Selections[fun]; sel != nil {
			switch sel.Kind() {
			case types.MethodVal:
				a.receiver(fun, sel, o)
			case types.FieldVal:
				a.walk(fun, o)
			}
		}
	default:
		a.walk(c.Fun, o)
	}
	for _, arg := range c.Args {
		a.walk(arg, o)
	}
}

// lit analyzes a function literal's body once.
func (a *evalAnalysis) lit(lit *ast.FuncLit) {
	if a.litsDone[lit] {
		return
	}
	a.litsDone[lit] = true
	a.body(lit.Body.List)
}

// finish records o when gc's order is observably different from left to
// right: a plain operation precedes (in source order) a sequenced operand
// that may write what it reads, or log before its trap.
func (a *evalAnalysis) finish(o *evalOwner) {
	if a.out.hoisted[o.key] != nil {
		a.markConverted(o)
		return
	}
	for _, s := range o.seq {
		effect := a.effect(s)
		if !effect.writes && !effect.logs {
			continue
		}
		for _, ev := range o.events {
			if ev.end <= s.Pos() && (ev.writable && effect.writes || ev.trap && effect.logs) {
				a.out.hoisted[o.key] = o.seq
				a.markConverted(o)
				return
			}
		}
	}
}

func (a *evalAnalysis) markConverted(o *evalOwner) {
	for _, s := range o.seq {
		if a.keyConvOf[s] {
			a.out.converted[s] = true
		}
	}
}

// effect summarizes what evaluating a sequenced expression may do: write
// storage another operand reads, or log (so an earlier trap is observable).
func (a *evalAnalysis) effect(e ast.Expr) evalEffect {
	if eff, ok := a.effects[e]; ok {
		return eff
	}
	var eff evalEffect
	ast.Inspect(e, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok {
			w, l := a.callEffect(c)
			eff.writes = eff.writes || w
			eff.logs = eff.logs || l
		}
		return true
	})
	a.effects[e] = eff
	return eff
}

// callEffect classifies one call: whether it may write storage and whether
// it may log.
func (a *evalAnalysis) callEffect(c *ast.CallExpr) (writes, logs bool) {
	info := a.f.info
	if info.Types[c.Fun].IsType() {
		return false, false
	}
	var object types.Object
	switch fun := ast.Unparen(a.f.callFun(c.Fun)).(type) {
	case *ast.Ident:
		object = a.f.callee(fun)
	case *ast.SelectorExpr:
		if a.f.ifaces.calls[c] != nil {
			return true, true
		}
		object = a.f.callee(fun.Sel)
	default:
		return true, true
	}
	switch o := object.(type) {
	case *types.Builtin:
		switch o.Name() {
		case "append", "copy":
			return true, false
		case "panic":
			return false, true
		}
		return false, false
	case *types.Func:
		if o.Pkg() != nil {
			switch o.Pkg().Path() {
			case "gosvm/solana":
				return !sdkPure[o.Name()], sdkLogs[o.Name()]
			case bitsPath:
				return false, false
			}
		}
		return a.mayWrite(o), a.mayLog(o)
	}
	return true, true
}

// mayLog reports a function that may log before returning or aborting: it
// calls panic, a logging SDK function, an unknown function value or
// interface method, or such a function.
func (a *evalAnalysis) mayLog(o types.Object) bool {
	switch a.logs[o] {
	case 1:
		return false
	case 2:
		return true
	}
	fn := a.byObject[o]
	if fn == nil || fn.Library != nil || fn.Decl.Body == nil {
		a.logs[o] = 2
		return true
	}
	a.logs[o] = 2 // recursion is rejected; be conservative while visiting
	logs := false
	ast.Inspect(fn.Decl.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && !logs {
			if _, l := a.callEffect(c); l {
				logs = true
			}
		}
		return !logs
	})
	if !logs {
		a.logs[o] = 1
	}
	return logs
}

// mayWrite reports a function that may write storage its caller can read:
// a package variable, anything through a pointer, slice or map (including
// append, copy, delete and clear), or anything an SDK writer, unknown
// function value, interface method or such a function writes. Assignments
// to its own locals (fresh in every call) are not writes.
func (a *evalAnalysis) mayWrite(o types.Object) bool {
	switch a.writes[o] {
	case 1:
		return false
	case 2:
		return true
	}
	fn := a.byObject[o]
	a.writes[o] = 2
	if fn == nil || fn.Library != nil || fn.Decl.Body == nil {
		return true
	}
	if a.bodyMayWrite(fn.Decl.Body, nil) {
		return true
	}
	a.writes[o] = 1
	return false
}

// bodyMayWrite reports whether body may write storage outside its own
// locals, as mayWrite; assignments to the variables in shared (a literal's
// captured variables) count too.
func (a *evalAnalysis) bodyMayWrite(body ast.Node, shared map[types.Object]bool) bool {
	return len(a.writeSites(body, shared, false, true)) > 0
}

// writeSites lists the statements and calls in body that may write storage
// outside its own locals (or the variables in shared): assignments,
// increments and range assignments to such storage (the statement), calls
// of delete and clear, and calls that may write (the call). With skipLits,
// function literals' bodies are not searched (their writes happen where
// they are called, which is a call that may write). With first, it stops
// at the first site.
func (a *evalAnalysis) writeSites(body ast.Node, shared map[types.Object]bool, skipLits, first bool) []ast.Node {
	info := a.f.info
	var sites []ast.Node
	writes := func(e ast.Expr) bool {
		for {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
				continue
			case *ast.Ident:
				v, ok := info.Uses[x].(*types.Var)
				return ok && (isPackageVar(v) || shared[v])
			case *ast.SelectorExpr:
				if a.f.packageVar(x) != nil {
					return true
				} else if sel := info.Selections[x]; sel == nil || sel.Indirect() {
					return true
				} else if _, isPointer := pointer(info.Types[x.X].Type); isPointer {
					return true
				}
				e = x.X
				continue
			case *ast.IndexExpr:
				if _, ok := info.Types[x.X].Type.Underlying().(*types.Array); ok {
					e = x.X
					continue
				}
				return true
			}
			return true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if first && len(sites) > 0 {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return !skipLits
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && (n.Tok == token.DEFINE || id.Name == "_") {
					continue
				}
				if writes(l) {
					sites = append(sites, n)
					break
				}
			}
		case *ast.IncDecStmt:
			if writes(n.X) {
				sites = append(sites, n)
			}
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN && (n.Key != nil && writes(n.Key) || n.Value != nil && writes(n.Value)) {
				sites = append(sites, n)
			}
		case *ast.CallExpr:
			w := false
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
				if b, ok := info.Uses[id].(*types.Builtin); ok && (b.Name() == "delete" || b.Name() == "clear") {
					w = true
				}
			}
			if !w {
				w, _ = a.callEffect(n)
			}
			if w {
				sites = append(sites, n)
			}
		}
		return true
	})
	return sites
}

// hoisting is the emitter's state for CheckedProgram.Hoisted: the saved
// values of hoisted expressions while their owner is emitted.
type hoisting struct {
	order *evalOrder
	value map[ast.Expr]string // hoisted values, unconverted
	boxed map[ast.Expr]string // hoisted values with their conversion
	used  map[ast.Expr]bool
}

// hoist evaluates the sequenced operands Check recorded for owner n, in
// order, and returns the function that releases them when n is done. Each
// saved value must be used by n's evaluation where the operand appears.
func (g *generator) hoist(n ast.Node) func() {
	list := g.order.order.hoisted[n]
	if list == nil {
		return func() {}
	}
	for _, e := range list {
		if g.order.order.converted[e] {
			g.order.boxed[e] = g.expr(e)
		} else {
			g.order.value[e] = g.exprValue(e)
		}
	}
	return func() {
		for _, e := range list {
			if !g.order.used[e] {
				g.unchecked(e)
			}
			delete(g.order.value, e)
			delete(g.order.boxed, e)
			delete(g.order.used, e)
		}
	}
}

// hoisted returns the saved value of e (or of the target operand e shares)
// while its owner is emitted.
func (g *generator) hoisted(e ast.Expr, boxed bool) (string, bool) {
	values := g.order.value
	if boxed {
		values = g.order.boxed
	}
	if src := g.order.order.shared[e]; src != nil {
		v, ok := values[src]
		return v, ok
	}
	v, ok := values[e]
	if ok {
		g.order.used[e] = true
	}
	return v, ok
}
