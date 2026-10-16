package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

// Compile-time evaluation of package initialization
// (docs/GO_GLOBALS.md#compile-time-evaluation).
//
// Package initialization is a sequence of steps: for each package in Inits
// order, each statement of its synthetic variable initializer (Vars), then
// each of its init functions. Check interprets the longest prefix of that
// sequence whose every step completes deterministically without the
// runtime: no trap or panic, no SDK or native-library call, no
// unsupported construct, within the step budget. The state after the
// prefix (the writable package-level variables and every heap object they
// reach, laid out exactly where the runtime would have allocated them, and
// the allocator's in-use count) becomes InitImage. At run time a backend
// installs the image and runs only the remaining steps, so the observable
// state, heap accounting included, is the one running every initializer
// produces; only CU differs. A step that would trap stays a runtime step, so
// it traps on every invocation as before.

// InitEvalStepLimit bounds the interpreter: every statement executed, call
// made and loop iteration counts one step. Initialization that needs more
// stops the prefix where the budget runs out (contracted; see
// docs/GO_GLOBALS.md#compile-time-evaluation).
const InitEvalStepLimit = 1 << 20

// InitImage is the compile-time evaluated prefix of package initialization
// and the state it leaves. A backend reproduces it by making the heap's
// first HeapUsed bytes hold Objects and the writable globals' Values (the
// in-use word at the heap start set to HeapUsed), then running the steps
// after the prefix: for each Inits entry, Vars' statements from
// EvaluatedVars on and the init functions from EvaluatedFuncs on.
type InitImage struct {
	// Steps is the number of leading initialization steps evaluated, of
	// Total (InitSteps).
	Steps, Total int
	// HeapUsed is the allocator's in-use word after the prefix: the offset
	// from the heap start of the next allocation (0 when nothing was
	// allocated). Out-of-memory boundaries depend on it exactly as if the
	// prefix had run.
	HeapUsed int64
	// Objects are the heap allocations the writable globals reach after the
	// prefix (directly or through other objects), by ascending Offset.
	// Allocations nothing reaches any more are not listed: their bytes are
	// counted in HeapUsed but are never read again.
	Objects []*InitObject
	// Values holds, for each CheckedProgram.Globals entry, the value of a
	// writable variable after the prefix (nil for read-only variables).
	Values []*InitValue
	// Stop is why evaluation stopped before the last step, or nil.
	Stop *InitStop
	// Work is the number of interpreter steps the prefix took.
	Work int
}

// InitStop records the first initialization step not evaluated at compile
// time.
type InitStop struct {
	// Step is its index in InitSteps; Pos is the construct that stopped it.
	Step   int
	Pos    token.Pos
	Reason string
}

// InitStep is one step of package initialization.
type InitStep struct {
	Init *PackageInit
	// Stmt is a statement of Init.Vars' body, or nil for an init function.
	Stmt ast.Stmt
	// Func is an init function (Stmt is nil).
	Func *Function
}

// InitSteps lists the steps of package initialization in order.
func (p *CheckedProgram) InitSteps() []InitStep {
	var out []InitStep
	for _, pi := range p.Inits {
		if pi.Vars != nil {
			for _, s := range pi.Vars.Decl.Body.List {
				out = append(out, InitStep{Init: pi, Stmt: s})
			}
		}
		for _, fn := range pi.Funcs {
			out = append(out, InitStep{Init: pi, Func: fn})
		}
	}
	return out
}

// InitObjectKind classifies a heap object of the image.
type InitObjectKind int

const (
	// InitObjectValue holds one value of Type: new(T), a boxed interface
	// value, a local moved to the heap or an escaping &T{...}.
	InitObjectValue InitObjectKind = iota + 1
	// InitObjectArray holds the backing array of slices: Value is an
	// InitArray of Type elements (make, slice literals, append growth,
	// []byte(s), []rune(s)).
	InitObjectArray
	// InitObjectBytes holds string bytes (concatenations and conversions).
	InitObjectBytes
	// InitObjectMap is a map header; Map describes the map.
	InitObjectMap
	// InitObjectMapSlots is a map's live slot array (Map is its map); its
	// layout is the backend's (the C backend's hash table).
	InitObjectMapSlots
	// InitObjectFunc is a closure environment or a method value; Func
	// describes it.
	InitObjectFunc
)

// InitObject is one heap allocation of the image.
type InitObject struct {
	// Offset is its address relative to the heap start (the globals block,
	// when the program has one, is not an InitObject: it is at 8). Size is
	// the bytes the allocation consumed, rounded up to 8 (0 for a zero-byte
	// allocation, which shares its address with the next one).
	Offset, Size int64
	Kind         InitObjectKind
	// Type is the value's type (InitObjectValue) or the element type
	// (InitObjectArray).
	Type  types.Type
	Value *InitValue
	Bytes []byte
	Map   *InitMap
	Func  *InitFunc
}

// InitMap is a map built during the prefix.
type InitMap struct {
	Type *MapType
	// Cap is the slot count of the live slot array (0: none yet), Slots
	// that array.
	Cap   int64
	Slots *InitObject
	// Entries are the live entries in insertion order.
	Entries []InitMapEntry
}

// InitMapEntry is one key and element.
type InitMapEntry struct{ Key, Elem *InitValue }

// InitFunc is a closure environment (Target a literal; Captures the
// addresses of its captured variables, in Closure.Captures order) or a
// method value (Target a method value; Receiver the bound pointer or
// receiver value).
type InitFunc struct {
	Target   *FuncTarget
	Captures []*InitRef
	Receiver *InitValue
}

// InitValueKind classifies an InitValue.
type InitValueKind int

const (
	// InitInt is an integer: Bits holds its two's-complement bits,
	// zero-extended from its width.
	InitInt InitValueKind = iota + 1
	// InitBool: Bits is 0 or 1.
	InitBool
	// InitString: Bytes is its content; Ref is nil when the bytes may be
	// read-only data (a literal), or the heap bytes object with Low the
	// byte offset.
	InitString
	// InitPointer: Ref is the storage pointed to (nil: a nil pointer).
	InitPointer
	// InitSlice: Ref is the backing array storage (nil: a nil slice), Low
	// the first element's index in it, Len and Cap.
	InitSlice
	// InitInterface: Dynamic is the dynamic type (nil: a nil interface);
	// Elem the pointer or map value for a Direct type; Ref the box object
	// otherwise (nil for a zero-size value).
	InitInterface
	// InitFuncValue: Target (nil: a nil function value) and Ref, the
	// InitObjectFunc environment of a closure with captures or a method
	// value (nil otherwise).
	InitFuncValue
	// InitMapValue: Ref is the map header (nil: a nil map).
	InitMapValue
	// InitStruct: Elems are the fields in declaration order.
	InitStruct
	// InitArray: Elems are the elements.
	InitArray
)

// InitValue is a value of the image.
type InitValue struct {
	Kind     InitValueKind
	Type     types.Type
	Bits     uint64
	Bytes    []byte
	Elems    []*InitValue
	Ref      *InitRef
	Low      int64
	Len, Cap int64
	Dynamic  *DynamicType
	Elem     *InitValue
	Target   *FuncTarget
}

// InitRef is storage a pointer, slice, string, interface box, map or
// function value refers to: a heap object (Object), a package-level
// variable (Global: a writable one in the globals block, or a read-only one
// in read-only data), or the heap start itself (HeapBase: the data word of
// a zero-size interface value and the pointer of []byte("")). Path selects
// a field (struct) or element (array) at each level below the referenced
// value; for an InitObjectBytes object, Low on the referring value is the
// byte offset instead.
type InitRef struct {
	Object   *InitObject
	Global   *Global
	HeapBase bool
	Path     []int
}

// evaluateInit runs the interpreter over the program's initialization
// steps and returns the image of the longest evaluable prefix, or nil when
// the program has no initialization steps or none is evaluable.
func evaluateInit(p *CheckedProgram) *InitImage {
	if p.Program != nil && p.Program.RuntimeInit {
		return nil
	}
	steps := p.InitSteps()
	if len(steps) == 0 {
		return nil
	}
	// First pass: find the prefix. A step that stops (or leaves a state the
	// image cannot hold) ends it; its partial effects are discarded by
	// evaluating the prefix again from the start.
	ev := newEvaluator(p)
	done, stop := ev.run(steps, len(steps))
	if stop != nil && done > 0 {
		ev = newEvaluator(p)
		if again, again2 := ev.run(steps, done); again != done || again2 != nil {
			// Deterministic by construction; anything else is a bug, and
			// running every initializer at run time is always correct.
			return nil
		}
	}
	if done == 0 {
		// Nothing to install: the runtime allocates the block and runs
		// every step, exactly as without compile-time evaluation.
		return nil
	}
	image, err := ev.export()
	if err != nil {
		return nil
	}
	image.Steps, image.Total, image.Stop = done, len(steps), stop
	return image
}

// run evaluates up to limit steps in order. It returns how many completed
// and, when one stopped, why.
func (ev *evaluator) run(steps []InitStep, limit int) (done int, stop *InitStop) {
	ev.allocBlock()
	var vars *evFrame
	var current *PackageInit
	for i := 0; i < limit; i++ {
		s := steps[i]
		reason, pos := ev.step(func() {
			if s.Stmt != nil {
				if current != s.Init {
					current = s.Init
					vars = ev.newFrame(s.Init.Vars, nil)
				}
				ev.frame = vars
				ev.exec(s.Stmt)
				ev.frame = nil
				return
			}
			ev.call(s.Func, nil, nil)
		})
		if reason == "" {
			if _, err := ev.export(); err != nil {
				reason, pos = err.Error(), s.pos()
			}
		}
		if reason != "" {
			return i, &InitStop{Step: i, Pos: pos, Reason: reason}
		}
	}
	return limit, nil
}

func (s InitStep) pos() token.Pos {
	if s.Stmt != nil {
		return s.Stmt.Pos()
	}
	return s.Func.Decl.Pos()
}

// step runs f and converts a stop (or an interpreter failure) into its
// reason.
func (ev *evaluator) step(f func()) (reason string, pos token.Pos) {
	defer func() {
		if r := recover(); r != nil {
			ev.frame = nil
			if s, ok := r.(*evStop); ok {
				reason, pos = s.reason, s.pos
				return
			}
			reason = fmt.Sprintf("internal error: %v", r)
		}
	}()
	f()
	return "", token.NoPos
}

// sortedObjects orders image objects by offset (zero-size objects before
// a non-empty object at the same offset).
func sortedObjects(objects []*InitObject) {
	sort.SliceStable(objects, func(i, j int) bool {
		if objects[i].Offset != objects[j].Offset {
			return objects[i].Offset < objects[j].Offset
		}
		return objects[i].Size < objects[j].Size
	})
}

// markEvaluated records on each PackageInit how many of its steps the
// image covers.
func (p *CheckedProgram) markEvaluated() {
	for i, s := range p.InitSteps() {
		if i >= p.InitImage.Steps {
			break
		}
		if s.Stmt != nil {
			s.Init.EvaluatedVars++
		} else {
			s.Init.EvaluatedFuncs++
		}
	}
}
