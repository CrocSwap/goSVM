# Invocation-heap allocation

On-chain Go code can allocate: `new`, `make`, `append`, `copy`, slice literals,
slices of any admitted element type, pointer and slice struct fields, and locals
whose references escape. Allocations live in the SBF heap region for the rest of
the invocation and are never freed. The budget is fixed at compile time
(32 KiB by default, up to 256 KiB); running out aborts. `append` grows capacity
exactly as Go 1.22's runtime does, so `cap()` and aliasing match native Go.

Programs that do not use any of this compile to byte-identical C and ELFs; the
allocator is emitted only when a program needs it.

## Builtins and slices

| Form | Semantics |
| --- | --- |
| `new(T)` | Zeroed `T` on the heap; `T` is any storable type (scalars, `bool`, value structs, scalar arrays) |
| `make([]T, len)`, `make([]T, len, cap)` | Zeroed backing array of `cap` elements. Lengths may have any integer type, as in Go |
| `append(s, v...)`, `append(s, t...)` | Go's append: in place when `len(s)+n <= cap(s)` (the result shares `s`'s array), otherwise a new array with [Go 1.22's capacity](#append-growth). Arguments are evaluated before growth; `append(s)` and `append(s, empty...)` return `s` (so `nil` stays `nil`) |
| `copy(dst, src)` | Copies `min(len(dst), len(src))` elements with `memmove` semantics (overlap allowed) and returns that `int` |
| `[]T{...}` | Positional and keyed literals; `len == cap ==` one past the largest index; elements are evaluated in source order, omitted ones are zero. Elided `&T{...}` elements of `[]*T` literals are heap structs |
| `&T{...}`, `&x` | Stack storage unless the reference escapes; then the storage is on the heap (see [escape rules](#escape-rules)) |

Slices may hold any storable element: integer scalars of every
[integer type](GO_INTEGERS.md), `bool`, value structs,
declared scalar arrays (`[][32]byte`), pointers (`[]*Node`), slices
(`[][]byte`) and [interface values](GO_INTERFACES.md) (`[]any`, `[]Shape`;
their growth uses Go's pointer-holding size classes). Elements must not contain zero-length arrays or empty structs
(their Go and C sizes could differ). Named slice types, slices of
`solana.Context`, arrays of pointers or slices, pointers to slices or pointers
and maps remain rejected. Slices of [strings](GO_STRINGS.md) (`[]string`, a
16-byte pointer-holding element) and `append(b, s...)`/`copy(b, s)` with a
string source are supported. `[]int`,
`[]int8` and the other signed slices are ordinary slices, and `copy`'s result
is an ordinary `int`.

Indexing, `len`, `cap`, nil comparison, two- and three-index slicing (of
slices, arrays and array pointers of any element type), `range` over any slice,
`s[i].F = v`, `&s[i]` and conversions between identical slice types follow Go.
Out-of-range indexes and slice bounds abort (the existing trap). A view of a
local array that escapes moves the array to the heap like any other escaping
local ([GO_VIEWS.md](GO_VIEWS.md)).

## Allocator

The SBF heap region starts at `MM_HEAP_START` = `0x300000000` for every SBPF
version (`solana-sbpf` 0.14.4 `ebpf.rs`: `MM_REGION_SIZE * 3`; only rodata and
bytecode swap places in SBPFv3), so fogo-v0 and solana-v3 use the same address.
The runtime maps a writable region of the transaction's heap size there for each
program invocation, including each CPI level, and zero-fills it:
`solana-program-runtime` (`create_vm` and `mem_pool.rs`, identical in 2.2, 2.3,
3.0 and 4.0; the runners use 3.0 through LiteSVM 0.8.2 for fogo-v0 and 4.0
through LiteSVM 0.13 for solana-v3) creates pooled heaps with
`AlignedMemory::zero_filled` and resets a returned heap with `fill(0)`. Rust's
bump allocator relies on the same zeroed start.

goSVM's allocator is a bump allocator over that region:

- The first 8 bytes hold the number of bytes in use; 0 means none yet, so
  allocations start at offset 8.
- Each allocation is rounded up to a multiple of 8 bytes and 8-byte aligned,
  enough for every admitted type. Memory is never freed or reused, so every
  allocation is already zero.
- An allocation that does not fit the remaining budget aborts. With the default
  32 KiB, at most 32,760 bytes are usable: `make([]byte, 32760)` succeeds and
  `make([]byte, 32761)` aborts.

Heap storage lives until the invocation ends. It is not visible to other
instructions, CPI callees or later transactions; persistent state still belongs
in account data.

### Strings

[String](GO_STRINGS.md#allocation) literals and constant expressions live in
read-only data and allocate nothing; so do `len`, indexing, slicing (which
shares bytes), comparisons, `switch` and `range`. A concatenation allocates its
total length rounded up to 8 bytes, or nothing when at most one operand is
non-empty; `string(b)` allocates `len(b)` rounded up (nothing when empty);
`string(rune)` 8 bytes; `[]byte(s)` and `[]rune(s)` follow Go 1.22's
`rawbyteslice` and `rawruneslice` (capacity and bytes `roundupsize` of the
size); `[]byte` of a constant allocates exactly its length (rounded up to 8).
Programs with strings carry the heap runtime only when a string operation
allocates.

### Interface boxing

Converting a value to an [interface](GO_INTERFACES.md) is an allocation like
any other: a pointer is stored directly and a zero-size value allocates
nothing; every other value is copied into a new allocation of its Go size
rounded up to 8 bytes (`uint64`, `int8` or `bool`: 8 bytes; `[3]uint64`: 24; a
slice header: 24). Comparisons, assertions, type switches, dynamic calls and
interface-to-interface conversions allocate nothing. With the default budget,
4,095 `uint64` conversions fit and the 4,096th aborts
([boxing cost](GO_INTERFACES.md#boxing-cost)). Programs with interface values
always carry the heap runtime.

### Function values and closures

[Function values](GO_FUNCTIONS.md#heap-cost) allocate like any other value:
a declared function, instance, method expression or literal without captures
allocates nothing; evaluating a literal with `n` captured variables allocates
`8 + 8n` bytes (the target ID and one address per capture); a method value
allocates 16 bytes (pointer receiver) or 8 plus the receiver's size rounded up
to 8 (value receiver). Every captured variable is moved to the heap by the
escape analysis (below), so closures share it by reference; a captured
three-clause loop variable gets a fresh allocation before each post statement
(Go 1.22 per-iteration variables). A `defer` in a loop allocates its record (16
bytes plus the saved values, rounded up to 8) per execution. Programs with
function values or `defer` always carry the heap runtime.

## Heap budget

The budget is a compile-time constant (`GOSVM_HEAP_BYTES` in the C runtime):

- Projects: `"heap_bytes": 65536` in `gosvm.json` (schema 1 or 2). Omitted or 0
  means 32768.
- Low-level CLI: `gosvm -heap-bytes 65536 ...`.

It must be a multiple of 1,024 from 32,768 to 262,144: the heap frame sizes a
transaction can request. **Above 32 KiB, every transaction that calls the
program must start with a ComputeBudget `RequestHeapFrame` instruction for at
least that size**, for example with the Rust SDK:

```rust
ComputeBudgetInstruction::request_heap_frame(64 * 1024)
```

(Program `ComputeBudget111111111111111111111111111111`, data `01` followed by
the size as a little-endian u32.) The runtime charges 8 CU per 32 KiB beyond the
first. A program built with a larger budget touches the budget's last byte on its
first allocation, so a missing or too-small frame fails there deterministically
with an access violation (`Access violation in heap section at address
0x30000ffff` for 64 KiB), consuming the transaction's remaining compute, rather
than at some later, data-dependent allocation. The ELF cannot detect the frame
size more cheaply; the client must send the instruction. Local fixtures can send
it through the `ComputeBudget` alias of [SVM fixtures](SVM_FIXTURES.md).

## Append growth

Go leaves capacity growth unspecified, but native tests observe it, so goSVM
reproduces Go 1.22's `runtime.growslice` for an element of `size` bytes:

1. `nextslicecap(newLen, oldCap)`: `newLen` if it exceeds `2*oldCap`; else
   `2*oldCap` below 256 elements; else add `(cap + 768) / 4` until it reaches
   `newLen`.
2. `roundupsize(newcap * size)` with Go 1.22's malloc size classes (8, 16, 24,
   32, 48, 64, 80, ... 28672, 32768): the smallest class that fits; above 32,760
   bytes, a multiple of 8 KiB. Element types that contain pointers or slices
   and need more than 512 bytes reserve Go's 8-byte malloc header inside the
   class.
3. The new capacity is the rounded size divided by `size`; the old elements are
   copied and the rest is zero.

The heap consumed by a growth is the new capacity times the element size,
rounded to 8. Large arrays round to 8 KiB pages exactly as Go's capacity does,
which matters for a 32 KiB budget.

Verification ([alloc_growth_test.go](../internal/compiler/alloc_growth_test.go)):
the model matches Go's own runtime (`reflect.AppendSlice` calls `growslice`
with the same new length) for 20,243 growth events across 364 element types of
1 to 1,100 bytes, with and without pointers, and append counts 1, 2, 3, 7 and
100; it matches compiled appends of `byte`, `[3]uint64`, `*uint64`, `[]byte` and
a 520-byte pointer struct; and the emitted C helpers match the model on 21,984
(length, capacity, count, size, pointers) combinations.

## Escape rules

Heap storage has invocation lifetime, so references to it may be returned,
stored in variables, fields and elements, and passed anywhere.

A reference to local storage (a variable, a value parameter or receiver, a range
variable or an `&T{...}` literal) **escapes** when it may be:

- returned;
- kept in a variable declared outside the local's lexical scope (block, branch
  or loop);
- stored into heap data: assigned to a field, an element or through a pointer,
  written into a struct or slice literal, or appended;
- converted to an interface (an interface value can be copied anywhere), or
  passed to a dynamic method call, which may run any implementation;
- captured by a function literal (always moved: a closure can be called
  anywhere), bound by a pointer-receiver method value, passed to a call
  through a function value (which may run any target), or returned by a
  literal;
- saved by a `defer` statement while declared in an inner block of the
  function (the block's storage ends before the deferred call runs); or
- passed as an argument that the callee may store into heap data (the callee's
  `BorrowSummary.Escapes`, propagated through calls and returned borrows).

An escaping local is **moved to the heap**, as Go's escape analysis would do:
its storage is allocated where it is declared (afresh in each loop iteration,
following Go 1.22's per-iteration variables; parameters and receivers at
function entry), and every use goes through that storage. The analysis is the
existing conservative, flow-insensitive union of origins, so it can move a local
that gc would keep on the stack; that only costs heap budget, never semantics.
Moves are part of the checked program (`Function.HeapVars`, `HeapLiterals`).

**Pointer and slice struct fields are allowed.** The invariant that keeps the
borrow analysis sound is: a pointer or slice stored in a struct field, a slice
element or through a pointer refers only to heap storage or to SDK invocation
storage (account data, keys, instruction data, `Process`'s byte-slice
arguments), never to a stack frame. The store rule above enforces it: storing a
local reference moves the local to the heap, and storing a caller's borrow makes
the caller responsible (it moves its own local or propagates the obligation).
Reading a reference from a field or element therefore yields heap or SDK
storage. Consequently no program is rejected for an escape any more; the
earlier "reference escapes ... heap allocation unsupported" programs compile and
behave as in Go ([alloc_moves_test.go](../internal/compiler/alloc_moves_test.go)).

SDK views keep their existing meaning: storing one in heap data does not extend
or refresh it after CPI or resizing ([GO_VIEWS.md](GO_VIEWS.md)).

## With generics and signed integers

Allocation works inside [generic](GO_GENERICS.md) code like anywhere else:
`make([]T, n)`, `append` on `[]T`, `new(T)`, `[]T{...}` literals, generic
structs with pointer or slice fields (`type Node[T any] struct{ Next *Node[T];
Val T }`) and methods that append to a `[]T` field. Escape analysis runs
separately for every instance, on the instance's own copy of the declaration:
`func Box[T any](v T) *T { return &v }` moves the parameter of `Box[int16]`
and of `Box[uint8]` (each instance's own object), while an instance whose
values do not escape moves nothing. The heap runtime is emitted only when an
instance or ordinary code needs it; an uninstantiated generic function that
would allocate adds nothing.

`make` lengths and capacities, indexes and slice bounds may have any
[integer type](GO_INTEGERS.md). A negative `make` operand aborts where Go
panics (`makeslice: len out of range` / `cap out of range`); a negative index
or bound aborts like any out-of-range one. Slices of signed elements (`[]int8`,
`[]int`, `[]Tick`) follow Go 1.22's growth for their element size.

[`examples/mixed-values`](../examples/mixed-values/program.go) combines the
three: generic functions that allocate over slices of signed and unsigned
elements, generic stacks and linked lists whose nodes live on the heap, moved
generic parameters, receivers and loop variables, signed `make` operands,
indexes and bounds, and signed division and shifts over allocated slices.
`TestMixedDifferential` compares 16,696 vectors (4,849 Go panics, each an
abort at the same point) natively and as host-compiled C with the default
32 KiB heap, and `TestMixedSBF` runs 444 of them (201 aborts with rollback) on
fogo-v0 and solana-v3 ([mixed_test.go](../internal/compiler/mixed_test.go)).

## Traps

Allocation adds two aborts to the [failure contract](ERROR_CONTRACT.md):

- **Out of memory**: `new`, `make`, a growing `append`, a slice literal, an
  escaping `&T{...}`, a moved local, an interface conversion, or a string
  concatenation or conversion needs more than the remaining budget.
- **make length or capacity out of range**: `len > cap`, a negative signed
  length, or a capacity whose size exceeds Go's 2^48-byte `maxAlloc` (Go panics
  `makeslice: len out of range` or `cap out of range`).

Native Go has no fixed budget: a program that runs out of memory on chain
completes natively. The out-of-memory boundaries are therefore tested with exact
budget arithmetic rather than a native oracle.

## Representation and backend contract

Every slice is the internal `slice` descriptor (pointer, length, capacity);
element `i` of a `[]T` is at `ptr + i*sizeof(T)`, and Go and C sizes agree for
admitted elements. Programs that allocate always carry capacities. The heap
runtime (allocator, `growslice`, `makeslice`, `memmove`) is emitted only for
programs that call `new`/`make`/`append`/`copy`, write a slice literal or
elided `&T{...}`, move a local, use a non-byte slice or a pointer/slice
field, use interface values, or concatenate or convert strings. An alternative backend must reproduce the allocator layout and budget
(observable only through out-of-memory boundaries), the growth rule and the
heap moves; see [BACKEND_INTERFACE.md](BACKEND_INTERFACE.md).

## Verification

- Check tables with exact messages and host-compiled accepted forms:
  [alloc_test.go](../internal/compiler/alloc_test.go).
- Differential: [examples/alloc-values](../examples/alloc-values/program.go) runs
  natively and as generated C (256 KiB heap) over 408 vectors: append
  len/cap/aliasing after every step for 18 element kinds (1, 2, 3, 4, 5, 8
  with and without pointers, 12, 16 (two kinds), 24 (array and `[]byte`), 40
  (array and pointer struct), 72, 520 (array and pointer struct, the malloc
  header case) and 1,024 bytes), make/new zero values and make traps, copy
  overlap, slicing bounds, linked lists and trees, escaping locals, literals and
  aliasing; 45 vectors panic in Go and abort in C at the same point.
- Exact budget arithmetic at 32 KiB and 64 KiB: one `make`, `new` plus `make`,
  a moved loop local (4,095 iterations fit), byte appends until exhaustion, and
  `make` range traps.
- SBF ([TestAllocationSBF](../internal/compiler/alloc_sbf_test.go)) for fogo-v0
  and solana-v3 through the pinned runners: 102 native-Go vectors (14 aborts
  with rollback), the 32,760/32,761-byte boundary, and a 64 KiB build with
  `RequestHeapFrame` (65,528 fits, 65,529 aborts; no or a 48 KiB frame faults
  on the first allocation). Abort and access-violation logs are checked
  separately. [Results](../results/compiler/2026-10-14-allocation/README.md).
- [Features report](FEATURES_REPORT.md) counts `new_calls`, `make_calls`,
  `append_calls`, `append_spreads` and `copy_calls`.

The local runner is not a public network; Fogo's heap behaviour is assumed to
match Agave's, not established here.
