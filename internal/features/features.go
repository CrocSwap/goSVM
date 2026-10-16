// Package features reports which Go language constructs and which SDK calls a
// type-checked program uses. The report sizes downstream IRs (for example a
// verified backend); it does not certify that the program is in the supported
// on-chain subset, which remains the compiler's job.
package features

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gosvm/internal/compiler"
)

// Schema identifies the JSON layout. Change it when fields change meaning.
const Schema = "gosvm-features-v1"

// File kinds.
const (
	Handwritten = "handwritten"
	Generated   = "generated"
	LibraryStub = "library_stub"
)

// Usage is the per-program report.
type Usage struct {
	Schema   string     `json:"schema"`
	Program  Program    `json:"program"`
	Files    []File     `json:"files"`
	Lines    Split      `json:"lines"`
	Language Language   `json:"language"`
	SDKCalls []CallSite `json:"sdk_calls"`
	// LibraryCalls are calls into generated pure-library bindings (Bend/Rust).
	LibraryCalls []CallSite `json:"library_calls"`
	// StdlibCalls are calls into standard-library packages the compiler
	// provides on-chain (math/bits and the overlay packages of
	// docs/GO_STDLIB.md). Added within gosvm-features-v1.
	StdlibCalls []CallSite `json:"stdlib_calls"`
	// Generics lists every generic declaration in program code with the
	// distinct instantiations the program needs, as the compiler would
	// monomorphize them. Added within gosvm-features-v1.
	Generics []GenericUse `json:"generics"`
	// Uses summarizes which language features beyond the original subset the
	// program uses, so a backend that supports only a subset can name why a
	// program is outside it. Added within gosvm-features-v1.
	Uses Uses `json:"uses"`
}

// Uses is the per-program feature summary. Each flag is derived from the
// counters in Language, StdlibCalls and Generics over handwritten and
// generated files; HeapMovedLocals and HeapMovedLiterals come from the
// compiler's Check and are filled by ApplyCheck.
type Uses struct {
	// Allocation: new/make/append/copy, slice literals, or locals the compiler
	// moves to the heap because their address escapes.
	Allocation bool `json:"allocation"`
	// Interfaces: any value of interface type (not a type-parameter constraint).
	Interfaces bool `json:"interfaces"`
	// SignedIntegers: a signed type written in source, a conversion to one, or
	// a non-constant arithmetic/shift/bitwise operation with a signed result.
	// int values used only as len/cap/count results (compared, converted,
	// indexed) do not count.
	SignedIntegers bool `json:"signed_integers"`
	MathBits       bool `json:"math_bits"`
	Generics       bool `json:"generics"`
	Uint16         bool `json:"uint16"`
	Range          bool `json:"range"`
	// Strings: any non-constant value of a string type (string constants
	// folded into constant expressions do not count). Added within
	// gosvm-features-v1.
	Strings bool `json:"strings"`
	// Closures: function literals, function values, method values or
	// expressions, or calls through function values (added within
	// gosvm-features-v1; docs/GO_FUNCTIONS.md).
	Closures bool `json:"closures"`
	// Defer: defer statements (added within gosvm-features-v1).
	Defer bool `json:"defer"`
	// Maps: any non-nil value of a map type, a map literal or make of a
	// map. Added within gosvm-features-v1.
	Maps bool `json:"maps"`
	// Globals: package-level variables or init functions (added within
	// gosvm-features-v1; docs/GO_GLOBALS.md). Every invocation initializes
	// them before Process; writable ones live in the globals block, the
	// invocation heap's first allocation (GlobalHeapBytes, from Check).
	Globals bool `json:"globals"`
	// StructEquality: == or != on struct operands, or a switch whose tag is
	// a struct (docs/GO_TYPES.md#struct-equality). Comparisons of interface
	// values holding structs are interfaces. TypeAliases: type alias
	// declarations (type A = T). CompositeArrays: array types written with
	// struct, array, pointer or slice elements (docs/GO_ARRAYS.md). Panic:
	// calls of the panic builtin (docs/ERROR_CONTRACT.md#panic). Added
	// within gosvm-features-v1.
	StructEquality  bool `json:"struct_equality"`
	TypeAliases     bool `json:"type_aliases"`
	CompositeArrays bool `json:"composite_arrays"`
	Panic           bool `json:"panic"`
	// PointerConversions: conversions (*T)(p) between pointer types
	// (docs/GO_POINTERS.md#conversions). Added within gosvm-features-v1.
	PointerConversions bool `json:"pointer_conversions"`
	// Stdlib lists, sorted, the standard-library packages the program
	// reaches besides math/bits (MathBits): the overlay packages of
	// docs/GO_STDLIB.md that program code imports, and the overlay packages
	// those import. Their sources are compiled with the program but are not
	// program code: they are not counted in Language, Files or the flags
	// above (added within gosvm-features-v1).
	Stdlib []string `json:"stdlib,omitempty"`
	// Checked reports whether compiler.Check accepted the program; CheckError
	// is its diagnostic otherwise. Both are set by ApplyCheck.
	Checked    bool   `json:"checked"`
	CheckError string `json:"check_error,omitempty"`
	// HeapMovedLocals and HeapMovedLiterals count escaping locals and &T{}
	// literals Check moves to the heap (across all functions and instances).
	HeapMovedLocals   int `json:"heap_moved_locals"`
	HeapMovedLiterals int `json:"heap_moved_literals"`
	// GlobalHeapBytes is the size of the globals block Check lays out for
	// writable package-level variables (0 when none), and ReadOnlyGlobals
	// the number of package-level variables Check placed in read-only data
	// (added within gosvm-features-v1; docs/GO_GLOBALS.md).
	GlobalHeapBytes int64 `json:"global_heap_bytes"`
	ReadOnlyGlobals int   `json:"readonly_globals"`
	// Init describes package initialization after compile-time evaluation
	// (added within gosvm-features-v1; docs/GO_GLOBALS.md#compile-time-evaluation),
	// from Check: nil when the program has no initialization steps.
	Init *InitUse `json:"init,omitempty"`
	// RecursionCheckInconclusive is set when a call through a function value
	// has possible targets that Check could not resolve to a set whose call
	// graph is acyclic, with the reasons in RecursionCheckReasons (added
	// within gosvm-features-v1). Check resolves every such call to all
	// targets of its signature (the address-taken set) and rejects any
	// cycle in the resulting graph, so a checked program's call graph is
	// proven acyclic and the flag is always false; it exists so a backend
	// can rely on that proof. See docs/GO_FUNCTIONS.md#recursion.
	RecursionCheckInconclusive bool     `json:"recursion_check_inconclusive"`
	RecursionCheckReasons      []string `json:"recursion_check_reasons,omitempty"`
	// Trimmed lists, per trimmed package, what opt-in trimming removed
	// (docs/GO_PACKAGES.md#trimming); omitted without trimming. The
	// language counters above describe only the declarations kept. Added
	// within gosvm-features-v1.
	Trimmed []TrimmedUse `json:"trimmed,omitempty"`
}

// TrimmedUse is what trimming removed from one package: its dropped
// declarations ("kind name", kind func, method, type, var or const; a var
// or const specification's names joined by ", "), the dropped var
// specifications with initializers (which Go would run), and the imports
// only dropped declarations used (type information, neither compiled nor
// initialized). All sorted.
type TrimmedUse struct {
	Package             string   `json:"package"`
	DroppedDeclarations []string `json:"dropped_declarations"`
	DroppedInitializers []string `json:"dropped_initializers"`
	TypeOnlyImports     []string `json:"type_only_imports"`
}

// InitUse is a program's package initialization after compile-time
// evaluation (docs/GO_GLOBALS.md#compile-time-evaluation).
type InitUse struct {
	// Cost is the per-invocation cost class: "image" (every step was
	// evaluated at compile time; each invocation only installs the image,
	// a cost independent of what the initializers compute), "image+runtime"
	// (the image, then the remaining steps run) or "runtime" (no step was
	// evaluated; every step runs on every invocation).
	Cost string `json:"cost"`
	// Steps counts the initialization steps (a statement of a package's
	// variable initializer, or an init function) and EvaluatedSteps the
	// leading ones evaluated at compile time.
	Steps          int `json:"steps"`
	EvaluatedSteps int `json:"evaluated_steps"`
	// ImageBytes is the heap in use after the evaluated steps (the image).
	ImageBytes int64 `json:"image_bytes"`
	// Stop is why the first remaining step runs at run time:
	// "<package path>/<file>:<line>: <reason>".
	Stop string `json:"stop,omitempty"`
	// CompileTime and Runtime list the package-level variables (pkg/path.V)
	// whose initializer was evaluated at compile time, or runs on every
	// invocation; InitFuncs counts the init functions each way.
	CompileTime          []string `json:"compile_time"`
	Runtime              []string `json:"runtime"`
	CompileTimeInitFuncs int      `json:"compile_time_init_funcs"`
	RuntimeInitFuncs     int      `json:"runtime_init_funcs"`
}

// initUse summarizes cp's initialization, or nil without steps.
func initUse(cp *compiler.CheckedProgram) *InitUse {
	steps := cp.InitSteps()
	if len(steps) == 0 {
		return nil
	}
	use := &InitUse{Steps: len(steps), Cost: "runtime", CompileTime: []string{}, Runtime: []string{}}
	if img := cp.InitImage; img != nil {
		use.EvaluatedSteps, use.ImageBytes = img.Steps, img.HeapUsed
		use.Cost = "image+runtime"
		if img.Steps == img.Total {
			use.Cost = "image"
		}
		if img.Stop != nil {
			pos := cp.Fset.Position(img.Stop.Pos)
			use.Stop = fmt.Sprintf("%s/%s:%d: %s", steps[img.Stop.Step].Init.Package.Path(), filepath.Base(pos.Filename), pos.Line, img.Stop.Reason)
		}
	}
	for i, s := range steps {
		evaluated := i < use.EvaluatedSteps
		if s.Stmt == nil {
			if evaluated {
				use.CompileTimeInitFuncs++
			} else {
				use.RuntimeInitFuncs++
			}
			continue
		}
		assign, ok := s.Stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for _, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			name := s.Init.Package.Path() + "." + id.Name
			if evaluated {
				use.CompileTime = append(use.CompileTime, name)
			} else {
				use.Runtime = append(use.Runtime, name)
			}
		}
	}
	sort.Strings(use.CompileTime)
	sort.Strings(use.Runtime)
	return use
}

// ApplyCheck records Check's verdict and heap moves in u.Uses.
func ApplyCheck(u *Usage, cp *compiler.CheckedProgram, err error) {
	if err != nil {
		u.Uses.Checked, u.Uses.CheckError = false, err.Error()
		return
	}
	u.Uses.Checked = true
	u.Uses.HeapMovedLocals, u.Uses.HeapMovedLiterals = 0, 0
	for _, f := range cp.Functions {
		u.Uses.HeapMovedLocals += len(f.HeapVars)
		u.Uses.HeapMovedLiterals += len(f.HeapLiterals)
	}
	u.Uses.GlobalHeapBytes, u.Uses.ReadOnlyGlobals = cp.GlobalBytes, 0
	for _, g := range cp.Globals {
		if g.ReadOnly {
			u.Uses.ReadOnlyGlobals++
		}
	}
	if u.Uses.HeapMovedLocals+u.Uses.HeapMovedLiterals > 0 || cp.GlobalBytes > 0 {
		u.Uses.Allocation = true
	}
	u.Uses.Init = initUse(cp)
}

// summarize derives the Uses flags from the collected counters.
func summarize(u *Usage) {
	sum := func(name string) int {
		return u.Language.Handwritten.Constructs[name] + u.Language.Generated.Constructs[name]
	}
	has := func(m func(*Side) map[string]int, keys ...string) bool {
		for _, side := range []*Side{u.Language.Handwritten, u.Language.Generated} {
			for _, k := range keys {
				if m(side)[k] > 0 {
					return true
				}
			}
		}
		return false
	}
	types := func(s *Side) map[string]int { return s.IntegerTypes }
	values := func(s *Side) map[string]int { return s.IntegerValues }
	conversions := func(s *Side) map[string]int { return s.Conversions }
	signed := []string{"int8", "int16", "int32", "int64", "int"}
	u.Uses.Allocation = sum("new_calls")+sum("make_calls")+sum("append_calls")+sum("copy_calls")+sum("slice_literals")+sum("map_literals") > 0
	u.Uses.Interfaces = sum("interface_values") > 0
	u.Uses.SignedIntegers = has(types, signed...) || has(conversions, signed...) || sum("signed_operations") > 0
	u.Uses.MathBits = false
	for _, call := range u.StdlibCalls {
		if call.Package == "math/bits" {
			u.Uses.MathBits = true
		}
	}
	u.Uses.Generics = len(u.Generics) > 0 || sum("type_parameters") > 0
	u.Uses.Uint16 = has(types, "uint16") || has(values, "uint16")
	u.Uses.Range = sum("for_range") > 0
	u.Uses.Strings = sum("string_values") > 0
	u.Uses.Closures = sum("function_literals")+sum("function_values")+sum("method_values")+sum("method_expressions")+sum("indirect_calls") > 0
	u.Uses.Defer = sum("defer") > 0
	u.Uses.Maps = sum("map_values")+sum("map_literals")+sum("map_makes") > 0
	u.Uses.Globals = sum("package_vars")+sum("init_functions") > 0
	u.Uses.StructEquality = sum("struct_equality")+sum("struct_switch_cases") > 0
	u.Uses.TypeAliases = sum("type_aliases") > 0
	u.Uses.CompositeArrays = sum("composite_array_types") > 0
	u.Uses.PointerConversions = sum("pointer_conversions") > 0
	u.Uses.Panic = has(func(s *Side) map[string]int { return s.Builtins }, "panic")
}

// GenericUse is one generic function, method or type and its instantiations.
type GenericUse struct {
	// Name is "pkg/path.F", "pkg/path.T.M" or "pkg/path.T".
	Name    string `json:"name"`
	Package string `json:"package"`
	// Kind is "function", "method" or "type".
	Kind string `json:"kind"`
	// Count is the number of distinct instantiations (len(Instances)).
	Count int `json:"count"`
	// Instances are the canonical instantiations, sorted, for example
	// "example.org/app.Min[uint64]".
	Instances []string `json:"instances"`
}

// Program identifies the measured program. Module and Targets are filled by
// callers that know the project; Report sets EntryPackage and SDKPackages.
type Program struct {
	Module       string   `json:"module,omitempty"`
	EntryPackage string   `json:"entry_package"`
	Targets      []Target `json:"targets,omitempty"`
	Packages     []string `json:"packages"`
	SDKPackages  []string `json:"sdk_packages"`
}

// Target is one configured network/profile pair.
type Target struct {
	Network string `json:"network,omitempty"`
	Profile string `json:"profile"`
}

// File describes one program source file. Path is relative to the common
// directory of all reported files and uses forward slashes.
type File struct {
	Path    string `json:"path"`
	Package string `json:"package"`
	Kind    string `json:"kind"`
	SHA256  string `json:"sha256,omitempty"`
	Lines   int    `json:"lines"`
}

// Split is a handwritten/generated pair.
type Split struct {
	Handwritten int `json:"handwritten"`
	Generated   int `json:"generated"`
}

// Language holds construct usage per file kind.
type Language struct {
	Handwritten *Side `json:"handwritten"`
	Generated   *Side `json:"generated"`
}

// Side counts constructs in one class of files.
type Side struct {
	Files int `json:"files"`
	Lines int `json:"lines"`
	// Constructs always contains every key in ConstructNames.
	Constructs     map[string]int `json:"constructs"`
	Operators      map[string]int `json:"operators"`
	OperatorClass  map[string]int `json:"operator_classes"`
	Builtins       map[string]int `json:"builtins"`
	IntegerTypes   map[string]int `json:"integer_types"`
	IntegerValues  map[string]int `json:"integer_value_types"`
	Conversions    map[string]int `json:"conversions"`
	Arrays         []ArrayUse     `json:"arrays"`
	Imports        []string       `json:"imports"`
	MaxLoopNesting int            `json:"max_loop_nesting"`
}

// ArrayUse counts fixed-array type expressions written in source.
type ArrayUse struct {
	Type   string `json:"type"`
	Elem   string `json:"elem"`
	Length int64  `json:"length"`
	Count  int    `json:"count"`
}

// CallSite aggregates calls to one function or method.
type CallSite struct {
	Name        string   `json:"name"`
	Package     string   `json:"package"`
	Method      bool     `json:"method"`
	Count       int      `json:"count"`
	Handwritten int      `json:"handwritten"`
	Generated   int      `json:"generated"`
	Positions   []string `json:"positions"`
}

// ConstructNames lists every reported construct counter, in schema order.
var ConstructNames = []string{
	"functions", "methods_declared", "pointer_receivers", "value_receivers", "function_literals",
	"params", "pointer_params", "results", "pointer_results", "multi_result_functions",
	"struct_type_decls", "array_type_decls", "other_type_decls", "type_aliases",
	"struct_literals", "array_literals", "slice_literals", "keyed_literals",
	"array_equality", "struct_equality",
	"slice_expressions", "slice_expressions_of_arrays", "index_expressions", "field_selections",
	"address_of", "pointer_derefs", "pointer_types", "slice_types", "map_types", "interface_types", "channel_types",
	"function_calls", "cross_package_calls", "method_calls", "sdk_calls", "library_calls", "builtin_calls", "indirect_calls", "conversions",
	"assignments", "compound_assignments", "multi_value_assignments", "short_var_decls", "var_decls", "multi_value_returns", "returns",
	"if", "else", "else_if", "switch", "switch_tagless", "switch_cases", "type_switch", "fallthrough",
	"for_three_clause", "for_condition_only", "for_infinite", "for_range", "for_range_array", "for_range_slice", "for_range_int", "for_range_other",
	"break", "continue", "labels", "goto", "defer", "go", "select",
	"const_typed", "const_untyped",
	"int_literals", "char_literals", "string_literals",
	"signed_division", "signed_shift_counts",
	// Generics (added within gosvm-features-v1).
	"generic_functions", "generic_methods", "generic_types", "type_parameters",
	"explicit_instantiations", "inferred_instantiations", "type_instantiations",
	// Allocation (added within gosvm-features-v1).
	"new_calls", "make_calls", "append_calls", "append_spreads", "copy_calls",
	// Feature summary inputs (added within gosvm-features-v1): non-constant
	// arithmetic/shift/bitwise operations with a signed integer result, and
	// expressions whose value has an interface type.
	"signed_operations", "interface_values",
	// Interface values (added within gosvm-features-v1): conversions of
	// non-interface values to interfaces (implicit or explicit; nil and
	// interface-to-interface conversions excluded), method calls on interface
	// values, and x.(T) type assertions (type switch guards excluded; type
	// switches are type_switch).
	"interface_conversions", "dynamic_calls", "type_assertions",
	// Strings (added within gosvm-features-v1): non-constant expressions
	// of a string type, non-constant string + operators and += on strings,
	// non-constant conversions between strings and byte or rune slices or
	// from integers (not string-to-string), and range over a string.
	"string_values", "string_concatenations", "string_conversions", "string_ranges",
	// Function values (added within gosvm-features-v1): declared functions
	// (or generic instances) used as values rather than called, method
	// values x.M and method expressions T.M not called, and functions and
	// literals with named results. Literals are function_literals, calls
	// through values indirect_calls and defer statements defer.
	"function_values", "method_values", "method_expressions", "named_results",
	// Maps (added within gosvm-features-v1): non-constant, non-nil
	// expressions of a map type, map composite literals, make of a map,
	// single-value m[k] reads, v, ok := m[k] reads, m[k] as an assignment,
	// ++/-- or += target, delete calls and clear calls on maps.
	"map_values", "map_literals", "map_makes", "map_lookups", "map_comma_ok_lookups", "map_assignments", "map_deletes", "map_clears",
	// Package-level variables (added within gosvm-features-v1): named or
	// blank variables declared at package scope (also counted in var_decls)
	// and init functions.
	"package_vars", "init_functions",
	// Value types (added within gosvm-features-v1): case values compared
	// with a struct switch tag, and array type expressions whose element
	// type is a struct, array, pointer or slice.
	"struct_switch_cases", "composite_array_types",
	// Pointer conversions (added within gosvm-features-v1): conversions
	// (*T)(p) to a pointer type (docs/GO_POINTERS.md#conversions).
	"pointer_conversions",
}

// allocationConstructs counts the allocation builtins by name.
var allocationConstructs = map[string]string{"new": "new_calls", "make": "make_calls", "append": "append_calls", "copy": "copy_calls"}

var operatorClass = map[string]string{
	"+": "arithmetic", "-": "arithmetic", "*": "arithmetic", "/": "arithmetic", "%": "arithmetic",
	"<<": "shift", ">>": "shift",
	"&": "bitwise", "|": "bitwise", "^": "bitwise", "&^": "bitwise",
	"==": "comparison", "!=": "comparison", "<": "comparison", "<=": "comparison", ">": "comparison", ">=": "comparison",
	"&&": "logical", "||": "logical", "!": "logical",
	"++": "incdec", "--": "incdec",
}

var generatedRE = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// Report reads each program file from disk (for hashes and generated headers)
// and counts constructs from the typed syntax. SDK package files are excluded
// from language counts; calls into them are reported as SDK calls.
func Report(tp *compiler.TypedProgram) (*Usage, error) {
	if tp == nil || tp.Entry == nil || tp.Info == nil || tp.Fset == nil {
		return nil, fmt.Errorf("features: incomplete typed program")
	}
	scopes := map[*types.Scope]string{}
	for path, pkg := range tp.Packages {
		scopes[pkg.Scope()] = path
	}
	type input struct {
		file *ast.File
		pkg  string
		abs  string
		info File
	}
	var inputs []*input
	libraries := map[string]bool{}
	sdkPackages := map[string]bool{}
	stdlibPackages := map[string]bool{}
	var packages []string
	for _, f := range tp.Files {
		scope := tp.Info.Scopes[f]
		if scope == nil {
			return nil, fmt.Errorf("features: file scope missing for %s", tp.Fset.Position(f.Package).Filename)
		}
		pkg, ok := scopes[scope.Parent()]
		if !ok {
			return nil, fmt.Errorf("features: package missing for %s", tp.Fset.Position(f.Package).Filename)
		}
		if isSDK(pkg) {
			sdkPackages[pkg] = true
			continue
		}
		name := tp.Fset.Position(f.Package).Filename
		if compiler.IsStdlibSource(name) {
			stdlibPackages[pkg] = true
			continue
		}
		in := &input{file: f, pkg: pkg, info: File{Package: pkg, Lines: tp.Fset.File(f.Pos()).LineCount()}}
		if filepath.Base(name) == "gosvm-library-stub.go" {
			libraries[pkg] = true
			in.info.Kind = LibraryStub
		} else {
			data, err := os.ReadFile(name)
			if err != nil {
				return nil, fmt.Errorf("features: %w", err)
			}
			sum := sha256.Sum256(data)
			in.info.SHA256 = hex.EncodeToString(sum[:])
			in.info.Lines = countLines(data)
			in.info.Kind = Handwritten
			if isGenerated(data) {
				in.info.Kind = Generated
			}
		}
		abs, err := filepath.Abs(name)
		if err != nil {
			return nil, err
		}
		in.abs = abs
		inputs = append(inputs, in)
	}
	for path := range tp.Packages {
		if isSDK(path) {
			sdkPackages[path] = true
		} else if compiler.IsStdlibPackage(path) {
			stdlibPackages[path] = true
		} else {
			packages = append(packages, path)
		}
	}
	// gosvm/solana is checked by a separate importer; record it when imported.
	for _, in := range inputs {
		for _, im := range in.file.Imports {
			if p := strings.Trim(im.Path.Value, `"`); isSDK(p) {
				sdkPackages[p] = true
			}
		}
	}
	root := ""
	for _, in := range inputs {
		dir := filepath.Dir(in.abs)
		if root == "" {
			root = dir
		}
		for !within(root, dir) {
			root = filepath.Dir(root)
		}
	}
	for _, in := range inputs {
		rel, err := filepath.Rel(root, in.abs)
		if err != nil {
			return nil, err
		}
		in.info.Path = filepath.ToSlash(rel)
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].info.Path < inputs[j].info.Path })

	u := &Usage{Schema: Schema, Program: Program{EntryPackage: tp.Entry.Path(), Packages: sorted(packages), SDKPackages: sortedSet(sdkPackages)},
		Files: []File{}, SDKCalls: []CallSite{}, LibraryCalls: []CallSite{}, StdlibCalls: []CallSite{},
		Language: Language{Handwritten: newSide(), Generated: newSide()}}
	sdkCalls, libCalls, stdCalls := map[string]*CallSite{}, map[string]*CallSite{}, map[string]*CallSite{}
	imports := map[string]map[string]bool{Handwritten: {}, Generated: {}}
	arrays := map[string]map[string]*ArrayUse{Handwritten: {}, Generated: {}}
	for _, in := range inputs {
		u.Files = append(u.Files, in.info)
		if in.info.Kind == LibraryStub {
			continue
		}
		side := u.Language.Handwritten
		if in.info.Kind == Generated {
			side = u.Language.Generated
			u.Lines.Generated += in.info.Lines
		} else {
			u.Lines.Handwritten += in.info.Lines
		}
		side.Files++
		side.Lines += in.info.Lines
		for _, im := range in.file.Imports {
			imports[in.info.Kind][strings.Trim(im.Path.Value, `"`)] = true
		}
		w := &walker{tp: tp, side: side, kind: in.info.Kind, path: in.info.Path, pkg: in.pkg, libraries: libraries,
			sdk: sdkCalls, lib: libCalls, std: stdCalls, arrays: arrays[in.info.Kind]}
		w.file(in.file)
		if d := maxLoopDepth(in.file); d > side.MaxLoopNesting {
			side.MaxLoopNesting = d
		}
	}
	for kind, side := range map[string]*Side{Handwritten: u.Language.Handwritten, Generated: u.Language.Generated} {
		side.Imports = sorted(keys(imports[kind]))
		for _, a := range arrays[kind] {
			side.Arrays = append(side.Arrays, *a)
		}
		sort.Slice(side.Arrays, func(i, j int) bool { return side.Arrays[i].Type < side.Arrays[j].Type })
		if side.Arrays == nil {
			side.Arrays = []ArrayUse{}
		}
	}
	u.SDKCalls = flatten(sdkCalls)
	u.LibraryCalls = flatten(libCalls)
	u.StdlibCalls = flatten(stdCalls)
	var program []*ast.File
	for _, in := range inputs {
		if in.info.Kind != LibraryStub {
			program = append(program, in.file)
		}
	}
	generics, err := genericUses(tp, program)
	if err != nil {
		return nil, err
	}
	u.Generics = generics
	summarize(u)
	if len(stdlibPackages) > 0 {
		u.Uses.Stdlib = sortedSet(stdlibPackages)
	}
	u.Uses.Trimmed = trimmedUses(tp.Trimmed)
	return u, nil
}

// trimmedUses reports what trimming removed (docs/GO_PACKAGES.md#trimming):
// one entry per trimmed package, nil without trimming.
func trimmedUses(r *compiler.TrimReport) []TrimmedUse {
	if r == nil {
		return nil
	}
	out := []TrimmedUse{}
	for _, p := range r.Packages {
		t := TrimmedUse{Package: p.Path, DroppedDeclarations: []string{}, DroppedInitializers: []string{}, TypeOnlyImports: append([]string{}, p.TypeOnlyImports...)}
		for _, d := range p.Dropped {
			t.DroppedDeclarations = append(t.DroppedDeclarations, d.Kind+" "+d.Name)
			if d.Initializer {
				t.DroppedInitializers = append(t.DroppedInitializers, d.Name)
			}
		}
		sort.Strings(t.DroppedDeclarations)
		sort.Strings(t.DroppedInitializers)
		sort.Strings(t.TypeOnlyImports)
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out
}

// genericUses lists the program's generic declarations with the
// instantiations compiler.Instantiations finds.
func genericUses(tp *compiler.TypedProgram, files []*ast.File) ([]GenericUse, error) {
	uses := map[string]*GenericUse{}
	add := func(name, pkg, kind string) {
		if uses[name] == nil {
			uses[name] = &GenericUse{Name: name, Package: pkg, Kind: kind, Instances: []string{}}
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				fn, ok := tp.Info.Defs[d.Name].(*types.Func)
				if !ok || fn.Pkg() == nil {
					continue
				}
				sig := fn.Type().(*types.Signature)
				if sig.TypeParams().Len() > 0 {
					add(fn.Pkg().Path()+"."+fn.Name(), fn.Pkg().Path(), "function")
				} else if sig.RecvTypeParams().Len() > 0 {
					recv := sig.Recv().Type()
					if p, ok := recv.(*types.Pointer); ok {
						recv = p.Elem()
					}
					add(fn.Pkg().Path()+"."+recv.(*types.Named).Obj().Name()+"."+fn.Name(), fn.Pkg().Path(), "method")
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.TypeParams != nil {
						if tn, ok := tp.Info.Defs[ts.Name].(*types.TypeName); ok && tn.Pkg() != nil {
							add(tn.Pkg().Path()+"."+tn.Name(), tn.Pkg().Path(), "type")
						}
					}
				}
			}
		}
	}
	instances, err := compiler.Instantiations(tp)
	if err != nil {
		return nil, fmt.Errorf("features: %w", err)
	}
	for _, inst := range instances {
		if use := uses[inst.Generic]; use != nil {
			use.Instances = append(use.Instances, inst.Key)
		}
	}
	out := []GenericUse{}
	for _, use := range uses {
		sort.Strings(use.Instances)
		use.Count = len(use.Instances)
		out = append(out, *use)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Marshal renders the report deterministically (sorted map keys, indented).
func Marshal(u *Usage) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	if err := e.Encode(u); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func isSDK(path string) bool { return path == "gosvm/solana" || strings.HasPrefix(path, "gosvm/sdk/") }

// isStdlib reports the standard-library packages the compiler provides to
// on-chain code. Their calls are listed in stdlib_calls and, as calls into
// another package, still count as cross_package_calls.
func isStdlib(path string) bool { return path == "math/bits" || compiler.IsStdlibPackage(path) }

func isGenerated(data []byte) bool {
	// Go's convention: the marker must precede the package clause.
	if i := bytes.Index(data, []byte("\npackage ")); i >= 0 {
		data = data[:i+1]
	} else if bytes.HasPrefix(data, []byte("package ")) {
		return false
	}
	return generatedRE.Match(data)
}

func countLines(data []byte) int {
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func within(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func newSide() *Side {
	s := &Side{Constructs: map[string]int{}, Operators: map[string]int{}, OperatorClass: map[string]int{}, Builtins: map[string]int{},
		IntegerTypes: map[string]int{}, IntegerValues: map[string]int{}, Conversions: map[string]int{}}
	for _, n := range ConstructNames {
		s.Constructs[n] = 0
	}
	return s
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sorted(s []string) []string {
	if s == nil {
		s = []string{}
	}
	sort.Strings(s)
	return s
}

func sortedSet(m map[string]bool) []string { return sorted(keys(m)) }

func flatten(m map[string]*CallSite) []CallSite {
	out := []CallSite{}
	for _, c := range m {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func maxLoopDepth(root ast.Node) int {
	best := 0
	ast.Inspect(root, func(n ast.Node) bool {
		if n == root {
			return true
		}
		switch n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			if d := 1 + maxLoopDepth(n); d > best {
				best = d
			}
			return false
		}
		return true
	})
	return best
}

type walker struct {
	tp        *compiler.TypedProgram
	side      *Side
	kind      string
	path      string
	pkg       string
	libraries map[string]bool
	sdk, lib  map[string]*CallSite
	std       map[string]*CallSite
	arrays    map[string]*ArrayUse
}

// packageLevel reports whether v is declared at package scope, including a
// blank variable (which go/types does not insert into any scope).
func packageLevel(v *types.Var) bool {
	if v.Pkg() == nil {
		return false
	}
	if v.Parent() != nil {
		return v.Parent() == v.Pkg().Scope()
	}
	inner := v.Pkg().Scope().Innermost(v.Pos())
	return inner != nil && inner.Parent() == v.Pkg().Scope() // a file scope
}

func (w *walker) count(name string) {
	if _, ok := w.side.Constructs[name]; !ok {
		panic("features: unknown construct " + name)
	}
	w.side.Constructs[name]++
}

func (w *walker) op(op string) {
	c := operatorClass[op]
	if c == "" {
		c = "other"
	}
	w.opClass(op, c)
}

func (w *walker) opClass(op, class string) {
	w.side.Operators[op]++
	w.side.OperatorClass[class]++
}

func (w *walker) typeString(t types.Type) string { return types.TypeString(t, nil) }

func (w *walker) isType(e ast.Expr) bool {
	tv, ok := w.tp.Info.Types[e]
	return ok && tv.IsType()
}

func (w *walker) typeOf(e ast.Expr) types.Type {
	if tv, ok := w.tp.Info.Types[e]; ok {
		return tv.Type
	}
	return nil
}

// isSignedInteger reports whether t is a typed signed integer type.
// isString reports whether t is a string type (or an untyped string).
func isString(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// isMapType reports whether t is a map type (or a named one).
func isMapType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

func isSignedInteger(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&(types.IsUnsigned|types.IsUntyped) == 0
}

func integerName(b *types.Basic) string {
	if b.Kind() == types.Uint8 {
		return "byte"
	}
	return b.Name()
}

func (w *walker) file(f *ast.File) {
	conversions, calls, asserts := compiler.ScanInterfaces(w.tp.Info, []*ast.File{f})
	for _, c := range conversions {
		if c.From != types.Typ[types.UntypedNil] {
			w.count("interface_conversions")
		}
	}
	w.side.Constructs["dynamic_calls"] += len(calls)
	w.side.Constructs["type_assertions"] += len(asserts)
	elseIf := map[ast.Stmt]bool{}
	mapTargets := map[ast.Expr]bool{} // m[k] written by an assignment or ++/--
	// Generic function names with written type arguments (F[T] or pkg.F[T]).
	explicit := map[*ast.Ident]bool{}
	// Callee expressions of calls (a function value is one that is not
	// called) and the selector names, which count at their selector.
	called := map[ast.Expr]bool{}
	selected := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		var x ast.Expr
		switch n := n.(type) {
		case *ast.IndexExpr:
			x = n.X
		case *ast.IndexListExpr:
			x = n.X
		case *ast.CallExpr:
			fun := ast.Unparen(n.Fun)
			switch f := fun.(type) {
			case *ast.IndexExpr:
				if w.instantiation(f.X) {
					fun = f.X
				}
			case *ast.IndexListExpr:
				if w.instantiation(f.X) {
					fun = f.X
				}
			}
			called[fun] = true
		case *ast.SelectorExpr:
			selected[n.Sel] = true
		}
		switch x := x.(type) {
		case *ast.Ident:
			explicit[x] = true
		case *ast.SelectorExpr:
			explicit[x.Sel] = true
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if tv, ok := w.tp.Info.Types[e]; ok && tv.IsValue() {
				if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUntyped == 0 {
					w.side.IntegerValues[integerName(b)]++
					if b.Info()&types.IsUnsigned == 0 && tv.Value == nil {
						switch x := e.(type) {
						case *ast.BinaryExpr:
							if c := operatorClass[x.Op.String()]; c == "arithmetic" || c == "shift" || c == "bitwise" {
								w.count("signed_operations")
							}
						case *ast.UnaryExpr:
							if x.Op.String() == "-" || x.Op.String() == "^" {
								w.count("signed_operations")
							}
						}
					}
				}
				if _, isParam := tv.Type.(*types.TypeParam); !isParam && types.IsInterface(tv.Type) && tv.Type != types.Typ[types.UntypedNil] {
					w.count("interface_values")
				}
				if isString(tv.Type) && tv.Value == nil {
					w.count("string_values")
				}
				if _, isParam := tv.Type.(*types.TypeParam); !isParam && isMapType(tv.Type) && tv.Value == nil {
					w.count("map_values")
				}
			}
		}
		// ++, -- and compound assignments on signed operands are signed
		// operations too.
		var signedTarget ast.Expr
		switch n := n.(type) {
		case *ast.IncDecStmt:
			signedTarget = n.X
		case *ast.AssignStmt:
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE && len(n.Lhs) == 1 {
				signedTarget = n.Lhs[0]
			}
		}
		if signedTarget != nil {
			if isSignedInteger(w.typeOf(signedTarget)) {
				w.count("signed_operations")
			}
		}
		switch n := n.(type) {
		case *ast.Ident:
			if fn, ok := w.tp.Info.Uses[n].(*types.Func); ok && !called[n] && !selected[n] && fn.Type().(*types.Signature).Recv() == nil {
				w.count("function_values")
			}
			if _, ok := w.tp.Info.Instances[n]; ok {
				switch w.tp.Info.Uses[n].(type) {
				case *types.TypeName:
					w.count("type_instantiations")
				case *types.Func:
					if explicit[n] {
						w.count("explicit_instantiations")
					} else {
						w.count("inferred_instantiations")
					}
				}
			}
			if tn, ok := w.tp.Info.Uses[n].(*types.TypeName); ok && tn.Parent() == types.Universe {
				if b, ok := tn.Type().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
					w.side.IntegerTypes[b.Name()]++
				}
			}
		case *ast.FuncDecl:
			w.funcDecl(n)
			if n.Recv == nil && n.Name.Name == "init" {
				w.count("init_functions")
			}
			if n.Type.TypeParams != nil {
				w.count("generic_functions")
				w.typeParams(n.Type.TypeParams)
			} else if fn, ok := w.tp.Info.Defs[n.Name].(*types.Func); ok && fn.Type().(*types.Signature).RecvTypeParams().Len() > 0 {
				w.count("generic_methods")
			}
		case *ast.FuncLit:
			w.count("function_literals")
			if sig, ok := w.typeOf(n).(*types.Signature); ok && sig.Results().Len() > 0 && sig.Results().At(0).Name() != "" {
				w.count("named_results")
			}
		case *ast.TypeSpec:
			if n.TypeParams != nil {
				w.count("generic_types")
				w.typeParams(n.TypeParams)
			}
			switch {
			case n.Assign.IsValid():
				w.count("type_aliases")
			default:
				switch n.Type.(type) {
				case *ast.StructType:
					w.count("struct_type_decls")
				case *ast.ArrayType:
					if n.Type.(*ast.ArrayType).Len != nil {
						w.count("array_type_decls")
					} else {
						w.count("other_type_decls")
					}
				default:
					w.count("other_type_decls")
				}
			}
		case *ast.ValueSpec:
			for _, name := range n.Names {
				switch obj := w.tp.Info.Defs[name].(type) {
				case *types.Const:
					if b, ok := obj.Type().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
						w.count("const_untyped")
					} else {
						w.count("const_typed")
					}
				case *types.Var:
					w.count("var_decls")
					if packageLevel(obj) {
						w.count("package_vars")
					}
				}
			}
			if len(n.Names) > 1 && len(n.Values) == 1 {
				w.count("multi_value_assignments")
			}
		case *ast.CompositeLit:
			if len(n.Elts) > 0 {
				if _, ok := n.Elts[0].(*ast.KeyValueExpr); ok {
					w.count("keyed_literals")
				}
			}
			if t := w.typeOf(n); t != nil {
				switch t.Underlying().(type) {
				case *types.Struct:
					w.count("struct_literals")
				case *types.Array:
					w.count("array_literals")
				case *types.Slice:
					w.count("slice_literals")
				case *types.Map:
					w.count("map_literals")
				}
			}
		case *ast.ArrayType:
			if n.Len == nil {
				w.count("slice_types")
				break
			}
			if a, ok := w.typeOf(n).(*types.Array); ok {
				switch a.Elem().Underlying().(type) {
				case *types.Struct, *types.Array, *types.Pointer, *types.Slice:
					w.count("composite_array_types")
				}
				key := w.typeString(a)
				use := w.arrays[key]
				if use == nil {
					use = &ArrayUse{Type: key, Elem: w.typeString(a.Elem()), Length: a.Len()}
					w.arrays[key] = use
				}
				use.Count++
			}
		case *ast.MapType:
			w.count("map_types")
		case *ast.InterfaceType:
			w.count("interface_types")
		case *ast.ChanType:
			w.count("channel_types")
		case *ast.StarExpr:
			if w.isType(n) {
				w.count("pointer_types")
			} else {
				w.count("pointer_derefs")
			}
		case *ast.UnaryExpr:
			switch n.Op {
			case token.AND:
				w.count("address_of")
			case token.SUB:
				w.opClass("unary-", "arithmetic")
			case token.ADD:
				w.opClass("unary+", "arithmetic")
			case token.XOR:
				w.opClass("unary^", "bitwise")
			default:
				w.op(n.Op.String())
			}
		case *ast.BinaryExpr:
			w.op(n.Op.String())
			if n.Op == token.ADD && w.tp.Info.Types[n].Value == nil && isString(w.typeOf(n)) {
				w.count("string_concatenations")
			}
			if w.tp.Info.Types[n].Value == nil {
				switch n.Op {
				case token.QUO, token.REM:
					if isSignedInteger(w.typeOf(n.X)) {
						w.count("signed_division")
					}
				case token.SHL, token.SHR:
					if isSignedInteger(w.typeOf(n.Y)) && w.tp.Info.Types[n.Y].Value == nil {
						w.count("signed_shift_counts")
					}
				}
			}
			if n.Op == token.EQL || n.Op == token.NEQ {
				if t := w.typeOf(n.X); t != nil {
					switch t.Underlying().(type) {
					case *types.Array:
						w.count("array_equality")
					case *types.Struct:
						w.count("struct_equality")
					}
				}
			}
		case *ast.SliceExpr:
			w.count("slice_expressions")
			if t := w.typeOf(n.X); t != nil {
				u := t.Underlying()
				if p, ok := u.(*types.Pointer); ok {
					u = p.Elem().Underlying()
				}
				if _, ok := u.(*types.Array); ok {
					w.count("slice_expressions_of_arrays")
				}
			}
		case *ast.IndexExpr:
			if !w.instantiation(n.X) {
				w.count("index_expressions")
			}
			if isMapType(w.typeOf(n.X)) {
				_, commaOK := w.typeOf(n).(*types.Tuple)
				switch {
				case mapTargets[n]:
					w.count("map_assignments")
				case commaOK:
					w.count("map_comma_ok_lookups")
				default:
					w.count("map_lookups")
				}
			}
		case *ast.SelectorExpr:
			if s := w.tp.Info.Selections[n]; s != nil && s.Kind() == types.FieldVal {
				w.count("field_selections")
			} else if s != nil && !called[n] {
				switch s.Kind() {
				case types.MethodVal:
					w.count("method_values")
				case types.MethodExpr:
					w.count("method_expressions")
				}
			} else if fn, ok := w.tp.Info.Uses[n.Sel].(*types.Func); ok && s == nil && !called[n] && fn.Type().(*types.Signature).Recv() == nil {
				w.count("function_values")
			}
		case *ast.CallExpr:
			w.call(n)
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				for _, lhs := range n.Lhs {
					mapTargets[ast.Unparen(lhs)] = true
				}
			}
			switch n.Tok {
			case token.DEFINE:
				w.count("short_var_decls")
			case token.ASSIGN:
				w.count("assignments")
			default:
				w.count("compound_assignments")
				w.op(n.Tok.String())
				if n.Tok == token.ADD_ASSIGN && len(n.Lhs) == 1 && isString(w.typeOf(n.Lhs[0])) {
					w.count("string_concatenations")
				}
			}
			if len(n.Lhs) > 1 && len(n.Rhs) == 1 {
				w.count("multi_value_assignments")
			}
		case *ast.IncDecStmt:
			mapTargets[ast.Unparen(n.X)] = true
			w.op(n.Tok.String())
		case *ast.ReturnStmt:
			w.count("returns")
			if len(n.Results) > 1 {
				w.count("multi_value_returns")
			}
		case *ast.IfStmt:
			if !elseIf[n] {
				w.count("if")
			}
			switch e := n.Else.(type) {
			case *ast.IfStmt:
				elseIf[e] = true
				w.count("else_if")
			case *ast.BlockStmt:
				w.count("else")
			}
		case *ast.SwitchStmt:
			w.count("switch")
			if n.Tag == nil {
				w.count("switch_tagless")
			} else if t := w.typeOf(n.Tag); t != nil {
				if _, ok := t.Underlying().(*types.Struct); ok {
					for _, stmt := range n.Body.List {
						w.side.Constructs["struct_switch_cases"] += len(stmt.(*ast.CaseClause).List)
					}
				}
			}
		case *ast.TypeSwitchStmt:
			w.count("type_switch")
		case *ast.CaseClause:
			w.count("switch_cases")
		case *ast.ForStmt:
			switch {
			case n.Init == nil && n.Cond == nil && n.Post == nil:
				w.count("for_infinite")
			case n.Init == nil && n.Post == nil:
				w.count("for_condition_only")
			default:
				w.count("for_three_clause")
			}
		case *ast.RangeStmt:
			w.count("for_range")
			if isString(w.typeOf(n.X)) {
				w.count("string_ranges")
			}
			kind := "for_range_other"
			if t := w.typeOf(n.X); t != nil {
				u := t.Underlying()
				if p, ok := u.(*types.Pointer); ok {
					u = p.Elem().Underlying()
				}
				switch u := u.(type) {
				case *types.Array:
					kind = "for_range_array"
				case *types.Slice:
					kind = "for_range_slice"
				case *types.Basic:
					if u.Info()&types.IsInteger != 0 {
						kind = "for_range_int"
					}
				}
			}
			w.count(kind)
		case *ast.BranchStmt:
			switch n.Tok {
			case token.BREAK:
				w.count("break")
			case token.CONTINUE:
				w.count("continue")
			case token.GOTO:
				w.count("goto")
			case token.FALLTHROUGH:
				w.count("fallthrough")
			}
		case *ast.LabeledStmt:
			w.count("labels")
		case *ast.DeferStmt:
			w.count("defer")
		case *ast.GoStmt:
			w.count("go")
		case *ast.SelectStmt:
			w.count("select")
		case *ast.BasicLit:
			switch n.Kind {
			case token.INT:
				w.count("int_literals")
			case token.CHAR:
				w.count("char_literals")
			case token.STRING:
				w.count("string_literals")
			}
		}
		return true
	})
	// Import paths are literals but not program values.
	w.side.Constructs["string_literals"] -= len(f.Imports)
}

func (w *walker) funcDecl(n *ast.FuncDecl) {
	fn, _ := w.tp.Info.Defs[n.Name].(*types.Func)
	if fn == nil {
		return
	}
	sig := fn.Type().(*types.Signature)
	if recv := sig.Recv(); recv != nil {
		w.count("methods_declared")
		if _, ok := recv.Type().(*types.Pointer); ok {
			w.count("pointer_receivers")
		} else {
			w.count("value_receivers")
		}
	} else {
		w.count("functions")
	}
	for i := 0; i < sig.Params().Len(); i++ {
		w.count("params")
		if _, ok := sig.Params().At(i).Type().(*types.Pointer); ok {
			w.count("pointer_params")
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		w.count("results")
		if _, ok := sig.Results().At(i).Type().(*types.Pointer); ok {
			w.count("pointer_results")
		}
	}
	if sig.Results().Len() > 1 {
		w.count("multi_result_functions")
	}
	if sig.Results().Len() > 0 && sig.Results().At(0).Name() != "" {
		w.count("named_results")
	}
}

// typeParams counts the type parameter names a declaration introduces.
func (w *walker) typeParams(list *ast.FieldList) {
	for _, field := range list.List {
		for range field.Names {
			w.count("type_parameters")
		}
	}
}

// instantiation reports whether x (the operand of F[T]) names a generic
// function or type, so that F[T] instantiates rather than indexes.
func (w *walker) instantiation(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident:
		_, ok := w.tp.Info.Instances[x]
		return ok
	case *ast.SelectorExpr:
		_, ok := w.tp.Info.Instances[x.Sel]
		return ok
	}
	return false
}

func (w *walker) call(n *ast.CallExpr) {
	fun := ast.Unparen(n.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		if w.instantiation(f.X) {
			fun = f.X
		}
	case *ast.IndexListExpr:
		if w.instantiation(f.X) {
			fun = f.X
		}
	}
	if tv, ok := w.tp.Info.Types[fun]; ok && tv.IsType() {
		w.count("conversions")
		w.side.Conversions[w.typeString(tv.Type)]++
		if _, ok := tv.Type.Underlying().(*types.Pointer); ok {
			w.count("pointer_conversions")
		}
		if len(n.Args) == 1 && w.tp.Info.Types[n].Value == nil {
			from := w.typeOf(n.Args[0])
			_, toSlice := tv.Type.Underlying().(*types.Slice)
			if isString(tv.Type) && !isString(from) || isString(from) && toSlice {
				w.count("string_conversions")
			}
		}
		return
	}
	var id *ast.Ident
	method := false
	switch f := fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
		if s := w.tp.Info.Selections[f]; s != nil && s.Kind() == types.MethodVal {
			method = true
		}
	}
	if id != nil {
		if b, ok := w.tp.Info.Uses[id].(*types.Builtin); ok {
			w.count("builtin_calls")
			w.side.Builtins[b.Name()]++
			if name, ok := allocationConstructs[b.Name()]; ok {
				w.count(name)
				if b.Name() == "append" && n.Ellipsis.IsValid() {
					w.count("append_spreads")
				}
			}
			switch {
			case b.Name() == "make" && len(n.Args) > 0 && isMapType(w.typeOf(n.Args[0])):
				w.count("map_makes")
			case b.Name() == "delete":
				w.count("map_deletes")
			case b.Name() == "clear" && len(n.Args) == 1 && isMapType(w.typeOf(n.Args[0])):
				w.count("map_clears")
			}
			return
		}
	}
	var fn *types.Func
	if id != nil {
		fn, _ = w.tp.Info.Uses[id].(*types.Func)
	}
	if fn == nil {
		w.count("indirect_calls")
		return
	}
	if method {
		w.count("method_calls")
	}
	pkg := ""
	if fn.Pkg() != nil {
		pkg = fn.Pkg().Path()
	}
	switch {
	case isSDK(pkg):
		w.count("sdk_calls")
		w.site(w.sdk, fn, pkg, method, n)
	case w.libraries[pkg]:
		w.count("library_calls")
		w.site(w.lib, fn, pkg, method, n)
	case method:
	case pkg == w.pkg:
		w.count("function_calls")
	default:
		w.count("cross_package_calls")
		if isStdlib(pkg) {
			w.site(w.std, fn, pkg, method, n)
		}
	}
}

func (w *walker) site(m map[string]*CallSite, fn *types.Func, pkg string, method bool, n *ast.CallExpr) {
	name := fn.FullName()
	c := m[name]
	if c == nil {
		c = &CallSite{Name: name, Package: pkg, Method: method, Positions: []string{}}
		m[name] = c
	}
	c.Count++
	if w.kind == Generated {
		c.Generated++
	} else {
		c.Handwritten++
	}
	c.Positions = append(c.Positions, fmt.Sprintf("%s:%d", w.path, w.tp.Fset.Position(n.Pos()).Line))
}
