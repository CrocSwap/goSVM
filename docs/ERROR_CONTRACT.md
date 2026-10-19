# Error and failure contract

Stable from October 14, 2026, at the request of the Qushta verified backend,
which reproduces this behaviour byte for byte. Everything listed here is a
contract. To change any of it, announce the change to Qushta before landing it
and record it in the changelog at the end. Adding a new code in an unused number
does not change the contract; changing the meaning, number or check order of an
existing code does, and so does turning a trap into a code or a code into a trap.

## Traps

These failures call the VM `abort` syscall: a static syscall hash on v3, the
legacy `abort` import on v0 (`internal/compiler/compiler.go`). The instruction
fails with no custom error code and the transaction rolls back.

- Division or modulo by zero, for every integer type, signed or unsigned.
- A negative shift count (a signed count below zero), where Go panics
  ([integers](GO_INTEGERS.md)).
- Array index, slice index or slice-expression bounds out of range, including
  a string index or string slice bounds ([strings](GO_STRINGS.md)).
- Nil pointer dereference, field access or method receiver, including a
  value-receiver method called through a nil `*T` held in an interface.
- Account index out of range in SDK account accessors.
- `math/bits.Div64(hi, lo, y)`, `Div32` and `Div` with `y == 0` or `y <= hi`,
  and `Rem64`/`Rem32`/`Rem` with `y == 0`, where Go panics
  ([math/bits](GO_BITS.md)).
- Out of memory: an allocation (`new`, `make`, a growing `append`, a slice
  literal, an escaping `&T{...}`, a local moved to the heap, a conversion to
  an interface, a string concatenation or conversion, or a map header, map
  literal or map growth) needs more than the remaining invocation heap budget
  ([allocation](GO_ALLOCATION.md)).
- `make` length or capacity out of range: `len > cap`, a negative length or
  capacity of any signed integer type, or a capacity larger than 2^48 bytes,
  where Go panics `makeslice: len out of range` or `cap out of range`.
- A failed single-result type assertion `x.(T)` or `x.(I)` (the dynamic type
  is not `T`, does not implement `I`, or `x` is nil), where Go panics
  ([interfaces](GO_INTERFACES.md#traps)).
- Comparing two interface values (with `==`, `!=` or a `switch`, directly or
  as elements of arrays or fields of structs) whose identical dynamic type is
  not comparable, where Go panics `comparing uncomparable type`.
- Calling a method on a nil interface value, after the receiver and the
  arguments are evaluated (Go's nil pointer dereference).
- Calling a nil function value, after the function value and the arguments
  are evaluated; a nil deferred function value aborts when the deferring
  function returns, after its body, where Go panics
  ([function values](GO_FUNCTIONS.md#traps)). A nil interface, or a value
  method through a nil pointer, in a `defer` statement aborts at the statement
  after its arguments are evaluated (the existing nil-interface and
  nil-pointer traps), as Go panics there.
- Assignment to an entry in a nil map (`m[k] = v`, `m[k]++`, `m[k] += s`),
  after the operands are evaluated, where Go panics `assignment to entry in nil
  map` ([maps](GO_MAPS.md#traps)).
- Hashing a map key that is, or contains, an interface value whose dynamic type
  is not comparable, in any map operation (including lookups and deletes on
  nil and empty maps), where Go panics `hash of unhashable type`.
- A call of the `panic` builtin, after its argument is evaluated and one log
  line is written ([below](#panic)). This is the only trap that logs.

A trap while package-level variables are initialized or an `init` function
runs aborts the same way, before `Process` runs
([package-level variables](GO_GLOBALS.md#traps)); a `panic` call there
writes its log line first, like any other panic.

A trap aborts without running pending deferred calls (there is no `recover`);
the instruction's effects roll back, so this is indistinguishable on chain
from Go's unwinding. This includes a deferred call that would have changed a
map: its change never happens, and the rollback leaves no trace of the
function's earlier map writes either.

### Panic

`panic(v)` ([GO_TYPES.md](GO_TYPES.md#panic)) is a trap: after `v` is
evaluated, it calls the VM `sol_log_` syscall once (static hash `0x207559bd`
on v3, the legacy `sol_log_` import on v0) and then the `abort` syscall. The
instruction fails with `ProgramFailedToComplete`, like every trap, and the
transaction log contains the line `Program log: <line>`. There is no custom
error code: the SVM cannot unwind a call stack to return one from the
entrypoint, so the line identifies the panic. Deferred calls do not run and
`recover` is rejected at compile time (the transaction fails either way).

The line is `panic: ` followed by:

1. for `panic(nil)`, or an interface value that is nil:
   `panic called with nil argument`;
2. for an argument of a string, integer or `bool` type with no `Error` or
   `String` method in its method set: the value as Go 1.22 prints it (the
   string's bytes, the integer in decimal with a leading `-` when negative,
   `true` or `false`); for a named type, wrapped as `T(value)`, or `T("value")`
   for a string type, where `T` is the package name, `.` and the type name
   (with type arguments for an instance of a generic type), as Go's runtime
   spells it;
3. for any other argument: `(` + its static type spelled as in rule 2 + `)`
   (for example `(error)` or `(pkg.Pair)`); Go would print an error's
   `Error()`, a `String()` result or an address.

The line is then truncated to its first 512 bytes, and every byte that does not
start a valid UTF-8 sequence (as Go's `utf8.DecodeRune` decides, with
`RuneError` of width 1) is replaced by `?`, because `sol_log_` rejects invalid
UTF-8.

#### Backend reproduction

A backend reproduces the line byte for byte from Check's output.
`CheckedProgram.Panics` maps each `panic` call (`*ast.CallExpr`) to a
`PanicSite` (`Call`, `Value`, `Text`, `Prefix`, `Suffix`). The exported Go
identifiers in `internal/compiler` are `PanicSite`, the `PanicValue`
constants `PanicText`, `PanicString`, `PanicSigned`, `PanicUnsigned`,
`PanicBool` and `PanicInterface`, `NilPanicLine` and `PanicLineBytes` (512).

At run time the argument is evaluated first (always, for its effects, except
`nil` and constants, which have none); then the line is built, truncated to
its first `PanicLineBytes` bytes, UTF-8-sanitized as above (after the
truncation, so a sequence the cut splits becomes `?`), logged once with
`sol_log_`, and `abort` is called. By `Value`:

- `PanicText`: the line is `Text`. This covers constant arguments, `nil`
  (`Text` is `NilPanicLine`) and values shown by their static type
  (`panic: (<T>)`, rule 3). `Text` is final apart from the runtime UTF-8
  sanitization: Check has already truncated it to 512 bytes.
- `PanicString`: `Prefix` + the string's bytes unmodified (no quoting or
  escaping) + `Suffix`.
- `PanicSigned`: `Prefix` + the value sign-extended to 64 bits, in decimal,
  with `-` when negative (MinInt64 is `-9223372036854775808`), + `Suffix`.
- `PanicUnsigned`: `Prefix` + the value zero-extended to 64 bits, in decimal,
  + `Suffix`.
- `PanicBool`: `Prefix` + `true` or `false` + `Suffix`.
- `PanicInterface`: `NilPanicLine` (`panic: panic called with nil argument`)
  when the interface value's type word is 0 (a nil interface), else `Text`
  (`panic: (<I>)`, the interface's static type).

`Prefix` is `panic: ` for an unnamed type, `panic: <T>(` for a named type and
`panic: <T>("` for a named string type; `Suffix` is correspondingly empty,
`)` or `")`. `<T>` is the runtime spelling of rule 2. A constant's `Text`
applies the same rules to the constant's type (its default type when
untyped), with integers in exact decimal.

A program built with a heap budget above 32 KiB whose transaction did not
request a large enough heap frame fails at its first allocation with a VM
access violation (also `ProgramFailedToComplete`, consuming the remaining
compute), not with the abort syscall.

These are **not** traps:

- `+`, `-`, `*`, unary `-` and `++`/`--` wrap modulo 2^width, two's complement
  for signed types.
- Signed division truncates toward zero, and the remainder takes the
  dividend's sign. `MinInt / -1` is `MinInt` and `MinInt % -1` is 0, for each
  signed width.
- A shift by the operand's width or more yields 0, or -1 for a signed `>>` of a
  negative value.
- Conversions truncate, or sign- or zero-extend by the source's signedness.
- Other `math/bits` calls never fail. [GO_BITS.md](GO_BITS.md) specifies their
  exact results, including `Add64`/`Sub64` carry inputs other than 0 and 1.
- `make(map[K]V, hint)` with a negative hint, or one whose table would exceed
  2^48 bytes, is treated as 0, as Go does. Reading, `len`, `delete` and `clear`
  on a nil map succeed.

## Contracted runtime behaviour

Besides traps and codes, these observable results are part of the contract,
because fixtures compare them byte for byte:

- **Append growth.** `append` grows capacity exactly as Go 1.22's
  `runtime.growslice`: `nextslicecap`, then `roundupsize` with Go 1.22's malloc
  size classes, including the 8-byte malloc header for pointer-holding element
  types above 512 bytes. `cap()` after an append, whether an append reuses the
  backing array, and the heap bytes a growth consumes all follow from it. The
  full rule is in [GO_ALLOCATION.md](GO_ALLOCATION.md#append-growth).
- **Heap accounting.** The allocator is a bump allocator over the heap region
  with no free. The first 8 bytes of the budget hold the in-use count, each
  allocation consumes its size rounded up to 8 bytes, and the out-of-memory
  trap fires at the first allocation that does not fit (32 KiB default, so
  32,760 usable bytes; `heap_bytes` up to 256 KiB). Locals moved to the heap by
  escape analysis consume budget the same way; `gosvm features` reports them.
  Converting a value to an interface allocates its Go size rounded up to 8
  bytes, except pointers and zero-size values, which allocate nothing
  ([interfaces](GO_INTERFACES.md#boxing-cost)).
  A call of a variadic function with `k > 0` extra arguments allocates
  its argument slice like a `k`-element slice literal; with none, or with a
  spread slice (`f(s...)`), it allocates nothing
  ([variadic functions](GO_FUNCTIONS.md#variadic-functions)).
  String literals and constant expressions allocate nothing; a concatenation
  allocates its total length rounded up to 8 bytes, or nothing when at most one
  operand is non-empty; `string(b)` allocates `len(b)` rounded up (nothing when
  empty), `string(rune)` 8 bytes and `string([]rune)` its encoded length
  rounded up; `[]byte(s)` and `[]rune(s)` allocate Go 1.22's `rawbyteslice`
  and `rawruneslice` sizes, which are also their capacities, and `[]byte` of a
  constant exactly its length (capacity `len`). The full table is in
  [GO_STRINGS.md](GO_STRINGS.md#allocation).
  Evaluating a function literal
  that captures `n ≥ 1` variables allocates `8 + 8n` bytes (nothing without
  captures, and nothing for a declared function, instance or method
  expression used as a value); a method value allocates 16 bytes for a
  pointer receiver and `8 + ((size+7)&^7)` for a value receiver of Go size
  `size`; captured variables are always moved to the heap (a captured
  three-clause loop variable is also copied to a fresh allocation before each
  post statement); a `defer` statement inside a loop allocates a 16-byte header
  plus its saved values as a C struct, rounded up to 8, per execution, and one
  outside loops allocates nothing ([function values](GO_FUNCTIONS.md#heap-cost)).
  A map header is 24 bytes (`make`, `map[K]V{}`); with the slot size
  `S = 8 + roundup8(sizeof(K)) + roundup8(sizeof(V))` and `cap(n)` the smallest
  power of two of at least 8 with `4n <= 3 cap(n)`, `make(m, h)` with `h > 0`
  and a literal of `n > 0` entries also allocate `cap(h) * S` or `cap(n) * S`
  at once; inserting a new key when `4 * (len + 1) > 3 * slots` allocates
  `2 * slots * S` (8 slots when there are none), and the old slots are never
  freed; every other map operation, including `delete` and `clear`, allocates
  nothing and never shrinks the table. Converting a map to an interface
  allocates nothing. The full table is in
  [GO_MAPS.md](GO_MAPS.md#heap-accounting).
  Package-level variables that are not read-only live in the globals block:
  a C struct of those variables in `CheckedProgram.Globals` order (each at
  its Go alignment), rounded up to 8 bytes (`GlobalBytes`). Every invocation
  allocates it first, before any initializer runs, so it is always at the
  heap start plus 8 and leaves `32,760 - GlobalBytes` bytes of the default
  budget; read-only variables (`Global.ReadOnly`, decided by Check) take no
  heap. Initializers and `init` functions then allocate like any other code
  ([package-level variables](GO_GLOBALS.md#heap-accounting)).
  See [GO_ALLOCATION.md](GO_ALLOCATION.md#allocator).
- **Compile-time evaluated initialization** (October 16;
  [GO_GLOBALS.md](GO_GLOBALS.md#compile-time-evaluation)). Check evaluates
  the longest prefix of the initialization steps that completes without
  trapping, panicking, calling the SDK or a native library, making a map
  keyed by pointers or interfaces, or exceeding 1,048,576 interpreter steps
  (`CheckedProgram.InitImage`, `PackageInit.EvaluatedVars`/`EvaluatedFuncs`).
  Every invocation then starts from that prefix's state and runs the
  remaining steps. This is not observable apart from CU and the ELF: the
  variables' values, aliasing and pointer identities, the heap accounting
  (the allocator's in-use count after the prefix is `InitImage.HeapUsed`,
  exactly what running the evaluated steps leaves, so every out-of-memory
  boundary is unchanged), the heap-frame fault at entry for budgets above
  32 KiB, and every trap and panic line of initialization (the step that
  would trap and every later step run at run time) are the same as running
  every step on every invocation. A backend may install the image or run
  every step.

## Evaluation order

goSVM evaluates expressions in gc's order: Go 1.22.0's
`cmd/compile/internal/walk/order.go` (the "order" pass), checked against
native Go 1.22.0 on the probe table in
[results/compiler/2026-10-15-eval-order](../results/compiler/2026-10-15-eval-order/README.md).
The spec fixes only the order of calls, method calls and receives; gc
evaluates calls (and the other *sequenced* operations below) into
temporaries where they appear and reads plain operands afterwards. Which
values a program computes, which trap fires first, and whether a panic line is
logged before a trap all follow from these rules.

**Owners.** Every evaluation belongs to an *owner*:

- a simple statement: an assignment (`=`, `:=`, `op=`), `++`/`--`, `return`,
  a `var` specification, or a `defer` statement (whose operands are the
  function value or receiver and the arguments; the deferred call itself runs
  when the function returns);
- a root expression: an expression statement, an `if` or `for` condition
  (each time it is evaluated), a `switch` tag (evaluated and saved first), each
  `case` expression (evaluated lazily, in source order, until one matches), a
  `range` operand, a type switch's operand;
- each operand of `&&` and `||` (the left one, then the right one only when
  needed), and each dynamic entry of a map literal (below);
- every sequenced operation, for its own operands.

**Rule.** To evaluate an owner: first evaluate every sequenced operation in
it that is not inside another sequenced operation (or a function literal), in
source order, each by this same rule; then perform its plain operations in
source order (operands before the operation using them); then the owner's own
operation (the call, the store, the comparison with the tag...). A sequenced
operation's value is computed once and used where it appears.

**Sequenced operations** (gc's `copyExpr` cases):

1. calls of declared functions, methods (static and interface), function
   values, literals, `gosvm/solana` and `math/bits` functions;
2. the builtins `len` and `cap` (when not constant), `append`, `copy`, `make`
   and `new`; the conversions `[]byte(s)` and `[]rune(s)` of strings;
3. `x && y` and `x || y` as a whole;
4. slice expressions `a[i:j]` and `a[i:j:k]` of slices, arrays, pointers to
   arrays and strings;
5. single-value type assertions `x.(T)` where `T` is an interface type or is
   not pointer-shaped (pointer-shaped: a pointer, map, function or channel, or
   a struct with exactly one field, or an array of length one, of a
   pointer-shaped type);
6. a map literal with at least one dynamic entry;
7. a map read `m[k]` whose key is `string(b)` of a byte slice, directly or as a
   field or element of struct and array literals;
8. the operand of a conversion to an interface type (explicit or implicit:
   assignment, argument, result, composite-literal element, comparison with an
   interface) when the operand is neither addressable nor constant (a
   constant, `nil`, or a struct or array literal of constants) and gc converts
   its type by address: every type except those of Go size 2 and alignment 2,
   size 4 and alignment 4 without pointers, size 8 and alignment 8 without
   pointers, and types whose sole component (through one-field structs and
   length-one arrays) is a string or slice. The operand's value is computed
   there; boxing it (and its out-of-memory trap) happens where the conversion
   is used;
9. a map key, in a read `m[k]`, an assignment, `op=` or `++`/`--` target
   `m[k]`, or a dynamic map-literal entry, when gc accesses the map through its
   generic path and the key is not constant and is not an addressable
   operand of exactly the key type. The generic path is every map whose
   element is larger than 128 bytes, or whose key type does not compare as 4
   bytes of memory without pointers, 8 bytes of memory, or a string (integer,
   boolean, pointer, and padding-free array and struct types of other sizes,
   interfaces, and structs or arrays containing strings or interfaces). A key
   converted to an interface key type is converted there too.
10. in `m[k] = append(m[k], args...)` (the same map index on both sides, as
    gc's `SameSafeExpr` decides), the target's map operand unless it is a
    variable, and its key unless it is a constant or a variable of exactly
    `uint32`, `uint64` or `string` type for a 4-byte, 8-byte or string key
    path (or any variable, for the generic path), are evaluated before the
    other arguments; the first argument reuses those values.

**Plain operations** are everything else: reading a variable, a field, an
element or a map element; indirection; arithmetic, shifts and comparisons
(including string, struct, array and interface comparisons); string
concatenation and the other conversions (`string(b)`, integer conversions);
boxing a value in an interface; struct, array and slice literals and map
literals of constant entries; function literals; method values; `&x`; and
type assertions to pointer-shaped types.

Consequences, as gc computes them: in `x + f()` where `f` assigns `x`, the
new `x`; in `f(x, g())`, `x` is read after `g`; a function value (`fv(g())`),
an interface receiver (`i.M(g())`) and a value receiver (`v.M(g())`,
`p.M(g())` copying `*p`) are read after the arguments' calls; in
`two(x, h(x, g()))`, `h`'s `x` is read before `h` runs and `two`'s after it;
`len(s) + f()` reads `len(s)` before `f`; `s[0:1][0] + f()` slices before `f`
and reads the element after it; `T{x, f()}`, `[]T{x, f()}` and
`string(b) + str(f())` read `x` and `b` after `f`.

A call of a variadic function with extra arguments is evaluated as the call
with those arguments in a slice literal (`f(a, x, g())` as
`f(a, []T{x, g()}...)`, as gc does): `x` is read after `g`, and the literal's
allocation is a plain operation; `f(a, g()...)` passes `g`'s slice. Pointers
to pointers add nothing: each indirection (`**pp`, `*pp = q`) is a plain
operation, so `**pp + f()` and `*p + f()` read through the pointers after `f`
has replaced them.

**Assignments.** In an assignment, the left-hand index and pointer operands
are plain operands of the statement: `a[i] = f()`, `p.x = f()`, `*p = f()`,
`b[0] += f()` and `m[k] op= f()` read `a`, `i`, `p`, `b` and `m` after `f` (the
store goes to the new slice, map or pointer when `f` replaced it), except for
the map keys of rule 9, which are read before `f`. All operands of a tuple
assignment are evaluated by the rule before any store; stores then happen
left to right (`a[i], i = 1, 2` stores at the old `i`). `x op= y` reads the
target after `y`, once.

**Map literals.** gc makes the map with the static entries (constant key and
value, including struct and array literals of constants, with no conversion
to an interface other than of untyped `nil`), in source order, then inserts
each dynamic entry in source order, each entry an owner of its key and value.
A dynamic entry with a computed key equal to a static key therefore wins
whatever their source order (`map[int]int{k: 3, 1: 2}` with `k == 1` holds
3).

**Traps.** A sequenced operation's traps happen where it is evaluated (a
failed type assertion, slice bounds, a `make` length, an out-of-memory
`append`). A plain operation's trap (index or slice-element bounds, nil
dereference, division or modulo by zero, a negative shift count, an
uncomparable interface comparison or map key, a nil-map store, out of memory
for a literal, concatenation, conversion or closure) happens after every
sequenced operation of its owner: `z[5] + f()`, `z[5] = f()`, `p.x + f()`
(nil `p`), `x/z + f()`, `m[k] = f()` (nil `m`) and `nilFunc(f())` all run `f`
first, so a `panic` in `f` logs its line and wins. A store's traps happen at
the store, after all operands; calling a nil function value or a method of a
nil interface traps after the function value and the arguments.

**Where gc's optimizer differs.** gc's SSA back end may move the nil-pointer
fault of an operand evaluated early (inside an inlined sequenced call such as
`b(*q == v)`, or a byte converted to an interface, `f(uint8(p.x), g())`)
after a later inlined call's panic; with inlining disabled (`-gcflags=-l`) gc
follows the order pass. goSVM always follows the order pass. Only which of a
nil-pointer trap and a panic line comes first can differ; values never do.
gc also evaluates `new(T)` arguments and builds slice-literal arguments of a
deferred call when the deferred call runs, while goSVM does so at the `defer`
statement (only the out-of-memory point can differ).

**Backend reproduction.** `CheckedProgram.Sequenced` reports the sequenced
operations; `Hoisted` lists, per owner, the sequenced operations to evaluate
first where evaluating left to right would observably differ;
`MapLiteralOrder` records the map-literal insertion order and
`SharedOperands` rule 10 ([backend interface](BACKEND_INTERFACE.md#evaluation-order)).
A backend may apply the rule everywhere or only at `Hoisted` owners.

## Error codes

A nonzero `Process` result is returned unchanged as the transaction's custom
error. Generated entrypoints, adapters and SDK helpers use these numbers;
application handlers use `Err*` constants in 1..5999.

### Entrypoint (generated C)

| Code | Meaning |
| --- | --- |
| 1001 | More accounts than the program's maximum |
| 1002 | Malformed duplicate-account marker |
| 1003 | Account data longer than 10 MiB |
| 1004 | Instruction data longer than 10,240 bytes |

SDK 1 single-account programs use 10–14 for their input checks
(`internal/compiler/abi.c.txt`).

### Runtime boundary (`solana` package)

| Code | Meaning |
| --- | --- |
| 2001 | Program account not executable, or not the classic Token program where required |
| 2002 | SDK 1 signed-transfer seed longer than 32 bytes |
| 2003 | SHA-256 input or output range out of bounds |
| 2004 | SHA-256 result not 32 bytes |
| 2005 | Account index out of range |
| 2006 | Malformed CPI account metas |
| 2007 | Malformed signer seeds or PDA input |
| 2008 | CPI instruction data longer than 10,240 bytes |
| 2009 | Native callback missing (native tests only) |
| 2010 | Resize or original length beyond limits |
| 2011 | Account not writable, or executable |
| 2012 | Account not owned by the program |
| 2013 | Refund account is the source, or malformed native key |
| 2014 | Refund or credit lamport overflow |
| 2015 | Rent output not 17 bytes |
| 2016 | Clock output not 40 bytes |
| 2017 | Return data longer than 1,024 bytes |
| 2018 | `MoveLamports` source balance below the amount |

### SDK 2 helpers

| Code | Meaning |
| --- | --- |
| 3001 | Seeds: more than 16, or one longer than 32 bytes, or nil where required |
| 3002 | CPI metas: more than 16, or nil |
| 3003 | CPI signers: more than 2 groups or 1,024 bytes |
| 3010 | Token account invalid (owner, executable, 165-byte layout, state, native) |
| 3011 | Token account identity |
| 3012 | Token mints differ |
| 3013 | Token authority or close authority mismatch, or missing signature |
| 3014 | Token account not writable |
| 3020 | Rent: size over 10 MiB, overflow or invalid threshold |
| 3021 | System program account invalid |
| 3022 | System create: account, alias, layout or privilege |
| 3023 | System create: payer signature, or target signer or PDA |
| 3024 | System create: size over 10,240 bytes |

Once a CPI's invoke syscall runs, a failure inside the callee fails the whole
transaction; the codes above come only from checks made before invoking.

### Schema 2 adapter

Checks run in this order; within each phase, accounts follow declaration order
(`internal/project/generate2.go`, [schema 2](SCHEMA2.md)):

1. Instruction discriminator and length: 6004.
2. Account count: 6000.
3. Writable, signer and executable flags: 6001.
4. Program ID length (6002, only when an account declares `owner:
   "program_id"`), then per account: key length and fixed address (6005),
   owner (6002).
5. Duplicate keys not declared as aliases: 6006.
6. Initialization target and payer empty: 6009.
7. Owned state length and discriminator: 6003.
8. Checked token layouts: SDK codes 3010–3014.
9. Relations, including token mint and authority: 6007.
10. Close authority: 6007.
11. PDA re-derivation: 6008.
12. Initialization (`system.Create` codes), in declaration order.
13. The handler; a nonzero result returns unchanged.
14. Post-handler close of declared `close` accounts (`CloseAccount` codes).
15. Write-back of writable, non-closed state, in declaration order. For
    instructions with init or close declarations, each such account is first
    re-read and re-checked: length (6003), owner (6002), then the
    discriminator (6003), or, for a newly initialized account, the
    discriminator is written. Read-only state, token accounts and other
    references are never written back.

Handler results propagate unchanged.

## Changelog

- 2026-10-14: contract established at `da6bf04` plus this document.
- 2026-10-14: steps 4 and 12–15 spelled out to match existing generated code
  (no behaviour change).
- 2026-10-14: addition (new functions), no existing behaviour changed:
  `math/bits` traps for `Div64`/`Div32` (`y == 0` or `y <= hi`) and
  `Rem64`/`Rem32` (`y == 0`).
- 2026-10-14: addition (signed integers), no existing behaviour changed: the
  division trap covers signed division, a negative shift count traps, and the
  wrapping, `MinInt / -1` and conversion rules above. The `math/bits` `uint`
  variants share the 64-bit traps (`Div`, `Rem`).
- 2026-10-14: addition (allocation), no existing behaviour changed: out-of-memory
  and `make` length/capacity-out-of-range traps; the heap-frame access
  violation for budgets above 32 KiB.
- 2026-10-14: signed integers, generics and allocation combined, no trap added
  or changed: negative signed `make` lengths and capacities, indexes and slice
  bounds use the traps above, and instances of generic code allocate and trap
  exactly as the equivalent hand-written code.
- 2026-10-14: append growth and heap accounting recorded as contracted
  behaviour at Qushta's request (documentation only, no behaviour change).
- 2026-10-14: addition (interface values), no existing behaviour changed: a
  failed single-result type assertion, comparing interfaces whose identical
  dynamic type is not comparable, and calling a method on a nil interface
  abort; boxing allocates as recorded under heap accounting; a value-receiver
  method through a nil `*T` in an interface and boxing beyond the budget use
  the existing nil-pointer and out-of-memory traps.
- 2026-10-14: addition (strings and `error`), no trap added and no existing
  behaviour changed: string index and slice bounds use the bounds trap, string
  concatenations and conversions allocate as recorded under heap accounting
  and use the out-of-memory trap, and `[]byte(s)`/`[]rune(s)` capacities are
  contracted with it; `error` values use the interface traps.
- 2026-10-14: addition (function values, closures, defer, named results), no
  existing behaviour changed: calling a nil function value aborts (a nil
  deferred value when the function returns); the heap cost of closure
  environments, method values, captured variables and deferred calls in loops
  is recorded under heap accounting; deferred calls do not run when a trap
  aborts. A nil interface or nil pointer in a `defer` statement, closures
  beyond the budget and comparisons of interfaces holding function values use
  the existing traps.
- 2026-10-14: addition (maps), no existing behaviour changed: two new traps
  (assignment to an entry in a nil map; hashing an interface key whose dynamic
  type is not comparable), map heap accounting recorded under contracted
  runtime behaviour (24-byte header, slot arrays sized by live-key count,
  growth at load factor 3/4, never freed), map allocations use the
  out-of-memory trap, and a negative or oversized `make` hint is treated as 0.
- 2026-10-14: maps combined with function values, no trap added and no
  existing behaviour changed: the two sets of heap costs add up (a closure
  capturing a map variable moves its 8-byte reference to the heap and never
  copies the table; a loop `defer` saving a map saves the 8-byte reference),
  and the nil-map write and unhashable-key traps fire inside closures and
  deferred calls at the points listed above.
- 2026-10-14: addition (compound assignment on integers), no trap added and
  no existing behaviour changed: `x op= y` uses the division-by-zero,
  negative-shift, bounds, nil-pointer and nil-map-write traps of the
  equivalent `x = x op y`, after the right operand is evaluated
  ([integers](GO_INTEGERS.md#compound-assignment)).
- 2026-10-14: addition (package-level variables and `init` functions), no
  trap added and no existing behaviour changed: initializers and `init`
  functions run on every invocation before `Process` and use the existing
  traps (a trap there aborts before `Process`); the globals block is the
  invocation heap's first allocation and is recorded under heap accounting
  (programs without package-level variables allocate exactly as before); a
  block larger than the heap budget is a Check error
  ([package-level variables](GO_GLOBALS.md)).
- 2026-10-14: addition (value types), no existing trap or code changed: the
  panic trap (`panic(v)` logs one contracted line with `sol_log_`, then
  aborts; no custom error code exists because the SVM cannot unwind to the
  entrypoint; deferred calls do not run; `recover` is a compile-time
  rejection). Struct `==`/`!=` and `switch` on a struct use the existing
  uncomparable-interface trap, arrays of composite elements the existing
  bounds, nil-pointer, nil-map and out-of-memory traps, and type aliases add
  nothing. Correction: struct map keys and interface comparisons of structs
  now ignore blank (`_`) fields, as Go does; before, keys differing only in a
  blank field were distinct.
- 2026-10-14: clarification (requested by Qushta), no behaviour changed: the
  [panic](#panic) section's backend-reproduction subsection spells out, per
  `PanicSite.Value`, how a backend builds the log line byte for byte from
  `CheckedProgram.Panics`, and lists the exported identifiers.
- 2026-10-14: merge of package-level variables and value types, no trap
  added and no existing code changed: a `panic` during package
  initialization logs its line, then aborts before `Process`. Check
  relaxation: an array type above the 1,024-byte per-value limit is
  accepted when its only values are package-level variables used in place
  (read-only data or the globals block, never a stack frame;
  [large tables](GO_GLOBALS.md#large-tables)); every other program keeps
  the same rejection at the same position, and accepted programs' C is
  unchanged.
- 2026-10-15: addition (standard-library packages, variadic functions,
  pointers to pointers), no trap added and no existing behaviour changed:
  the [standard-library packages](GO_STDLIB.md) are Go 1.22.0 code compiled
  with the program, so they use the existing traps: Go's own `panic` calls
  (for example `panic: strings: negative Repeat count`,
  `panic: errors: target must be a non-nil pointer`) log their line and
  abort as any panic, and runtime errors (an index out of range, a nil
  pointer, out of memory) are the existing traps. Their allocations follow
  the existing heap accounting, and `errors.ErrUnsupported`,
  `strconv.ErrRange` and `strconv.ErrSyntax` are package-level variables of
  the programs that import them. A call of a variadic function with `k > 0`
  extra arguments allocates its argument slice as a slice literal of `k`
  elements (no allocation for none, or for a spread `s...`); recorded under
  heap accounting. Pointers to pointers use the existing nil-pointer trap.
- 2026-10-15: change (evaluation order; announce to Qushta), no trap or code
  added: expressions are evaluated in gc's (Go 1.22.0) order, now contracted
  under [evaluation order](#evaluation-order). Before, goSVM read plain
  operands where they appeared, left to right, before the calls to their
  right; now they are read after their owner's calls (and the other
  sequenced operations), as gc does. Observable changes, only where a call
  writes an operand of its own expression or a plain operand's trap races a
  logging call: the value read (`x + f()`, `a[i] = f()`, `b[0] += f()`, a
  function value or receiver read after the arguments' calls), which trap or
  panic line comes first (`z[5] + f()` now logs `f`'s panic line), and map
  literals inserting constant entries before computed ones (a computed key
  equal to a constant key now keeps the computed value). Programs where no
  call can change an earlier operand compile to the same C.
- 2026-10-15: bug fix (aggregate parameters on SBF; announce to Qushta), no
  trap or code added: on SBF a callee could change its caller's string,
  slice, struct, array or interface variable by writing its own parameter
  (also a value receiver, also through an interface), could read its
  parameter's new value after changing the caller's variable through a
  package variable or pointer, and `t := append(s, x)` with spare capacity
  could lengthen `s`, because the SBF backend does not copy C `byval`
  parameters ([GO_STDLIB.md](GO_STDLIB.md#aggregate-parameters-on-sbf)).
  Behaviour now matches Go on SBF (it always did on the host): parameters
  are values, and writes trap or complete exactly as before. Only programs
  that hit the bug behave differently; the copies cost CU where they run
  (the full swap's wide swap +599/+603).
- 2026-10-15: change (build), no Check rule or trap changed: when the
  platform-tools v1.57 SBF backend reports a stack frame over 4,096 bytes (or
  a call overwriting its caller's frame), the built-in backend's build now
  fails with `SBF stack frame exceeds 4096 bytes; ...` naming the Go function
  (or the entrypoint, into which `Process` is usually inlined). Clang printed
  these as `Error:` lines but exited successfully, so such programs used to
  build and then read or write outside their frame at run time. `Check` and
  `CheckedProgram` are unaffected; no recorded program or example hits it.
- 2026-10-16: addition (real libraries), no trap added and no existing
  behaviour changed: package `io` joined the standard-library overlay (its
  error values, interfaces, `ReadAtLeast`, `ReadFull`, `WriteString`), so the
  unsupported-import message lists it
  (`... bytes, encoding/binary, errors, io, math, ...`), and `io.ReadAll` and
  the other left-out symbols are reported by name. Check relaxations: elided
  `&T{...}` elements of array type compile (they were `elided &T{...}
  literals require a struct type; write &T{...}`; other non-struct element
  types keep it), pointer conversions `(*T)(p)` compile (they were
  `dereference requires a supported pointer`), and a package-level variable
  whose address only reaches read-only pointer parameters stays read-only
  data (a 1,920-byte table used so is now admitted instead of `array storage
  exceeds experimental 1024-byte per-value limit`). The new forms use the
  existing traps (a nil converted pointer traps when dereferenced). No
  recorded corpus program's C changed; announce to Qushta.
- 2026-10-16: addition (compile-time evaluation of package initialization;
  announce to Qushta), no trap, code, panic line or heap accounting changed:
  Check evaluates the longest evaluable prefix of initialization and the
  built-in backend installs its heap image at entry (copied with the
  `sol_memcpy_` syscall, which joins the admitted fogo-v0 imports, then
  pointers to read-only data stored) instead of running those steps; only CU
  and ELF bytes change (`utils` initialization 25,442 → 114 CU). New
  `CheckedProgram.InitImage` (with `InitObject`, `InitValue`, `InitRef`,
  `InitMap`, `InitFunc`, `InitStop`, `InitSteps()`), `PackageInit.EvaluatedVars`
  and `EvaluatedFuncs`, and `Program.RuntimeInit` (off: evaluate). Recorded
  under contracted runtime behaviour above; the corpus's two empty `init`
  functions are no longer called (their C changed).
- 2026-10-16: addition (opt-in trimming; announce to Qushta), no trap or
  code added and nothing changed without the opt-in: a program can name
  dependency packages (`"trim"` in `gosvm.json`, `-trim`, `Program.Trim`)
  whose declarations are checked and compiled only when `Process` reaches
  them ([trimming](GO_PACKAGES.md#trimming)). In those packages an
  initializer of a variable no kept code references does not run (it is
  dropped and reported; Go would run it, so its allocation, panic or
  writes do not happen), and an import only dropped declarations use is
  neither compiled nor initialized; `init` functions and every other
  initializer run as before, and kept code is checked with the existing
  rules and messages. Diagnostic change: a whole-package rejection located
  in a module package other than the entry package (an import path
  starting with a domain name) ends with `; to check only the declarations
  Process reaches in package "P", opt in with "trim" in gosvm.json or -trim
  (docs/GO_PACKAGES.md#trimming)`. No recorded corpus entry's message
  changed (their dependency packages are not module paths).
- 2026-10-19: addition (`solana.MoveLamports`; announce to Qushta), no trap
  added and no existing behaviour changed: code 2018 (source balance below the
  amount) is new, and 2014 now also covers a destination credit overflow. The
  other checks reuse 2005, 2011 and 2012 in `ResizeAccount`'s order (source,
  then destination). Programs that do not call it compile to the same C.
