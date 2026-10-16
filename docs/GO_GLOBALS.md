# Package-level variables and `init` on-chain

On-chain Go code can declare package-level variables and `init` functions, in
any package of the program, with Go's semantics: zero values, Go's
initialization order, multiple `init` functions, blank and multiple-value
initializers, and variables of every admitted type that can be read,
assigned, addressed and used from closures and other packages.

```go
package binary

type bigEndian struct{}

func (bigEndian) Uint32(b []byte) uint32 { return uint32(b[3]) | uint32(b[2])<<8 | uint32(b[1])<<16 | uint32(b[0])<<24 }

var BigEndian bigEndian                   // never written: no storage, no cost
var pows64 = [...]uint64{1, 10, 100, 1e3} // never written: read-only data

type Err struct{ msg string }

func (e *Err) Error() string { return e.msg }

var ErrShort error = &Err{"short"}        // initialized on every invocation
var calls uint64                          // the globals block

func init() { calls = 1 }
```

**Every SBF invocation is a fresh process.** Before `Process` runs, every
invocation (including each CPI level) allocates the variables' storage and
runs every package's initialization, exactly as a Go program starting anew
would. Changes made during an invocation last until it ends and are never seen
by later transactions or other instructions; persistent state still belongs in
account data.

Programs without package-level variables or `init` functions compile to
byte-identical C and ELFs: none of the code below is emitted for them (the
full-swap ELFs are unchanged, fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…`).

## Semantics

| Form | Semantics |
| --- | --- |
| `var x T`, `var x = e`, `var x T = e`, `var a, b = 1, 2` | Zero values, or the initializer's value converted to `T` (an interface conversion boxes as anywhere else) |
| `var a, b = f()` | One call sets both variables |
| `var _ = f()` | The initializer runs; its value is discarded |
| `func init()` | Any number per file and package; they run after all of the package's variables are initialized, in file-name order, then source order. `init` cannot be called or referenced, as in Go |
| Types | Every admitted storage type: integers, `bool`, strings, arrays, structs, slices, maps, pointers, interfaces (including `error`), function values, empty structs and generic type instances. `solana.Context` is rejected (`Context must come from the entrypoint, not a package-level variable`) |
| Uses | Read, assigned, compound-assigned, incremented, ranged into, addressed (`&x`, `&x.f`, `&x[i]`, `x[:]`), passed to pointer-receiver methods and method values, captured by closures (by reference: a closure refers to the variable itself, which is not moved or copied), and named from other packages (`pkg.V`, including `pkg.F(x)` for a function-valued variable and `&pkg.V`) |
| Initializers | Any supported expression: calls (including across packages and of generic functions), closures, composite literals, map literals, `new`, `make`, method values, conversions |

### Initialization order

Go's rules, which Check reproduces:

1. **Packages** in Go 1.21's order: of the packages not yet initialized, the
   first by import path whose imports are all initialized. Imported packages
   are therefore always initialized first; independent packages run in
   import-path order (not the depth-first import order of Go before 1.21).
2. **Variables** within a package in dependency order, as go/types computes
   it (`types.Info.InitOrder`): repeatedly the earliest variable in
   declaration order that has no dependency on an uninitialized variable,
   where references through functions and methods count. `var a = b + f()`
   where `f` reads `c` initializes `b` and `c` first. Cycles are Go type
   errors (`initialization cycle`).
3. **`init` functions** of the package, in the order the files are sorted
   by name and then in source order.

`examples/global-values` records its order in a trace
(`LbBCzZa2AD4231_mI`): `log` (no imports), then `beta` before `zeta`
(both need only `log`; by import path), then `alpha` (needs `zeta`), with
`alpha.go`'s variables and init functions before `alpha2.go`'s, then the
entry package, whose `r1 = Mark('1') + r3`, `r2`, `r3 = compute()` (reads `r4`)
and `r4` run in the order `4 3 1 2`.

## Storage

**The globals block.** Every variable that is not read-only lives in one
block, laid out as a C struct of those variables in `Globals` order (packages
in initialization order, then declaration order; each aligned to its Go
alignment, at most 8), rounded up to 8 bytes (`CheckedProgram.GlobalBytes`).
It is the invocation heap's **first allocation**, made at entry before any
initializer runs, so it is always at `0x300000008` (the heap start plus the
allocator's 8-byte counter) on both targets, and variables are addressed at
fixed offsets from there. The SVM zero-fills the heap for every invocation,
so the zero values cost nothing; only non-zero initializers run code.

Writable `.data` and `.bss` sections are not used: the SBF VM maps the
program's ELF bytes read-only (only the stack, heap and input regions are
writable), and goSVM's solana-v3 linker script discards `.data` and `.bss`
outright. The heap region is the per-invocation writable memory that code can
address at a fixed location, which is also why variables cannot persist
between invocations.

**Read-only data.** A variable that can never change is a constant in the
ELF's read-only data instead: no block storage, no initialization code,
zero CU. Check decides this (`Global.ReadOnly`) with a sound syntactic
analysis; when in doubt, the variable stays in the block. A variable is
read-only when all of the following hold:

- Its type is *plain data* (integers, `bool`, and arrays and structs of
  them, with no pointers, slices, strings, maps, interfaces or function
  values), a string, or an unnamed slice of plain-data elements.
- Its initializer is absent (the zero value) or a constant expression or a
  composite literal of constants, recursively (`[...]uint64{1e0, 1e1}`,
  `Config{Limit: 1 << 40}`, `[]powerOf2{{1, [4]uint64{2}}}`), a constant
  string, or for a slice a non-empty literal. A multiple-value call never
  qualifies; neither does a specification that also declares a variable
  that is not read-only.
- No code anywhere (any function, method, generic instance, closure,
  initializer or `init` function) can write it: it is never assigned,
  compound-assigned, incremented or used as a range target; neither it nor
  any field or element of it has its address taken (`&`, slicing an array,
  calling a pointer-receiver method or making a pointer-receiver method
  value), except where the address goes only to a *read-only pointer
  parameter* (below); and a slice variable's value is only indexed for
  reading, ranged over, compared with nil, passed to `len` or `cap`, or used
  as the source of `copy` or the spread source of `append`. Every other use
  of a slice value (an assignment, an argument, a conversion, slicing) could
  write the shared elements through a copy, so it keeps the slice in the
  block.

<a id="read-only-pointer-parameters"></a>**Read-only pointer parameters**
(October 16, for holiman/uint256's `Log10`, which compares with
`z.Lt(&pows[t-20])`). Taking the address of a variable or of a field or
element of it does not keep it in the block when the address is passed
directly as an argument (or as the receiver of a pointer-receiver method
call) to a pointer parameter that only reads: in the declared function's
body, the parameter is never assigned, addressed or captured by a closure,
and each of its uses reads the storage it points to (`*p`, `p[i]`, `p.f` and
the paths below them, classified with the same rules as a variable's own
uses), compares it with `==`/`!=`, or passes it on to `len`, `cap` or
another read-only pointer parameter of a static call. Returning, storing or
converting the pointer, a dynamic call, a generic or variadic callee, a
function without a Go body and an intrinsic all count as writes
(`internal/compiler/readonlyptr.go`). Every pointer to the variable's storage
is then created at an analyzed address operation and never outlives the
call, so nothing can write the read-only data. No recorded corpus program,
example or full-swap ELF changed placement.

Read-only plain data is a `static const` C object (`.rodata`); a read-only
slice is a `static const` backing array whose header is built where the
variable is read (`len == cap ==` the literal's length); a read-only string is
the literal itself. Empty-struct singletons such as `binary.BigEndian`
take no storage at all. In the example, `pows64`, `table`
(`[]powerOf2`), `negatives`, `flags`, `zeroTable`, `config`, `greeting`,
`BigEndian` and `zeta.Base` are read-only.

Arrays of composite elements ([GO_ARRAYS.md](GO_ARRAYS.md)) and type aliases
([GO_TYPES.md](GO_TYPES.md)) need nothing special: `[4]Pair`, `[2][2][2]uint16`
or `[60]Int` with `type Int = wide.Int` are plain data, read-only when never
written, and an element of a package-level array is addressable storage, so
`G[i].F = v`, `G[i][j]++` and `pkg.G[i].F -= x` update the variable in place.

### Large tables

The 1,024-byte per-value array limit ([GO_ARRAYS.md](GO_ARRAYS.md)) protects
SBF stack frames (4 KiB): a value of a larger array type could be copied
into a frame (a local, a parameter, a temporary for `==`, a range copy, an
interface conversion or an initializer's composite literal). A
package-level variable itself is never on the stack: it is read-only data or
part of the globals block on the heap. So Check admits an array type above
the limit when **no value of the type is ever copied** — in the lowered
files (every function, instance, closure, initializer and `init`):

- no object other than a non-blank package-level variable has the type
  (no local, parameter, result or struct field), and
- every expression of the type, or of a pointer to it, is either the
  composite-literal initializer of a read-only variable (emitted as
  `static const` data, never built at run time) or names a package-level
  variable (`V` or `pkg.V`) that is the operand of an index expression
  (`V[i]`, `V[i].F`, `V[i][j]++`, `&V[i]`), of `len` or `cap`, or of a
  `range` without an iteration value (`for i := range V`).

`&V[i]` keeps a read-only table read-only when the address only reaches
[read-only pointer parameters](#read-only-pointer-parameters), as in
holiman/uint256's `Log10` (`z.Lt(&pows[t-20])`).
Anything else — a copy, `==`, `&V`, slicing, a value `range`, an
interface conversion, a method call, a writable variable with an
initializer (its package's initializer assigns the literal, building it
first; for example a table whose element address goes to a function that
writes through it) or an array of such arrays — keeps the rejection
`array storage exceeds experimental 1024-byte per-value limit` at the same
position, so the check order is unchanged. Elements are still bounded by
their own types' rules, and `v := V[i]` copies only an element. This admits
holiman/uint256's `var pows = [60]Int{...}` (1,920 bytes) and zero-valued
writable tables such as `var ledger [160]Int` (5,120 bytes of the globals
block, counted against the heap budget as usual). Tests:
`TestLargeTablesAccepted` and `TestLargeTablesRejected`
([globaltables_test.go](../internal/compiler/globaltables_test.go)) and the
4,800-byte read-only and 5,120-byte writable tables of
`examples/global-value-types` on SBF (both above the 4 KiB frame).

## Cost

Initialization runs on **every invocation**, so its CU is part of every
transaction's cost. Measured on the pinned runners (identical on fogo-v0 and
solana-v3; a program whose `Process` returns at once, against the same
program without variables, 85 CU):

| Declarations | CU per invocation |
| --- | --- |
| Read-only tables (`[8]uint64`, `[]uint32` of 5, a string) | 0 |
| `var hits [16]uint64` written elsewhere (allocate the block) | 14 |
| `var counter uint64 = 5` written elsewhere | 16 |
| Three sentinel errors `var ErrA error = &E{"a"}` (box) | 76 |
| `var registry = map[string]uint64{...}` with 4 entries | 442 |
| An `init` filling `[64]uint64` in a loop | 400 |
| All of `examples/global-values` (5 packages, 63 named variables of which 11 read-only, a 616-byte block, 7 `init` functions, maps, closures, strings, appends) | 2,179 (fogo-v0) / 2,183 (solana-v3) |

Real libraries make this cost visible. Importing
KyberNetwork/uniswapv3-sdk-uint256's `utils` (with holiman/uint256,
KyberNetwork/int256 and its `constants`) costs **25,442 CU per invocation**
(fogo-v0; 25,445 on solana-v3) before `Process` runs, nearly all of it
computed constants: 25 `uint256.MustFromHex`, one `MustFromDecimal`, three
`int256.MustFromDec`, an `Exp` and a map literal
([breakdown](../results/compiler/2026-10-16-real-libraries/README.md#initialization)).

**Go-exact: every initializer of every package in the program runs.**
Initializers and `init` functions in imported packages run even when
`Process` never uses their results, because they can trap (an index out of
range in a table builder aborts the transaction in Go too, before `main`),
change other variables, and consume heap. Pruning them would make those
programs behave differently. The only initialization goSVM skips is that of
read-only variables, whose initialization has no effects at all; an unused
read-only variable is not emitted. Computed constants are therefore the
cost to watch: `var sqrtConst1 = uint256.MustFromHex("0x...")` parses its
string on every invocation. Where a table can be written as a constant
composite literal, it is free.

## Heap accounting

The globals block is part of the contracted heap accounting
([error contract](ERROR_CONTRACT.md#contracted-runtime-behaviour)): it is the
first allocation of `GlobalBytes` bytes, so with the default 32 KiB budget
`32,760 - GlobalBytes` bytes remain for everything else (a 48-byte block
leaves 32,712: `make([]byte, 32712)` fits and `32713` aborts,
`TestGlobalHeapAccounting`). Initializers then allocate like any other code
(boxing, map literals, slice literals, closures, string concatenations) and
that memory also lives for the invocation. Read-only variables take no heap.
A block larger than the budget is rejected by Check (`package-level variables
need N heap bytes; the B-byte heap budget leaves B-8`). With a budget above
32 KiB, the block's allocation is the invocation's first allocation, so a
transaction without a large enough `RequestHeapFrame` faults at entry
([heap budget](GO_ALLOCATION.md#heap-budget)).

## Traps

No new traps. Initializers and `init` functions use the existing ones
(bounds, division, nil pointer, nil map write, nil function, out of memory,
...). A trap during initialization aborts the invocation before `Process`
runs, so the transaction fails and nothing is written, as a Go program that
panics during initialization never reaches `main`
(`TestGlobalInitTrap`). A `panic` call during initialization — in an
`init` function, in a variable's initializer or in any function an
initializer calls, in any package — first logs its contracted line
(`Program log: panic: …`, [ERROR_CONTRACT](ERROR_CONTRACT.md#panic)), the
same first line native Go prints before exiting with status 2; a runtime
error (an index out of range, say) logs nothing, as for any trap
(`TestGlobalInitPanics`, and on SBF `TestGlobalValueTypesSBF`).

## Deviations and notes

- **Operand order around calls** is gc's
  ([evaluation order](ERROR_CONTRACT.md#evaluation-order)): in `x + f()`
  where `f` assigns `x`, the new `x` is read, as for every operand a call may
  write (`TestGlobalOperandOrder`).
- **`var _ I = v`** discards `v` without converting it to `I` (nothing is
  boxed), as gc also allocates nothing for it.
- **No persistence.** Unlike a long-running Go process that calls `Process`
  repeatedly, every invocation starts from freshly initialized variables.
  Native tests that call a program's `Process` several times in one process
  see accumulated state that the chain never does; the differential test
  therefore runs each native vector in a fresh process.
- **`Process` itself** is called by the generated entry after
  initialization; a call of `Process` from Go code (or through a function
  value) runs it directly, without initializing again, as in Go.
- **SDK views** stored in package variables (`w.out = solana.Data(c, 0)`)
  behave like views stored in heap data: they are not refreshed by CPI or
  resizing ([views](GO_VIEWS.md)).

## Backend contract

Check lowers package initialization into ordinary checked code, so a backend
needs no special analysis ([backend interface](BACKEND_INTERFACE.md#package-level-variables)):

- `CheckedProgram.Globals` lists every named package-level variable with its
  declaration, its own initializer (`Init`), `ReadOnly` and its block
  `Offset`; `GlobalBytes` is the block size.
- `CheckedProgram.Inits` lists, in package initialization order, each
  package with initialization work: `Vars`, a synthetic `Function` (in
  `Functions`, with `PackageInit` set) whose body assigns the package's
  non-read-only initializers in go/types order (`a = e`, `a, b = f()`, `_ = e`),
  and `Funcs`, its `init` functions. It is checked, borrow-checked, escape
  analyzed and lowered like any function.
- The lowered files keep each `var` declaration; a specification keeps its
  initializers only when all of its variables are read-only (they are then
  the read-only data); otherwise its initializers appear only in `Vars`.
- At entry, before `Process`: allocate the block (when `GlobalBytes > 0`) as
  the first allocation, then for each `Inits` entry call `Vars` and then each
  of `Funcs`.

A backend must reproduce the order, the block's heap accounting and
read-only placement's absence of cost only where it is observable: the block
size moves out-of-memory boundaries; read-only data is not observable except
through CU.

## Evidence

- [`examples/global-values`](../examples/global-values/program.go): five
  packages (`log`, `zeta`, `beta`, `alpha` with two files, and the entry
  package with two files) covering the initialization order (dependency,
  cross-package, Go 1.21 package order, blank and multiple-value initializers,
  init functions in several files and packages, a deferred closure in `init`),
  every kind of variable, mutation within an invocation (scripts of 32
  operations), closures and method values over package variables,
  function-valued variables across packages, read-only tables (arrays, slices
  of structs, strings, signed values, an empty-struct byte-order singleton),
  errors and maps, and heap use. `TestGlobalsDifferential` runs 2,042 vectors
  (600 Go panics, each an abort at the same point) natively, **one fresh
  process per vector** (the `oracle` command), and as host-compiled C with
  the default 32 KiB heap.
- `TestGlobalsSBF` runs 241 of those vectors plus an idle invocation on
  fogo-v0 and solana-v3 through the pinned runners (65 aborts with
  rollback), and the CU probes above
  ([results](../results/compiler/2026-10-14-globals/README.md)).
- `TestGlobalAcceptedForms`, `TestGlobalPackages` (Go 1.21 package order,
  qualified reads, writes, addresses and calls), `TestGlobalReadOnly` (each
  write and escape pattern), `TestGlobalRejections`,
  `TestGlobalHeapAccounting`, `TestGlobalInitTrap`, `TestGlobalOperandOrder`,
  `TestGlobalRuntimeOnlyWhenUsed` and `TestCheckedProgramGlobals`
  ([globals_test.go](../internal/compiler/globals_test.go)).
- [`examples/global-value-types`](../examples/global-value-types/program.go)
  combines package-level variables with value types: read-only and writable
  arrays of structs and arrays (including the large tables above), in-place
  mutation of this package's and another package's composite arrays,
  struct and array `==` on globals and through interfaces, alias-typed
  variables and a panicking function that initializers also call.
  `TestGlobalValueTypesDifferential` runs 1,402 vectors against one fresh
  native process each; `TestGlobalInitPanics` runs eight
  initialization-panic probes (named, string, signed, bool and nil values,
  an imported package, a runtime error) against native processes; and
  `TestGlobalValueTypesSBF` runs 151 vectors plus an idle invocation and the
  eight probes on fogo-v0 and solana-v3
  ([results](../results/compiler/2026-10-14-globals-value-types/README.md)).
- The frozen check corpus: the nine recorded package-variable and `init`
  rejections now compile (`corpusChanges`); every other entry is unchanged.
- The [library shakeout](../results/compiler/2026-10-14-globals/README.md#library-shakeout)'s
  pass 2 rerun: no package-variable rejection remains.

The local runners are not a public network; see
[target profiles](TARGET_PROFILES.md).
