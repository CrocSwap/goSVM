# Function values, closures and defer on-chain

On-chain Go code can use function values with Go's semantics: functions,
generic instances, method values and method expressions as values; function
literals that capture variables by reference; calls through values; `defer`;
and named results. A call through a nil function value aborts, where Go
panics. Closure environments and method values live on the invocation heap
([allocation](GO_ALLOCATION.md)), at a fixed, documented cost.

```go
type Pool struct{ fees uint64 }

func (p *Pool) Charge(v uint64) uint64 { p.fees = p.fees + v/100; return v - v/100 }

func Apply(xs []uint64, f func(uint64) uint64) (total uint64) {
	defer func() { total = total + 1 }() // runs after the result is set
	for _, x := range xs {
		total = total + f(x)
	}
	return total
}

func Process(s, i []byte) uint64 {
	p := &Pool{}
	scale := uint64(i[0])
	double := func(v uint64) uint64 { return v * scale } // captures scale
	a := Apply([]uint64{1, 2, 3}, double)
	b := Apply([]uint64{400}, p.Charge) // a method value binds p
	return a + b + p.fees
}
```

Programs that use none of this compile to byte-identical C and ELFs: the
function-value runtime is emitted only when a program has a function value,
a function type or a `defer` statement. Named results alone need no runtime.

## Supported

| Form | Semantics |
| --- | --- |
| Function types | `func(...)...` and named function types (`type Op func(a, b uint64) uint64`) as locals, parameters, results, struct fields, slice and array elements, type arguments and interface contents. Parameters and results must be storable values; variadic function types are rejected |
| Function values | A declared function (`f := add`, `pkg.F`), a generic instance (`Max[int8]`, or inferred: `var f func(int64, int64) int64 = Max`), a literal, a method value or a method expression. Conversions between identical function types (`Op(add)`, `(func())(nil)`) copy the value |
| Comparison | Only with `nil` (`f == nil`, `f != nil`), as Go allows. Comparing two interfaces holding function values of the same dynamic type aborts (Go panics: uncomparable type) |
| Calls through values | `f(x)`, `s.f(x)`, `fs[i](x)`, `g()(x)`, `func(...){...}(x)`. The function value is evaluated, then the arguments, then the call; a nil value aborts after the arguments are evaluated |
| Closures | Literals capture variables by reference: a change made through one closure is seen by the enclosing function and by every other closure of the same variable. Nested literals capture through the enclosing literal. Captured parameters, receivers, named results and loop variables are all captured by reference |
| Loop variables | Go 1.22: every iteration of a three-clause or `range` loop has its own variables, so each iteration's closure (or `&i`) sees its own `i`. A three-clause loop variable that a closure captures (or whose address escapes) is copied to a fresh variable before each post statement, as the Go spec describes; a change made by a closure during an iteration carries into the next iteration's copy |
| Method values | `x.M` binds the receiver when the value is made: a value-receiver method binds a copy (through a pointer `p.M`, `*p` is copied then, and a nil `p` aborts then), and each call gets a fresh copy of the bound value; a pointer-receiver method binds the pointer (`&x` for an addressable `x`), so later changes to `x` are seen and a nil pointer aborts only when the method dereferences it. Methods of generic types and named scalars included |
| Method expressions | `T.M` (receiver as first argument), `(*T).M` for value and pointer receivers; `(*T).M` of a value method dereferences its argument when called (nil aborts then) |
| `defer` | Calls of functions, static and dynamic methods, function values, literals and the `copy` builtin. The function value (or receiver) and the arguments are evaluated at the `defer` statement; deferred calls run when the function returns, last-in first-out, after the result values are set; deferred closures can read and change named results. A nil deferred function value aborts when the function returns; a nil interface (or a value method through a nil pointer) in a `defer` statement aborts at the statement, after the arguments, exactly when Go panics |
| `defer` in loops | Supported: each execution pushes its own record, so `n` iterations run `n` deferred calls. A record in a loop lives on the heap (see [heap cost](#heap-cost)); outside loops it lives in the function's frame |
| Named results | `func f() (a, b uint64)`, bare `return`, and `return x, y` (evaluated, then assigned to the results, then deferred calls run). Named results start at their zero values |
| Generics | Generic functions take and return function values (`Map[T, U any](xs []T, f func(T) U) []U`); function types are valid type arguments; literals inside generic functions are separate closures per instance; generic instances and methods of generic types are function values |

Rejected, with source-located messages:

- `recover` (`recover unsupported: a panic aborts the transaction and rolls
  back its effects, so there is nothing to recover`) and `defer panic(v)`
  (`defer panic(...) unsupported; defer a function literal that panics`);
  `go` statements. `panic` itself is supported in functions, closures and
  deferred calls ([panic](GO_TYPES.md#panic)): it logs one line and aborts.
- Method values and method expressions of interface types (`x.M` with `x` an
  interface, `I.M`): `method values of interface types unsupported; use a
  function literal that calls the method`.
- `gosvm/solana` and `math/bits` functions as values or deferred:
  `<path> functions cannot be used as values; call them in a function
  literal`, `defer of <path> calls unsupported; defer a function literal that
  makes the call`. They have no Go bodies.
- Pointers to function values (`&f`), variadic function types and literals
  (variadic declared functions and methods are supported,
  [below](#variadic-functions)), function types with unsupported parameter
  types, method values whose receiver contains zero-length arrays or empty
  structs.
- Recursion through function values ([below](#recursion)).

There is no `recover`: a trap aborts the transaction without running deferred
calls. On chain this is equivalent to Go's unwinding: the failed instruction's
effects are rolled back, so nothing a deferred call would have done can be
observed.

## With strings and `error`

Function values and [strings](GO_STRINGS.md) combine with no extra rules.
Function types may take and return strings, named strings and `error`
(`func(string) (string, error)`, `type Step func(string) (string, error)`);
closures capture string variables by reference like any variable (the
16-byte header moves to the heap; the bytes stay where they are, in read-only
data or the heap, and are never copied by a capture); `defer` saves string
and `error` operands at the statement (16 bytes each in a loop record);
deferred closures can change named `string` and `error` results; method values
and method expressions of types with `Error() string` (or other string
methods) bind like any method value; a named function type can implement
`error` (`type ErrFunc func() string` with an `Error` method); and function
values sit in interfaces next to errors. `err.Error` of an `error` value is a
method value of an interface type and is rejected; write
`func() string { return err.Error() }`. The recursion check treats string- and
`error`-typed signatures like any other: a literal of type
`func(string) string` that calls a `func(string) string` value is rejected,
and a cycle through a dynamic `Error()` call and a function value is found.

Heap costs add up as listed below and in
[string allocation](GO_STRINGS.md#allocation): a closure capturing one string
declared in a loop costs 16 (the moved header) + 16 (the environment) per
iteration, a value-receiver method value of a named string 8 + 16, and a loop
`defer f(s)` record 16 + 16. `TestClosureStringHeap` checks these
out-of-memory points exactly, `TestFuncStringsDifferential` compares
[`examples/func-strings`](../examples/func-strings/program.go) natively and as
host-compiled C, and `TestFuncCombinationsSBF` runs it on fogo-v0 and
solana-v3 ([with maps](#with-maps)).

## With maps

Function values and [maps](GO_MAPS.md) combine with no extra rules. Function
types may take and return maps (`func(map[string]uint64) uint64`); maps may
hold function values (`map[string]Op`, `map[uint8][]func() uint64`, structs
with function fields), and a lookup of an absent key yields a nil function
value, whose call aborts after its arguments are evaluated; closures capture
map variables by reference (the 8-byte reference moves to the heap, the
table is never copied, and assigning a new map to the captured variable is
seen everywhere); `defer` saves a map operand as its reference, so a deferred
call sees writes made after the `defer` statement, and deferred closures can
change named map results; method values of map-backed named types bind the
map reference (`c.Inc` with `type Counter map[string]uint64`: reassigning `c`
later does not change the bound map, writes through it do), pointer-receiver
method values bind `&c`, and method values of generic map types work alike.
Function values are not comparable, so a map keyed by `any` aborts when a
function value is used as a key (Go panics: hash of unhashable type), and a
nil-map write inside a closure or deferred call aborts where Go panics.

The recursion check treats map-typed signatures like any other: a literal of
type `func(map[K]V) V` that calls a value of the identical signature, or a
function value stored in a map that looks itself up and calls itself, is
rejected; `func(map[uint8]uint32)` and `func(map[uint8]uint64)`, or a named
map type and its underlying type, are different signatures.

Heap costs add up: a closure capturing a map declared in a loop costs 8 (the
moved reference) + 24 (the header of `make`) + 16 (the environment) per
iteration, a closure capturing an outer map 16, a loop `defer f(m, k)` record
16 + 8 + 8, and a value-receiver method value of a map type 8 + 8.
`TestClosureMapHeap` checks these out-of-memory points exactly,
`TestFuncMapsDifferential` compares
[`examples/func-maps`](../examples/func-maps/program.go) natively and as
host-compiled C (9,060 vectors, 2,487 Go panics), and
`TestFuncCombinationsSBF` runs it and
[`examples/func-strings`](../examples/func-strings/program.go) on fogo-v0
and solana-v3 through the pinned runners
([results](../results/compiler/2026-10-14-func-maps/README.md)).

## Variadic functions

Declared functions and methods may have a final `...T` parameter, which is a
`[]T` in the body, with Go's semantics (added for `errors.Join`,
[standard library](GO_STDLIB.md)):

```go
func sum(base uint64, xs ...uint64) uint64 {
	for _, x := range xs {
		base += x
	}
	return base
}

sum(1)          // xs == nil
sum(1, 2, 3)    // xs is a new []uint64{2, 3}: len == cap == 2
sum(1, s...)    // xs is s itself: writes to xs[i] change s
```

Generic variadic functions (`func n[T any](xs ...T) int`), variadic methods,
deferred variadic calls, interface-typed variadic parameters (`errs
...error`, each argument converted) and variadic calls in package
initializers work. Check lowers every call that does not end in `...` into one
that does: the extra arguments become a slice literal (`[]T{x, y}...`), or
`nil...` when there are none, evaluated in Go's order. The slice is allocated
like any slice literal ([allocation](GO_ALLOCATION.md)): `k > 0` extra
arguments allocate `k * sizeof(T)` rounded up to 8 bytes; no extra argument
and a spread slice allocate nothing (gc stack-allocates a non-escaping
argument slice, so it can use less heap). A variadic function cannot be used
as a function value (`variadic function types are unsupported`), and
variadic function literals and interface methods stay unsupported.

## Representation

A function value is one 64-bit word, Go's size (C: `typedef u64 gosvm_func;`):

- `0` is nil.
- A value below 2^16 is the ID of a target that needs no environment: a
  declared function or instance, a method expression, or a literal without
  captures. Making such a value allocates nothing.
- Any other value points at a heap object whose first word is the target ID:
  for a closure, the captured variables' addresses follow (one word each, in
  first-use order); for a method value, the bound receiver pointer
  (pointer-receiver methods) or a copy of the receiver.

Targets are the program's **address-taken set** (`CheckedProgram.FuncTargets`):
every declared function, instance, method value, method expression and literal
that is used as a value. IDs are 1-based in discovery order (functions in
`Functions` order, then source order); the same function used at several sites
is one target, and each literal is its own.

### Dispatch

Each signature that the program calls through a value has one dispatcher
(`CheckedProgram.FuncClasses`). It reads the target ID (the value itself, or
the first word of its object), compares it with each target of an identical
signature, in ID order, and calls that target; nil (or no match) aborts:

```c
static u64 gosvm_fcall0(gosvm_func f,u64 p0) { /* func(uint64) uint64 */
u64 id = GOSVM_FUNC_ID(f);  /* f <= 0xffff ? f : *(u64 *)f */
if (id == 3ULL) return v12(p0);                         /* a declared function */
if (id == 5ULL) return gosvm_lit5(f,p0);                /* a closure: reads its captures from f */
if (id == 6ULL) return v20(*(v7 *)((u8 *)f + 8),p0);    /* a value-receiver method value */
abort();
}
```

A literal called where it is written (`func(){...}()`, `defer func(){...}()`)
calls its function directly. Function types are identical, and so share a
dispatcher, when their parameter and result types are identical, whatever their
names (Go's assignability lets a value of `Op` flow into a `func(a, b uint64)
uint64` variable).

**Why compare-and-branch rather than C function pointers.** SBF supports
indirect calls on both profiles: Clang emits `callx` for C function pointers,
with the function's address loaded by `lddw` and an `R_SBF_64_RELATIVE`
dynamic relocation on fogo-v0 (SBPFv0) or an absolute address with no
relocation on solana-v3 (SBPFv3). An experiment built a program calling
through a `volatile` C function pointer, and through a `const` table of
function pointers, with the pinned platform-tools v1.57 and ran it through the
pinned runners: both variants run correctly on both targets, so indirect calls
are not problematic as such. goSVM still uses compare-and-branch dispatch,
because it is the same code on both profiles with no relocations in code or
data (like [interface dispatch](GO_INTERFACES.md#dispatch)), the possible
callees of every call are a static, checked set (which the recursion check
needs anyway), and it was cheaper in the same experiment: 111/109 CU per
transaction for the dispatcher against 117/115 CU with a function pointer and
120/118 CU with a table, on both targets
([results](../results/compiler/2026-10-14-func-values/README.md)). The cost of
a dispatch grows with the number of targets of the called signature.

## Heap cost

All of the following follow the [allocator contract](GO_ALLOCATION.md#allocator):
each allocation is rounded up to 8 bytes, and one that does not fit aborts.

| Operation | Heap bytes |
| --- | --- |
| A declared function, instance, method expression or literal without captures as a value | 0 |
| Evaluating a literal with `n ≥ 1` captured variables | `8 + 8n` (every evaluation: each closure has its own environment) |
| A method value of a pointer-receiver method | 16 |
| A method value of a value-receiver method with receiver type `T` | `8 + ((sizeof(T) + 7) &^ 7)`; 8 for a zero-size `T` |
| A captured variable | Its storage moves to the heap ([escape rules](GO_ALLOCATION.md#escape-rules)): its size rounded up to 8, allocated where it is declared (afresh in every loop iteration; parameters, receivers and named results at function entry). A captured three-clause loop variable is also copied to a fresh allocation before each post statement |
| A `defer` statement outside loops | 0 (its record is in the function's frame) |
| A `defer` statement inside a `for` or `range` loop of its body | Per execution: a 16-byte header plus the saved values (function value or receiver, then arguments) laid out as a C struct, rounded up to 8 (`DeferSite.RecordBytes`): `defer sink(v)` with a `uint64` is 24 bytes |
| Calls through values, conversions between function types, comparisons with nil | 0 |
| Converting a function value to an interface | 8 (the boxed word; [boxing](GO_INTERFACES.md#boxing-cost)) |

Captured variables are always moved to the heap, even when Go's escape
analysis would keep a variable that is never changed after capture on the
stack: that costs heap budget, never semantics. With the default 32 KiB heap
(32,760 usable bytes) and 48 bytes moved before the loop, 2,044 closures
capturing one variable fit and the 2,045th aborts; 1,022 capturing three; 1,022
value-receiver method values of a 24-byte struct; 2,044 pointer-receiver method
values; 1,363 deferred calls with one `uint64` argument in a loop
(`TestClosureEnvironmentHeap`).

## Recursion

The subset bans recursion: SBF has a fixed call depth and stack frames, and a
program with no cycle in its call graph has a static bound on both. Check
builds the call graph over declared functions, instances **and function
literals** (each literal is a node of its own, whose calls are its body's,
not its enclosing function's) and rejects any cycle with `recursion is not
supported`. The edges are:

- direct calls of functions and methods;
- dynamic interface calls: every method that fills the called slot;
- calls through function values: **every target of an identical signature**
  (the address-taken set of that signature), whatever value actually reaches
  the call; a method value or method expression target runs its method;
- a literal called where it is written: that literal.

This resolution is sound: a value of a function type can only hold a target
of an identical signature, and every target is listed, so the graph contains
every call that can happen and an acyclic graph proves that no execution
recurses. It is conservative: a function or literal that calls a value of its
own signature is rejected even when no cycle can form at run time, for
example a wrapper `w := func(x uint64) uint64 { return inc(x) }` with `inc` of
the same type, or a pipeline method `Run(x T) T` whose method value is itself a
`func(T) T`. Such a program can usually be accepted by giving the wrapper a
different signature (see `compose` in
[`examples/func-values`](../examples/func-values/program.go)).

Every possibly-recursive program is rejected outright, so the recursion check
of a checked program is never inconclusive: `gosvm features` reports
`uses.recursion_check_inconclusive: false` for every program, and a backend can
rely on the acyclic graph (`CheckedProgram.CallTargets` lists each call's
possible targets). Deferred calls are calls of the function that defers them
and are part of its edges. A deferred call cannot add frames beyond the graph's
bound: it runs from the function's own frame.

## Traps

Function values add one abort to the [failure contract](ERROR_CONTRACT.md),
where Go panics:

- **Nil function call**: calling a nil function value, after the function
  value and the arguments are evaluated; a nil deferred function value aborts
  when the deferring function returns, after its body (Go's timing).

A nil interface in a `defer` statement, a value-receiver method value or
deferred call through a nil pointer, out-of-memory for closure environments,
method values and loop defer records, and comparing interfaces holding function
values use the existing nil-interface, nil-pointer, out-of-memory and
uncomparable-comparison traps. A trap aborts without running deferred calls;
the transaction rolls back.

## Backends and reports

`CheckedProgram` lists the address-taken set (`FuncTargets`), the expression
making each value (`FuncValues`), the signatures called through values with
their possible targets (`FuncClasses`, `IndirectCalls`, `CallTargets(call)`),
every literal with its captured variables and environment size (`Closures`),
and every `defer` statement with its kind, loop placement and saved types
(`DeferSites`). Captured variables are in their function's `HeapVars`. See
[the backend interface](BACKEND_INTERFACE.md#function-values). `gosvm features`
counts `function_literals`, `function_values`, `method_values`,
`method_expressions`, `indirect_calls`, `defer` and `named_results`, and sets
`uses.closures`, `uses.defer` and `uses.recursion_check_inconclusive`
([feature report](FEATURES_REPORT.md)).

## Evidence

- [`examples/func-values`](../examples/func-values/program.go) is an
  ordinary-Go oracle: shared captures, counters, nested closures, captured
  structs, arrays, slices, pointers, parameters and receivers; Go 1.22 loop
  variables (three-clause and `range`, closures changing the loop variable,
  per-iteration `&i`); method values (binding time, copies, nil pointers,
  generic types, named scalars) and method expressions; function values in
  fields, slices, arrays and interfaces (assertions, type switches,
  uncomparable comparisons); nil function calls after argument side effects;
  `defer` ordering, argument evaluation time, receivers, dynamic and indirect
  deferred calls, deferred closures changing named results, `defer` in loops,
  `defer copy`, early returns, nested literals with their own defers, nil
  deferred values and interfaces, and panics with pending defers; generics with
  function values; and closure-environment heap counting.
  `TestFuncValuesDifferential` compares 13,932 vectors (4,463 Go panics, each
  an abort at the same point) natively and as host-compiled C with the default
  32 KiB heap; `TestClosureEnvironmentHeap` checks the exact out-of-memory
  points above.
- `TestFuncValuesSBF` ([results](../results/compiler/2026-10-14-func-values/README.md))
  runs 336 cases on fogo-v0 and solana-v3 through the pinned runners: 324
  native-Go vectors (100 aborts with rollback) and 12 heap boundaries (7 fit or
  allocate nothing, 5 run out of memory and roll back), and measures calls: a
  call through a function value costs about 4 CU more than the direct call
  (50.11 vs 46.11 CU per iteration), a closure call about 6 CU (52.1).
- `TestFuncAcceptedForms`, `TestFuncRejections` (exact messages, including
  recursion through function values), `TestCheckedProgramFunctions` and
  `TestFuncRuntimeOnlyWhenUsed` cover forms, diagnostics, the backend view and
  byte identity; `internal/features` tests the counters and flags.
- Programs without function values emit byte-identical C: the frozen corpus
  matches except for 14 recorded function-value, closure, `defer` and
  named-result programs (12 now compile; 2 are rejected earlier by a new
  rule), and the full-swap ELFs are unchanged (fogo-v0 `4697a9f5…`, solana-v3
  `2b5f21d9…`).

The local runners are not a public network; see
[target profiles](TARGET_PROFILES.md).
