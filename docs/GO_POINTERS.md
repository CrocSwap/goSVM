# Constrained pointers and methods

The experimental compiler supports value and pointer methods and references to
caller-owned storage. Native Go and compiled SBF checks cover alias updates,
receiver copies, nil and bounds failures, and returned caller borrows. Local
storage whose reference escapes is moved to the invocation heap
([allocation](GO_ALLOCATION.md#escape-rules)). [Borrowed byte views](GO_VIEWS.md) are also implemented with the same lifetime
analysis.

## Supported storage and calls

Pointers may refer to the supported unsigned or bool scalars, named value structs,
and bounded scalar arrays. Locals, parameters, nested fields, array elements and
slice elements can be addressed. `&T{...}` creates local storage; its reference
obeys the same lifetime analysis as a local variable and moves to the heap if it
escapes. `new(T)` allocates on the heap. Pointer and slice struct fields and
pointer-element slices are supported and hold only heap or SDK storage
([allocation](GO_ALLOCATION.md#escape-rules)). Pointers to pointers are
supported ([below](#pointers-to-pointers)); pointers-to-slices, pointers to
function values, named pointer types and pointers to the opaque SDK Context are
rejected.

Direct calls support value receivers, pointer receivers, Go's implicit addressing
of addressable values, implicit dereferencing for value receivers, and explicit
method expressions such as `Value.Read(v, n)` and `(*Value).Read(p, n)`. Imported
methods use their actual named types and method identities. Method values and
recursion remain unsupported; calls through [interface values](GO_INTERFACES.md)
dispatch dynamically. Pointers to interface values (`*any`) are admitted.

A value receiver gets a copy. A pointer receiver updates the caller's storage and
may return a pointer borrowing from that storage. For example:

```go
type Counter struct { N uint64 }
func (c *Counter) Add(n uint64) *uint64 {
    c.N = c.N + n
    return &c.N
}
func update() uint64 {
    c := Counter{N: 7}
    p := c.Add(3)
    *p = *p + 1
    return c.N // 11
}
```

Pointers have nil zero values and support `==`/`!=`, assignment, parameter passing
and results, including multiple results. Dereferenced values retain existing
by-value struct/array semantics. Increment/decrement now also accepts supported
fields, dereferences and indexed elements, evaluating the target operands once.
Compound assignment works on the same targets
([integers](GO_INTEGERS.md#compound-assignment)).

## Pointers to pointers

`**T` (and deeper) values work as in Go: `pp := &p`, `**pp = v`, `*pp = &x`,
`new(*T)`, parameters and results, as fields and elements. They were added
for the standard library's `errors.As(err, &target)` with a pointer-typed
`target` ([standard library](GO_STDLIB.md#errors)).

The borrow analysis keeps them sound with one rule: a pointer variable whose
address is taken (`&p`) is treated like heap storage, because code can
store into it through the `**T`. Every reference assigned to it is stored
(a local moves to the heap; a parameter becomes the caller's obligation,
`BorrowSummary.Escapes`), and reading it yields heap or SDK storage, as
reading a pointer field does. A reference stored through a `**T` (`*pp =
&x`) is a store into heap data, as any store through a pointer. Pointers to
slices stay rejected.

## Borrowed lifetimes

A reference may borrow from incoming pointer/slice parameters, SDK invocation
storage, or a local storage region. Nonrecursive functions receive per-result
borrow summaries, including receiver borrows. Calls substitute the actual
argument roots through forwarded calls and multiple results. Assignments join
origins across branches and loop back edges; they never discard a potentially
live origin just because another path overwrites a reference.

Returning an address of a local or of a copied value parameter/receiver moves that
local (or the copy) to the invocation heap, as Go's escape analysis would. A
pointer receiver's `&c.N` borrows the caller's storage; a value receiver's `&c.N`
belongs to its copy, which then lives on the heap. Returning an incoming pointer,
a field of incoming pointed storage, or an element of an incoming slice is a
plain borrow. A caller that passes local storage to a function returning that
borrow moves the local only if the result escapes in turn.

Storing a borrow in a variable outside its backing storage's lexical scope
(block, if, switch-case and loop locals), or into a field, element, literal or
`append`, also moves the local to the heap; a loop local gets fresh storage in
each iteration. The analysis is deliberately conservative and flow-insensitive:
it can move a local that ordinary Go would keep on the stack, which costs heap
budget but never changes results. Until allocation existed these programs were
rejected; see [GO_ALLOCATION.md](GO_ALLOCATION.md#escape-rules).

## Evaluation and failures

Assignment captures pointer/index operands and evaluates RHS expressions before
performing left-to-right stores. An earlier local write or RHS side effect can
precede a later nil/bounds failure. Native-Go/generated-C comparisons inspect
these pre-panic effects; SBF checks separately prove transaction rollback.

Pointer receiver calls may handle nil themselves. A nil implicit value receiver
or a nil pointer passed to a value-method expression aborts after the explicit
argument calls in the verified fixtures. Taking `&*p` still evaluates Go's
indirection and panics when `p` is nil. Taking a field/element address checks
nil/bounds too. `len` and `cap` of a pointer to an array do not dereference it;
nonconstant operand calls still execute.

Go does not order every plain variable read relative to a neighboring call.
The compiler snapshots ordinary expression operands in source order; do not rely
on a particular native compiler's choice for otherwise unspecified reads. The
receiver-copy differential fixture uses a function-returned snapshot to give its
receiver evaluation a defined order relative to the mutating argument call.
See the [Go evaluation rules](https://go.dev/ref/spec#Order_of_evaluation).

Runtime nil/bounds failures abort; there is no panic recovery, GC, stack growth or
general Go conformance claim. Heap allocation is bounded and invocation-scoped
([GO_ALLOCATION.md](GO_ALLOCATION.md)). Account data borrowed from the
SDK remains invocation data, not a durable object or a guarantee that a decoded
snapshot is current after CPI. Lifecycle/framework APIs must read current account
state and define their own mutation policies.

## Verification

The [shared-package fixture](../examples/pointer-values/program.go) imports actual
receiver types and methods from [its model package](../examples/pointer-values/model/model.go).
[Compiler tests](../internal/compiler/pointers_test.go) compare 1,000 random
native-Go/generated-C cases and 60 nil/bounds/error/alias edge cases. Additional
checks cover receiver call effects, pointer-array len/cap, cross-package result
origins and local/block/loop escapes, including a loop back edge, which now move
to the heap ([alloc_moves_test.go](../internal/compiler/alloc_moves_test.go)).

The [SBF evidence](../results/compiler/2026-10-05-pointer-values/README.md) records
194 ordinary-Go vectors and 196 SBF scenarios in three repetitions. It checks
exact state/error outcomes and multi-instruction nil/custom-error rollback. The
3,600-byte ELF's handler and entry frames are each 64 bytes; hidden memory helpers
use zero-byte frames. Instrumented linking reproduces the exact tested ELF.
These frame measurements apply to the fixture, not every supported source.

Reproduce with a new evidence directory:

```sh
python3 scripts/pointer_verify.py --output results/compiler/new-pointers
```

The recipe builds an isolated CLI, installs the pinned local runner package and
records source/artifact hashes. Historical results remain snapshots of their own
sources. The compiler's method/pointer and view gates are distinct from the remaining
schema-2 generation and full application requirements.
