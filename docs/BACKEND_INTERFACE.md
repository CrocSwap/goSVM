# Backend interface

goSVM separates the on-chain Go subset from code generation. An alternative
backend (for example Go typed AST → its own IR → SBF) consumes the same checked
values as the C/LLVM path and never re-implements the subset rules. The C/LLVM
path is unchanged: `CompileProgram` is now `Check` followed by C emission, and
the emitted C and ELFs are byte-identical to the pre-split compiler (see
[Equivalence evidence](#equivalence-evidence)).

```text
Program ──Check──▶ CheckedProgram ──emitC──▶ C ──Clang/LLD──▶ SBF ELF   (built in)
                                  └──your backend──▶ IR ──▶ SBF ELF       (alternative)
gosvm.json ──LoadSchema──▶ ResolvedSchema (layouts, instructions, PDAs, lifecycle)
```

All three APIs are in internal packages of module `gosvm`, so a backend lives in
this module or a fork of it.

## `compiler.Check`

`func Check(program *Program) (*CheckedProgram, error)` in
[`internal/compiler/check.go`](../internal/compiler/check.go). Build the
`Program` with `compiler.ReadProgram(dir)` (or `ReadProgramWithSources`). Check:

1. parses and type-checks the package graph with the pinned SDK in one
   `go/types` universe (dot/blank and illegal internal imports are rejected);
2. requires `Process([]byte, []byte) uint64` or `Process(solana.Context) uint64`;
3. requires bodies and rejects recursion (including through generic
   declarations, dynamic calls and function values);
4. monomorphizes generics: each instantiation becomes a synthetic non-generic
   declaration ([generics](#generics));
5. runs the borrow checker (no reference may outlive its storage; locals whose
   references escape are moved to the heap, separately for each instance of a
   generic declaration);
6. applies every declaration, type, statement and expression rule of the
   subset ([`subset.go`](../internal/compiler/subset.go), with the type rules
   in [`rules.go`](../internal/compiler/rules.go)).

It returns the first violation as a `file:line:col: message` error. The order and
text are the same as `CompileProgram` has always reported. The emitter applies no
language rules. If it reaches a construct Check should have rejected, it reports
`internal error: construct was not certified by Check`.

## `compiler.CheckedProgram`

Every field is documented in `check.go`. In summary:

| Field | Meaning |
| --- | --- |
| `TypedProgram` (embedded) | `Entry` package, `Packages` by import path (SDK excluded), `Files` in dependency-first order, shared `Info` and `Fset` |
| `Program` | The input, including `Libraries` and `SDK` |
| `SDK`, `SDKPackage` | SDK API version (0/2 current, 1 legacy) and the checked `gosvm/solana` package, or nil if it is not imported |
| `Process`, `EntryKind` | Entry function; `EntryBytes` or `EntryContext` |
| `Functions` | Every function and method in declaration order, each with `Decl`, direct program `Callees`, a `Library` binding if the body is native, an `Intrinsic` name for the standard library's intrinsics ([standard library](#standard-library-packages)), its `Borrows`, and the locals it moves to the heap (`HeapVars`, `HeapLiterals`); a generic declaration is replaced by its instances (`Instance` set), each with its own `Borrows` and heap moves |
| `CallOrder` | The same functions, callees before callers (acyclic); the possible targets of a dynamic call, or of a call through a function value, come before its caller |
| `Libraries` | Pure native library bindings in the graph |
| `Instances`, `TypeInstances`, `Instantiated` | Generic instantiations ([generics](#generics)); empty without generics |
| `Conversions`, `DynamicTypes`, `MethodSlots`, `DynamicCalls`, `TypeAssertions`, `TypeSwitches` | Interface values ([interfaces](#interfaces)); empty without them |
| `FuncTargets`, `FuncValues`, `FuncClasses`, `IndirectCalls`, `Closures`, `DeferSites` | Function values, closures and `defer` ([function values](#function-values)); empty without them |
| `Maps` | Map types ([maps](#maps)); empty without them |
| `Globals`, `GlobalBytes`, `Inits` | Package-level variables, the globals block size and each package's initialization in Go's order ([package-level variables](#package-level-variables)); empty (0) without them |
| `Hoisted`, `HoistedConversions`, `MapLiteralOrder`, `SharedOperands`; `Sequenced(e)` | gc's evaluation order where left to right would observably differ, and the sequenced operations ([evaluation order](#evaluation-order)); empty where it would not |

`BorrowSummary.Results[i]` gives the argument slots a returned pointer or slice
may alias. Slot 0 is the receiver for methods. `SDK` is true if the result may
alias SDK-owned storage such as account data; `Heap` is true if it may refer to
the invocation heap (an allocation, a moved local, or a reference loaded from a
field or element). A result never aliases the callee's own stack locals.
`BorrowSummary.Escapes` lists the argument slots whose referenced storage the
function may store into heap data; a caller that passes its own local there
moves that local to the heap.

### Allocation

Heap allocation ([GO_ALLOCATION.md](GO_ALLOCATION.md)) adds no IR of its own:
`new`, `make`, `append` and `copy` are `*types.Builtin` calls, slice literals
(and elided `&T{...}` elements, whose `types.TypeAndValue` type is `*T`) are
`*ast.CompositeLit`s, and slices of any admitted element are `*types.Slice`
values. Check decides heap placement: `Function.HeapVars` are local variables,
value parameters and receivers whose storage must be allocated on the heap (at
the declaration, each time it executes, or at function entry for parameters and
receivers), and `Function.HeapLiterals` are the `&T{...}` literals to allocate.
Every other local stays on the stack and is never referenced from heap data. A
backend must reproduce the allocator (an 8-byte in-use counter at the heap start,
8-byte rounding and alignment, abort when `Program.HeapBytes` (0: 32 KiB) is
exhausted), Go 1.22's `growslice` capacities, and the `makeslice` range checks.
`Program.HeapBytes` comes from `gosvm.json` `heap_bytes` or the CLI's
`-heap-bytes`.

`make` lengths and capacities, indexes and slice bounds may have any integer
type. A negative signed `make` operand traps like `len > cap`; a negative index
or bound traps like any out-of-range one (the C backend compares them as `u64`,
where a negative value is out of range). Instances of generic declarations are
analyzed separately: an instance's `HeapVars` are its own `Decl`'s objects
(its parameters are its `Object`'s signature parameters) and its
`HeapLiterals` are nodes of its own `Decl`, so `Box[int16]` and `Box[uint8]`
each move their own parameter and `Id[T]` moves nothing.

## Generics

Check lowers generics by monomorphization ([GO_GENERICS.md](GO_GENERICS.md)),
so a backend that lowers `Functions` needs no generic support of its own:

- `Instances` lists one `*Instance` per instantiation of a generic function or
  of a method of a generic type, in `Functions` order. `Origin` is the generic
  declaration's object, `TypeArgs` the concrete arguments (the receiver type's,
  for methods), `Object` the instance's own non-generic `*types.Func` (its name
  is `Name`, for example `Min[uint64]` or `Stack[uint8].Push`), and `Decl` a
  deep copy of the declaration without type parameters. `Key` is the
  package-path-qualified instantiation and `Symbol` the C name the built-in
  backend uses; both depend only on the declaration and the type arguments.
- Each instance is also a `Function` (with `Function.Instance` set) in place of
  its generic declaration, with its own `Callees`, its own `Borrows` and its
  own heap moves (`HeapVars`, `HeapLiterals`). Generic
  declarations themselves are not listed and have no code.
- `Instantiated` maps every identifier that names a generic callee at a call,
  in `Files` and in instance bodies, to the instance the call runs: `F` in
  `F(x)` and in `F[T](x)` (strip the index expression), `pkg.F`'s `F`, and a
  method selector's `Sel`. `Info.Uses` still records the generic declaration
  (functions) or go/types' instantiated method there.
- `TypeInstances` lists every instantiated generic type checked code uses, by
  `Key`. Its `Type` is a `*types.Named` whose `Underlying()` is substituted.
  go/types can keep identical instances spelled with `byte` and `uint8` as
  distinct `*types.Named`; they share a key, and their fields share
  `Var.Origin()`, so key types and fields by those rather than by pointer.

Every node of an instance `Decl` has substituted `Info` entries: `Types` (with
the original value mode and constant), `Defs` (fresh parameter, result and
local objects; `Defs[Decl.Name]` is `Object`), `Uses` (mapped locals; fields and
methods looked up again in the substituted types, so a call through a method
constraint names the concrete method) and `Scopes` (a mirror of the generic
declaration's scopes rooted at the function scope). Three things differ from
go/types' own output: `Selections` for an instance's selectors hold the generic
declaration's selection (its `Kind`, `Index` and `Indirect` are right; use
`Uses` and `Types` for the substituted method or field and types), `Instances`
has no entries for instance bodies (use `Instantiated`), and type-parameter
identifiers keep their `TypeName` objects (use `Types`). Instance nodes share
the generic declaration's positions, so diagnostics point into its source.

Check does not lower SDK calls. Calls into `gosvm/solana` and the `gosvm/sdk/*`
packages are ordinary typed calls. The C backend maps the SDK intrinsics onto its
runtime templates (`*.c.txt`). Another backend needs its own lowering for them.

`math/bits` calls look the same: they are standard-library calls in the typed
program (`types.Func` with `Pkg().Path() == "math/bits"`), are not listed in
`Function.Callees`, and have no Go body in the checked program. Check has
already certified that each is a supported function. A `RotateLeft*` count is
any `int` expression; when it is constant, its `types.TypeAndValue.Value` holds
it. The `uint` variants (`bits.Add`, `bits.Len`, ...) are the 64-bit functions,
because `uint` is 64 bits. The C backend lowers them to `bits.c.txt`; another
backend needs its own lowering, with the results and traps in
[GO_BITS.md](GO_BITS.md). `UintSize` is an untyped constant (64).

Integer types are ordinary `types.Basic` kinds: every fixed-size integer, plus
`int` and `uint` (both 64 bits), and named versions. `len`/`cap`/`copy`
results, range keys and `math/bits` counts are `int` values like any other. A backend must
implement Go's integer semantics (wrapping arithmetic, truncating signed
division with `MinInt / -1 == MinInt`, sign-filling `>>`, signed or unsigned
shift counts with a trap on a negative count, and sign- or zero-extending
conversions), as specified in [GO_INTEGERS.md](GO_INTEGERS.md). SBF has no
signed divide instruction, so signed division needs a lowering that does not
call `__divdi3`.

## Interfaces

Interface values ([GO_INTERFACES.md](GO_INTERFACES.md)) are ordinary
`types.Interface` values in the typed program; Check adds what lowering them
needs. All fields are additive and empty (or an empty map) for programs without
interface values:

- `Conversions` lists, in source order, every conversion of a non-interface
  value to an interface type: `Expr` (as written), `Index` (-1, or the element
  of a multiple-value call or comma-ok assertion), `From` (the concrete type,
  or `types.Typ[types.UntypedNil]` for nil, the zero interface value) and `To`.
  Implicit conversions are not visible in go/types' `Types` (an argument keeps
  its concrete type), so a backend must convert exactly these expressions where
  it evaluates them. For a range statement that assigns to interface
  variables, `Expr` is the key or value identifier and each element is
  converted when assigned. Interface-to-interface conversions copy the two
  words and are not listed. Explicit `I(x)` is a `*ast.CallExpr` whose `Fun`
  is a type; its argument is the listed conversion (or an interface value).
- `DynamicTypes` lists the concrete types interface values may hold or are
  compared or asserted against, sorted by `Key`; `ID` is the index plus 1 (0 is
  the nil interface). `Boxed` types are converted somewhere; `Direct` (pointer
  and map) types store the pointer (for a map, its header pointer) in the data
  word; otherwise a conversion allocates
  `(Size+7)&^7` bytes (nothing when `Size` is 0) and copies the value.
  `Comparable` is false where equality must abort. `Methods` is the type's
  method table: for each slot its method set implements, the `Func` to call (an
  `Instance.Object` for methods of generic types, which Check instantiates even
  when only dynamic calls reach them) and whether the call copies `*data`
  (`Deref`, a value method of `*T`, which traps on nil).
- `MethodSlots` are the distinct interface methods (name, package of an
  unexported name, signature) called dynamically (`Called`) or required by an
  interface that code asserts to, sorted by `Key`. `DynamicCalls` maps each
  method call on an interface value (a `*ast.CallExpr` whose selector's
  `Selections` entry is a `MethodVal` on an interface type) to its slot. A
  dynamic call evaluates its operands in [gc's order](#evaluation-order) (the
  receiver is a plain operand, read after the arguments' calls), then aborts
  if the interface is nil.
- `TypeAssertions` and `TypeSwitches` are the sites in source order. A
  comma-ok assertion's `Types` entry is a two-element tuple (go/types records
  it so); it is not a `resultsN` value. A type switch clause's variable is
  `Info.Implicits[clause]` (Check now records `Implicits`, including for
  instance bodies); it may be in `HeapVars`.
- `(*CheckedProgram).Implementers(I)` lists the boxed dynamic types that
  implement `I`.

A backend must reproduce the representation where it is observable: dynamic
type identity (types identical in Go, including `byte`/`uint8` spellings, are
one dynamic type), boxing copies and their heap bytes (the out-of-memory point),
Go's comparison order (struct fields and array elements in order, an
uncomparable identical dynamic type aborts) and the three interface traps. The
type IDs, the two-word layout and the dispatch code are the built-in backend's
choice.

## Strings

Strings ([GO_STRINGS.md](GO_STRINGS.md)) add no field to `CheckedProgram`
(additive section). They are ordinary `types.Basic` values (`types.String`,
`types.UntypedString` for constants, or a named type with that underlying) in
the typed program, and the operations are ordinary syntax: `+` and `+=`
(`*ast.AssignStmt` with `token.ADD_ASSIGN`, the only compound assignment Go
allows on strings), comparisons, `s[i]` (`*ast.IndexExpr`, a `byte`),
`s[lo:hi]` (`*ast.SliceExpr`), `len`, conversions (`*ast.CallExpr` whose `Fun`
is a type: `string`/named string from a string, a `[]byte`-like or
`[]rune`-like slice or an integer; `[]byte`/`[]rune`-like from a string),
`append(b, s...)`, `copy(b, s)`, `*ast.RangeStmt` over a string (key `int`,
value `rune`), `switch` on a string, and string constants (`TypeAndValue.Value`
of kind `constant.String`). The predeclared `error` is an ordinary interface
value (its method slot is `Error() (string)`); conversions to it are listed in
`Conversions` like any other.

A backend must reproduce what is observable:

- bytewise `==` and ordering, the bounds trap for an index or `lo <= hi <=
  len(s)`;
- Go's UTF-8 decoding for `range` and `[]rune(s)` (an invalid, overlong,
  surrogate or truncated encoding is U+FFFD with width 1) and encoding for
  `string(x)` and `string(rs)` (negative values, surrogates and values above
  U+10FFFF encode U+FFFD);
- the heap bytes of each allocating operation (the out-of-memory point) and the
  capacities of `[]byte(s)` (Go 1.22 `rawbyteslice`, or `len(c)` for a
  constant `c`) and `[]rune(s)` (`rawruneslice`), both contracted in
  [ERROR_CONTRACT.md](ERROR_CONTRACT.md#contracted-runtime-behaviour); a
  concatenation is a maximal tree of non-constant string `+` operators
  (parentheses included), its operands evaluated in
  [gc's order](#evaluation-order), allocating nothing when at most one
  operand is non-empty; `x += y` evaluates `y` and the target's operands in
  that order, then reads `x`;
- boxing a string allocates 16 bytes (`DynamicType.Size`), and `[]string`
  grows as a 16-byte pointer-holding element.

String bytes are never stack or SDK storage (conversions from slices copy), so
the borrow analysis ignores string values; `[]byte(s)` and `[]rune(s)` results
are heap storage (`ResultBorrow.Heap`). The built-in backend's C representation
is `typedef struct { u8 *ptr; i64 len; } gosvm_string;` with literals as C
string literals in `.rodata`; the layout is the backend's choice. `gosvm
features` reports `uses.strings`.

## Function values

Function values, closures and `defer` ([GO_FUNCTIONS.md](GO_FUNCTIONS.md))
are ordinary `*types.Signature` values, `*ast.FuncLit`s and `*ast.DeferStmt`s
in the typed program; Check adds what lowering them needs. All fields are
additive and empty for programs without them:

- `FuncTargets` is the **address-taken set**: every function a function
  value may hold, with `ID` (1-based; `FuncTargets[i].ID == i+1`), `Kind`
  (`TargetFunction` for a declared function or an instance's `Object`,
  `TargetLiteral`, `TargetMethodValue`, `TargetMethodExpr`), `Func` (the
  function or method called; an `Instance.Object` for generic code) or `Lit`,
  the value's `Signature`, and for methods whether the value binds a pointer
  (`PointerReceiver`) or the call dereferences a `*T` argument of a value
  method (`Deref`, which traps on nil). `FuncValues` maps each expression that
  makes a value (identifier, `pkg.F`, `F[T]`, `x.M`, `T.M`, literal) to its
  target.
- `FuncClasses` are the signatures called through values; each `Targets`
  lists every target of an identical signature (parameter names ignored, so
  a named function type and its underlying type share a class), in ID order.
  `IndirectCalls` maps each call through a value to its class, and
  `(*CheckedProgram).CallTargets(call)` returns the possible targets of a
  call: the class's targets, or the one literal for a literal called where it
  is written. This is the analogue of `DynamicCalls` and `Implementers`: a
  backend can lower the call as compare-and-branch dispatch over exactly these
  targets, aborting when none matches (a nil value). Check's recursion check
  uses the same edges, so the graph they form with direct and dynamic calls is
  acyclic.
- `Closures` lists every literal with its `Owner` function, enclosing
  literal (`Parent`), parameter and named-result objects, `Captures` (the
  variables declared outside it that its body uses, in first-use order) and
  `EnvBytes` (heap per evaluation: 0, or 8 plus 8 per capture). Captured
  variables are always in the owner's `HeapVars` (including literal parameters
  and named results captured by nested literals); the literal accesses them by
  reference. `Callees` lists its direct callees.
- `DeferSites` lists every `defer` statement with its `Kind` (function,
  static method, dynamic method, indirect, literal, `copy`), `Owner` and
  enclosing literal (`Lit`, nil for the function body), `InLoop` (each
  execution's record is then heap-allocated: `RecordBytes`), and `Saved`, the
  types the statement evaluates and saves, in order.

A backend must reproduce what is observable: [evaluation order](#evaluation-order)
(the function value is a plain operand, read after the arguments' calls; the
nil check at the call; deferred operands at the `defer` statement; a nil interface or a value method
through a nil pointer in a `defer` statement aborts there), last-in
first-out deferred calls after the results are set (named results visible to
deferred closures), deferred calls not running on a trap, Go 1.22
per-iteration loop variables, method-value binding and copy semantics, the
nil-function trap and the heap bytes of environments, method values,
captured variables and loop defer records (the out-of-memory points). The
one-word value encoding, target IDs and dispatch code are the built-in
backend's choice.

Function values and strings combine without new fields: a function type's
parameters and results may be strings or `error` (a `gosvm_string` or an
interface value in the built-in backend), closures capture string variables by
reference like any variable (16 bytes moved to the heap), and loop `defer`
records save string and interface operands at their Go size (16 bytes each).
## Compound assignment

Compound assignment on integers ([GO_INTEGERS.md](GO_INTEGERS.md#compound-assignment))
adds no field: it is an `*ast.AssignStmt` with one left and one right
operand and a `Tok` of `token.ADD_ASSIGN` … `token.AND_NOT_ASSIGN`, whose
left operand has an integer type (string `+=` is the case above). A backend
must lower `x op= y` as `x = x op y` with the target's index and pointer
operands evaluated once, in [gc's order](#evaluation-order) (after `y`'s
calls, except early map keys); the target read after `y`; the binary
operator's values and traps (division or modulo by zero, negative shift
count); and the target's traps (bounds, nil pointer, nil-map write) after
`y`. A map element is looked up once after `y`, inserted with the zero value
when absent.

## Maps

Maps ([GO_MAPS.md](GO_MAPS.md)) are ordinary `types.Map` values (or named
types with that underlying type) in the typed program, and the operations are
ordinary syntax: `make` (a `*types.Builtin` call, with an optional hint of any
integer type), `*ast.CompositeLit` of a map type (every element a
`*ast.KeyValueExpr`), `m[k]` (`*ast.IndexExpr` whose `X` has a map type; in
`v, ok := m[k]` its `Types` entry is a two-element tuple, as go/types records
comma-ok forms), `m[k]` as an assignment, `++`/`--` or `+=` target,
`delete`, `len` and `clear` builtin calls, `== nil`/`!= nil`, and conversions
between map types. `*ast.RangeStmt` over a map never reaches a backend (Check
rejects it). `struct{}` (an unnamed `*types.Struct` with no fields) is
admitted as a zero-size value. Check adds one field:

- `Maps` lists every map type checked code uses, sorted by `Key`
  (`typeKey` of the unnamed `*types.Map`, so identical map types share one
  entry); `Index` is the position. `KeySize` and `ElemSize` are Go sizes,
  `SlotSize` is `8 + roundup8(KeySize) + roundup8(ElemSize)`, and
  `KeyInterfaces` reports that the key holds interface values (directly, or
  in struct fields or array elements), so hashing it can abort.

Map keys converted to an interface key type are listed in `Conversions` like
any other conversion (the key of `m[k]`, `delete(m, k)` and a literal key, and
literal elements). In the borrow analysis, a pointer or slice stored into a
map as a key or element (by assignment or in a literal) is stored into heap
data, so a local it refers to moves to the heap; elements read from a map are
heap or SDK storage (`ResultBorrow.Heap`).

A backend must reproduce what is observable:

- Go's results for every operation: zero values for absent keys and nil maps,
  key equality as Go's `==` (strings bytewise, structs field by field, arrays
  element by element, interfaces by dynamic type and value, pointers by
  address), reference semantics, and [evaluation order](#evaluation-order)
  (the map and key are plain operands read after the value's calls, except
  keys of gc's generic map path, which are read where they appear; the store
  last; tuple assignments store left to right after all operands; `m[k] op= x`
  evaluates `x` before reading `m[k]`; map literals insert static entries
  first, `MapLiteralOrder`);
- the two traps (assignment to an entry in a nil map; hashing an interface
  key whose dynamic type is not comparable, in every operation, even on nil
  and empty maps), and that a `make` hint never traps;
- the heap accounting, which depends only on `SlotSize` and the live-key
  counts: a 24-byte header, `cap(h)` or `cap(n)` slots for a positive hint or
  a literal of `n` entries, doubling when `4 * (len + 1) > 3 * slots`, never
  shrinking or freeing ([contracted](ERROR_CONTRACT.md#contracted-runtime-behaviour)).

The table layout, probing scheme and hash function are the backend's choice:
none of them is observable without iteration. The built-in backend's C
representation is `gosvm_map *` with per-type `gosvm_map<Index>_find`,
`_set` and `_delete` helpers. A map in an interface is `Direct` (no box), and
`[]map[K]V` grows as a pointer-holding 8-byte element. `gosvm features`
reports `uses.maps`.

Maps and [function values](#function-values) combine without new fields: a
function type's parameters and results may be maps (and share a `FuncClass`
with every target of the identical signature, so the recursion check treats a
`func(map[K]V) V` like any other signature), map elements may be function
values (`map[string]func(uint64) uint64`; a lookup of an absent key yields a nil
function value, whose call aborts), closures capture map variables by
reference (the 8-byte reference moves to the heap, never the table), loop
`defer` records save a map operand as its 8-byte reference, and method values
of named map types bind the reference (a value receiver copies the
reference, so the bound value still sees later writes, as in Go).

## Package-level variables

Package-level variables and `init` functions ([GO_GLOBALS.md](GO_GLOBALS.md))
are lowered by Check into ordinary checked code. All fields are additive and
empty for programs without them:

- `Globals` lists every named package-level variable: packages in `Inits`
  order (all of the program's packages in Go's initialization order), then
  declaration order. `Var` and `Spec` are the variable and its declaration;
  `Init` its own initializer (nil for none, or when a multiple-value call
  initializes several variables). `ReadOnly` means the variable never
  changes: its value is the zero value or `Init` (a constant expression or a
  composite literal of constants of a plain-data type, a constant string, or a
  non-empty slice literal of plain-data constants), and Check proved that no
  code writes it, takes its address or lets a slice value escape. A
  read-only variable has no storage in the block and no initialization code;
  the built-in backend makes it a constant in read-only data.
- `GlobalBytes` is the size of the globals block: a C struct of the
  variables that are not read-only, in `Globals` order, each at its Go
  alignment and `Offset`, rounded up to 8. It must be the invocation heap's
  first allocation (so it is at the heap start plus 8), made before anything
  else runs; out-of-memory boundaries depend on it.
- `Inits` lists, in Go's package initialization order, each package with
  initialization work. `Vars` is a synthetic `Function` (also in `Functions`
  and `CallOrder`, with `PackageInit` set and `Object.Name()` `init.vars`)
  whose body is one `*ast.AssignStmt` (`token.ASSIGN`) per go/types
  initializer that is not read-only, in go/types' dependency order:
  `a = e`, `a, b = f()` or `_ = e`. The left-hand identifiers are synthetic
  (their `Uses` and `Types` entries are the variable; a blank one has a nil
  `Defs` entry) and the right-hand sides are the declared initializer
  expressions, with their own `Types`, conversions, closures and heap moves.
  Its function scope (`Info.Scopes[Decl.Type]`) is the package scope. `Funcs`
  are the package's `init` functions in file and source order.
- At entry, before `Process`: allocate the block when `GlobalBytes > 0`,
  then for each `Inits` entry call `Vars` (if any), then each of `Funcs`.

The lowered files keep every `var` declaration, with its initializers only
when all of the specification's variables are read-only; otherwise the
initializers appear once, in `Vars`. A package-level variable is named by an
`*ast.Ident` or, from another package, an `*ast.SelectorExpr` whose `X` is a
`*types.PkgName` identifier (`pkg.V`; a call `pkg.F(x)` of a function-valued
variable is an indirect call in `IndirectCalls`). In the borrow analysis,
storing a reference into a package-level variable stores it into heap data
(the referenced local moves to the heap), its address is heap storage, and
reading a pointer or slice from it yields heap or SDK storage. A closure
never captures a package-level variable: it uses the variable itself.

A backend must reproduce the initialization order (packages, variables,
`init` functions), that every invocation initializes afresh, that a trap
during initialization aborts before `Process`, and the block's heap
accounting. `gosvm features` reports `uses.globals`, `global_heap_bytes` and
`readonly_globals`.

A package-level variable may have an array type above the 1,024-byte
per-value limit when Check proved that no value of that type is ever copied
([large tables](GO_GLOBALS.md#large-tables)): it is then only indexed, passed
to `len`/`cap` or ranged over by index, and is either read-only (its literal
is constant data) or in the globals block without an initializer. A backend
must address such a variable in place and never materialize it in a stack
frame.

## Value types

Struct comparison, type aliases, arrays of composite elements and `panic`
([GO_TYPES.md](GO_TYPES.md), [GO_ARRAYS.md](GO_ARRAYS.md)) are ordinary syntax
in the typed program. Check adds one field:

- `Panics` maps each call of the `panic` builtin (an `*ast.CallExpr` whose
  `Fun` names `*types.Builtin` `panic`; inside instances of generic code, the
  instance `Decl`'s node) to a `PanicSite`: `Value` is `PanicText` (log
  `Text`: constants, `nil`, and values logged by static type),
  `PanicString`, `PanicSigned`, `PanicUnsigned` or `PanicBool` (log `Prefix`,
  the run-time value formatted as Go prints it, then `Suffix`) or
  `PanicInterface` (log `NilPanicLine` when the value is a nil interface, else
  `Text`). The argument is evaluated (except constants and `nil`), never
  converted to an interface: `Conversions` does not list it.

Everything else needs no new field:

- **Struct `==`, `!=` and struct `switch` tags** are `*ast.BinaryExpr` and
  `*ast.SwitchStmt` whose operands have a `*types.Struct` underlying type
  (go/types has required comparability). Compare the non-blank fields in
  declaration order, stopping at the first difference, recursively through
  arrays (index order) and structs; strings bytewise, pointers by address,
  interfaces by dynamic type and value (an uncomparable identical dynamic type
  traps). Map key equality and hashing, and interface comparisons of structs,
  also skip blank fields.
- **Type aliases** are `*ast.TypeSpec`s with `Assign` set. go/types (with its
  default `gotypesalias=0`) records every use of the alias as the aliased type,
  so an alias has no representation, no code and no `TypeInstances` entry of
  its own; methods declared through an alias receiver are the aliased type's.
- **Arrays of composite elements** (structs, arrays, pointers, slices,
  `struct{}`) are `*types.Array` values like any other. An element of
  addressable array storage (`a[i]`, `p[i]` through a pointer, nested
  `a[i][j]`) is addressable: field and element reads, stores, `op=`, `++`,
  `&`, method receivers and slicing act on it in place; the bounds (and nil)
  check runs where the element is used, after the operands of the statement
  are evaluated, as for scalar arrays. An index-only `range` over an element
  of an array of arrays evaluates nothing when it has no calls. In the borrow
  analysis an array literal's pointer or slice elements are stored into heap
  data, like struct literal fields, so a local they refer to moves to the heap
  (`Function.HeapVars`).
- **`recover`** never reaches a backend (Check rejects it), and neither does
  `defer panic(...)`.

A backend must reproduce the panic line (it is
[contracted](ERROR_CONTRACT.md#panic)), Go's comparison order and the existing
traps. The built-in backend emits the panic runtime (`gosvm_panic`,
`sol_log_`) only for programs with `Panics`, so every other program's C is
unchanged; arrays of composite elements are tagged C structs defined after
their element types, and arrays of the earlier element kinds keep their
earlier C.

## Standard-library packages

The standard-library packages of [GO_STDLIB.md](GO_STDLIB.md) are ordinary
packages of the typed program: `Packages` has them by their import paths
(`errors`, `strings`, ..., and the overlay's `internal/bytealg` and
`internal/errtext`), `Files` has their sources (positions in
`gosvm-stdlib/go1.22.0/<path>/<file>.go`), and their functions are in
`Functions` and `CallOrder` like user code. `compiler.ProgramStdlib` lists the
ones a program reaches and `compiler.StdlibIdentity` hashes them. A backend
lowers them like user code, plus:

- **Intrinsics.** A function with `Intrinsic` set has a Go body that is
  checked but replaced: `"comparable"` (`func(x any) bool`: whether x's
  dynamic type is comparable, `DynamicType.Comparable`; the program's
  uncomparable dynamic types are false), `"unsafeString"` (`func(b []byte)
  string`: a string sharing b's bytes, with no allocation) and `"noescape"`
  (`func(p *T) *T`: returns p; its Go body returns nil so that Check's borrow
  summary refers to no storage, which is what the overlay relies on: the
  result is only compared).
- **Restricted dispatch.** A `MethodSlot` with `ExceptImplementers` set (an
  interface type) never runs the methods of the dynamic types implementing
  it; `Except` lists them, and they do not fill the slot. Its dispatcher
  aborts for them as for a nil interface (they never reach it). `CallOrder`
  and the recursion check use these restricted target sets, which is what
  keeps `(*errors.joinError).Error` and `(*strconv.NumError).Error` acyclic.
- **Directives** (`//gosvm:intrinsic`, `//gosvm:dispatch-except`) are honoured
  only in overlay files; a backend reads the resulting fields, not comments.

## Variadic functions

Declared variadic functions and methods ([GO_FUNCTIONS.md](GO_FUNCTIONS.md#variadic-functions))
reach a backend with every call already passing the final parameter as a
slice: Check rewrites, in place in the checked syntax (`Files` and the lowered
files), each call that does not end in `...` into one that does, with a
synthetic `*ast.CompositeLit` of the parameter's slice type (no `Type`
expression; `Info.Types` has its type) holding the extra arguments, or a
synthetic `nil` identifier (`Info.Uses` is the universe `nil`) when there are
none, and `Ellipsis` set. The literal is an ordinary slice literal for
interface conversions, escape analysis (`HeapLiterals` never lists it: slice
literals are always heap) and emission. Calls into `gosvm/solana` are not
rewritten.

## Pointers to pointers

`*types.Pointer` values whose element is a pointer
([GO_POINTERS.md](GO_POINTERS.md#pointers-to-pointers)) need no new field. In
the borrow analysis a pointer variable whose address is taken is treated
like heap storage (references assigned to it are stores into heap data;
reading it yields heap or SDK storage), so `Function.HeapVars` and
`BorrowSummary.Escapes` already account for references reached through a
`**T`.

## Aggregate parameters

**Backends must give every callee its own copy of each aggregate parameter**
(string, slice, interface, struct, array; receivers included), as Go does:
neither the callee's writes to its parameter may reach the caller's
variable, nor may the caller's variable changing during the call (through a
package variable, a pointer, or a call) change the parameter. Clang's SBF
lowering of C `byval` parameters breaks both: it passes the caller's own
memory and makes no copy, and LLVM forwards copies into such arguments. The
built-in backend therefore copies, in every function and literal, each
aggregate parameter the body writes, and (through an opaque pointer) each one
it may read after a write that can change the caller's variable, into a
local at entry ([GO_STDLIB.md](GO_STDLIB.md#aggregate-parameters-on-sbf),
`internal/compiler/byval.go`). This is a lowering matter, not a field of
`CheckedProgram`: a backend whose calling convention copies aggregates, or
that never shares a caller's memory with a callee, needs nothing; one that
passes aggregates by reference must copy (or prove the copy unobservable).

## Evaluation order

goSVM evaluates expressions in gc's order, contracted in
[ERROR_CONTRACT.md](ERROR_CONTRACT.md#evaluation-order): within each
*owner* (a simple statement, a root expression, a sequenced operation, an
operand of `&&`/`||`, a dynamic map-literal entry), the sequenced operations
(calls, `len`/`cap`/`append`/`copy`/`make`/`new`, `&&`/`||`, slice
expressions, string-to-slice conversions, type assertions to non-pointer
shapes, dynamic map literals, and gc's early-copied conversion operands and
map keys) run first, left to right, each by the same rule; then the plain
operands are read and the plain operations (and their traps) performed, left
to right.

**Where the decision lives.** Check makes it and records it in
`CheckedProgram`; the C emitter applies no rule of its own:

- `Sequenced(e)` reports whether `e` is a sequenced operation. Together with
  the contract's owner rule it determines the complete order of any checked
  program, so a backend that hoists every sequenced operation of every owner
  into a temporary, left to right, then reads the rest, is exact.
- `Hoisted` maps each owner whose left-to-right evaluation would observably
  differ from that order to the sequenced operations (in source order) to
  evaluate first. "Observably" is a sound, conservative analysis: a plain
  operation before a sequenced one in source order reads storage the
  sequenced one may write (a package variable; a local whose address is taken
  or that a closure captures; anything through a pointer, slice or map), or
  may trap while the sequenced one may log (a `panic`, a CPI or return data,
  an unknown function value or interface method). Calls are summarized
  transitively: a function that assigns only its own locals writes nothing,
  and one that never reaches `panic`, `Invoke`, `Transfer`,
  `TransferSigned`, `SetReturnData` or a dynamic call never logs. Everywhere
  else evaluating left to right is gc's order, which is what keeps programs
  without such hazards (the full swap included) byte-identical.
- `HoistedConversions` lists hoisted generic-path map keys whose interface
  conversion belongs to the early evaluation.
- `MapLiteralOrder` gives a map literal's insertion order when a static
  entry follows a dynamic one (static entries first).
- `SharedOperands` maps the map operand or key of the first `append`
  argument in `m[k] = append(m[k], ...)` to the target's operand whose early
  value it reuses; the statement is then in `Hoisted`.

The built-in emitter evaluates each `Hoisted` owner's list first (each
expression with its own `Hoisted` entry, if any), saves the values, and then
emits the owner left to right using them; every saved value must be used
exactly where its expression appears (an unused one is an internal error).
Keeping the order as data rather than rewriting the syntax keeps
`CheckedProgram`'s typed syntax unchanged for every analysis (escape
analysis, closures, interfaces) and for backends. A backend may apply the
rule only at `Hoisted` owners, as the emitter does, or everywhere.

## `project.LoadSchema`

`func LoadSchema(dir string) (*ResolvedSchema, error)` in
[`internal/project/schema_load.go`](../internal/project/schema_load.go). It
loads and validates a schema-2 `gosvm.json` and resolves layouts with the same
code `gosvm generate` uses, including the `layouts.json` compatibility lock. It
writes nothing. `ResolvedSchema` holds:

- the validated `Config`;
- `States` and `Arguments`, as `WireLayout`s with discriminator, size and packed
  fields (absolute offsets and sizes, nested structs, array lengths, `GoType`);
- `Instructions`, with accounts in transaction order (kind, access, signer,
  owner, address, layout, PDA seeds, init/close), key relations and aliases;
- `Errors` (generated 6000+ codes and handler `Err*` constants);
- `ValidationOrder`;
- the `TypedProgram` the layouts were resolved in.

`State(name)` looks up a state layout by name. For schema details see
[SCHEMA2.md](SCHEMA2.md). LoadSchema certifies the declarations, not the handler
code. Run `Check` on the generated program (`gosvm check` does) for that.

## Failure semantics

Runtime traps, error codes and check order are a stable contract:
[ERROR_CONTRACT.md](ERROR_CONTRACT.md). A backend must reproduce them. The
compile-time diagnostics above are a separate matter. They come only from
`Check`, so they are identical for every backend.

## Equivalence evidence

[`internal/compiler/check_corpus_test.go`](../internal/compiler/check_corpus_test.go)
replays 312 programs recorded from the pre-split compiler (commit `dfcb8fc`) in
[`testdata/check_corpus.jsonl.gz`](../internal/compiler/testdata/check_corpus.jsonl.gz):

- every program the root test suite passed to `CompileProgram`;
- every program under `examples/` that declares `Process`, through `-emit-c`,
  and every example project through `gosvm check`;
- a supplementary table of rejections not otherwise exercised.

For each program, `Check` must reject with exactly the recorded error, or accept
when the old compiler accepted. `CompileProgram` must reproduce the recorded error
or the SHA-256 of the emitted C. The recording is never edited: programs whose
verdict changed on purpose since are listed with their new expected outcome in
the test's `corpusChanges` table. These are: range loops and `uint16` became
supported; signed integers, `int` and `uint` became supported; the two recorded
generic programs are now accepted; interface values were first rejected with a
message naming the constraint rule, and became supported, so two recorded
interface programs compile and `error` keeps its position with a message
naming strings; the array-element and pointer-element messages now
name the admitted types; and allocation turned 32 recorded rejections (escaping
locals, `copy`, `make`, slice literals, `[]uint64`, non-byte views and
pointer/slice fields) into accepted programs. Two of those (a `[]byte` and a
`*uint64` struct field) had had their message reworded by signed integers; the
merged compiler accepts them with the C the allocation change recorded. Every
changed entry's outcome equals the outcome under the one feature that explains
it. `CheckedProgram` did not change shape for range loops, `uint16` or signed
integers. A backend must lower `*ast.RangeStmt` as described in
[GO_CONTROL.md](GO_CONTROL.md#range-loops), `uint16` as wrapping 16-bit
arithmetic, and signed integers as in [GO_INTEGERS.md](GO_INTEGERS.md).
Generics added the `Instances`, `TypeInstances` and `Instantiated` fields and
`Function.Instance`; allocation added the `HeapVars`, `HeapLiterals`, `Escapes`
and `Heap` fields above; interfaces added `Conversions`, `DynamicTypes`,
`MethodSlots`, `DynamicCalls`, `TypeAssertions`, `TypeSwitches` and
`Implementers`; function values added `FuncTargets`, `FuncValues`,
`FuncClasses`, `IndirectCalls`, `Closures`, `DeferSites` and `CallTargets`;
maps added `Maps`. Strings and `error` added no field. Existing fields keep their meaning, and the
new ones are empty or false for every program that compiled before. Strings
and `error` turned three recorded entries into new outcomes (the `error` result
and `[]byte("abc")` compile, and the `fmt` import names `math/bits`). Function
values, closures, `defer` and named results turned 12 recorded rejections into
accepted programs, and two rejections are now reached first by a new rule
(a call through a `Context` field reports that `Context` fields are opaque; an
SDK function used as a value reports that `gosvm/solana` functions cannot be
values). Maps turned the corpus's one map program (`var m map[uint64]uint64;
_ = m`) into a compiled program; no entry is affected by both maps and
function values, so the merged corpus requires 82 rejections (83 with function
values alone, less the map entry). Compound assignment on integers added no
field and turned the corpus's two compound assignments (`x <<= 2` and a
byte-slice `+=` in a loop) into compiled programs, so the corpus now requires
80 rejections. Value types (struct comparison, type aliases, arrays of
composite elements and `panic`) added `Panics` and turned nine recorded
rejections into compiled programs (five arrays of structs, arrays, slices or
pointers, two struct `switch` tags, one struct `==` and one unused alias); the
array of `Context` keeps its position with the message `arrays of Context
unsupported`, and no other entry's C changed, so the corpus now requires 70
rejections (71 remain). Separately, the
full-swap ELFs built with
`gosvm build -no-cache` were identical before and after the split: fogo-v0
`4697a9f5…`, solana-v3 `2b5f21d9…`, and target-less v1.51 `4e96d2bb…`.
The [aggregate-parameter fix](#aggregate-parameters) (October 15) changed
them on purpose; the current reference is fogo-v0 `a66c5a97…`, solana-v3
`b419245b…` and target-less v1.51 `05f52dd2…`
([results](../results/compiler/2026-10-15-byval-rebuild/README.md)), and the
corpus lists the 111 entries whose C it changed in `corpusChanges`.
