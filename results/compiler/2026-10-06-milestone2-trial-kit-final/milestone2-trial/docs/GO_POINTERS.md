# Constrained pointers and methods

The experimental compiler supports value and pointer methods and references to
caller-owned storage. Native Go and compiled SBF checks cover alias updates,
receiver copies, nil and bounds failures, and returned caller borrows. Escaping
local storage is rejected rather than allocated on a heap. [Borrowed byte views](GO_VIEWS.md) are also implemented with the same lifetime
analysis.

## Supported storage and calls

Pointers may refer to the supported unsigned or bool scalars, named value structs,
and bounded scalar arrays. Locals, parameters, nested fields, array elements and
byte-slice elements can be addressed. `&T{...}` creates invocation-local storage;
its reference obeys the same lifetime checks as a local variable. Pointer fields,
pointer-element arrays/slices, pointers-to-pointers, pointers-to-slices, named
pointer types and pointers to the opaque SDK Context are rejected.

Direct calls support value receivers, pointer receivers, Go's implicit addressing
of addressable values, implicit dereferencing for value receivers, and explicit
method expressions such as `Value.Read(v, n)` and `(*Value).Read(p, n)`. Imported
methods use their actual named types and method identities. Method values,
interfaces and recursion remain unsupported.

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
Compound assignment remains unsupported.

## Borrowed lifetimes

A reference may borrow from incoming pointer/slice parameters, SDK invocation
storage, or a local storage region. Nonrecursive functions receive per-result
borrow summaries, including receiver borrows. Calls substitute the actual
argument roots through forwarded calls and multiple results. Assignments join
origins across branches and loop back edges; they never discard a potentially
live origin just because another path overwrites a reference.

Returning an address of a local or of a copied value parameter/receiver fails with
a source-located escape diagnostic. A pointer receiver may return `&c.N`; a value
receiver cannot return `&c.N`, because that field belongs to its local copy.
Returning an incoming pointer, a field of incoming pointed storage, or an element
of an incoming byte slice is allowed. A caller that passes local storage still
cannot forward that borrow beyond the local's lifetime.

Storing a borrow in a variable outside its backing storage's lexical scope also
fails. This includes block, if, switch-case and loop locals, even when ordinary Go
would heap-allocate the object or later control flow would avoid using the pointer.
The analysis is deliberately conservative and flow-insensitive. It can reject
safe native-Go programs; rejected forms require a supported restructuring, not
an implicit change to Go storage lifetime.

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

Runtime nil/bounds failures abort; there is no panic recovery, heap allocation,
GC, stack growth or general Go conformance claim. Account data borrowed from the
SDK remains invocation data, not a durable object or a guarantee that a decoded
snapshot is current after CPI. Lifecycle/framework APIs must read current account
state and define their own mutation policies.

## Verification

The [shared-package fixture](../examples/pointer-values/program.go) imports actual
receiver types and methods from [its model package](../examples/pointer-values/model/model.go).
[Compiler tests](../internal/compiler/pointers_test.go) compare 1,000 random
native-Go/generated-C cases and 60 nil/bounds/error/alias edge cases. Additional
checks cover receiver call effects, pointer-array len/cap, cross-package result
origins and source-located local/block/loop escapes, including a loop back edge.

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
