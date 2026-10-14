# Multiple results and practical control flow

The milestone 2 compiler now supports multiple unnamed results, multiple
assignments/declarations, expression switches and unlabeled `continue`, using the
same subset types as single-result functions. This permits handlers and shared
calculations to return updated values plus an explicit status without inventing
wire layouts for intermediate results.

```go
type Status uint64
const Success Status = 0

func Apply(pool Pool, args SwapArgs) (Pool, Status) {
    if args.Amount == 0 {
        return pool, 7
    }
    pool.Total = pool.Total + args.Amount
    return pool, Success
}

next, code := Apply(pool, args)
if code != 0 {
    return uint64(code)
}
```

Multiple results retain Go type identity and value-copy semantics, including
structs and scalar arrays across imported packages. Forwarding `return other()`
and passing a multi-result call as the sole argument list (`use(pair())`) are
supported. Blank destinations still evaluate their right-hand expressions.
Multiple `:=` can reuse existing variables while declaring new ones; `var` can
initialize or zero multiple variables.

Assignment evaluates left-hand operands and right-hand expressions before
left-to-right stores. Swaps use old values; index expressions run once. A later
bounds failure can follow an earlier local write, and native-Go/generated-C tests
observe those effects before panic/abort. Actual SBF transactions must still
roll back all account updates on failure. The generated C result structs are
internal compiler representations, not Go memory-layout or wire-format promises.

`switch` supports scalar/bool tags, supported scalar-array tags, interface tags
and cases ([interfaces](GO_INTERFACES.md)), expressionless
conditions, init statements, multiple case expressions, and default anywhere in
the source. Case calls run in order and stop once a case matches. Type switches
follow the same rules ([interfaces](GO_INTERFACES.md)). Struct comparisons,
`fallthrough` and labeled branches remain unsupported. An unlabeled
break exits the nearest switch or loop. An unlabeled continue targets the nearest
loop, including from a nested switch, and executes the loop post statement once.

## Range loops

`for … range` follows Go 1.22 semantics over four kinds of operand:

| Operand | Key type | Forms |
| --- | --- | --- |
| integer `n` of any [integer type](GO_INTEGERS.md) | type of `n` | `for i := range n`, `for range n` |
| untyped constant | `int` | `for i := range 10` |
| fixed scalar array, or pointer to one | `int` | `i`, `i, v`, `_, v`, none |
| slice or view of any element type ([allocation](GO_ALLOCATION.md)) | `int` | `i`, `i, v`, `_, v`, none |

```go
for i, v := range key {          // key is a [32]byte: v reads a copy
    out[i] = v ^ mask[i]
}
for j := range uint64(len(data)) / 8 { // j is uint64
    sum = sum + gosvmRead(data, j*8, 8)
}
```

- The operand is evaluated once, before the first iteration. An integer count
  and a slice's length are fixed then; later writes to the count or slice
  variable do not change the iteration. A negative signed count runs no
  iterations.
- A range over an array **value** with a value variable iterates over a copy:
  writes to the array in the body are not seen by `v`. A pointer to an array is
  read through on every iteration, so `v` sees earlier writes, and a nil pointer
  aborts on its first element (never for a zero-length array). Slice elements
  are read from the shared storage on each iteration.
- Without a value variable, an array's length is constant and the array is not
  loaded: a nil `*[N]T` or `*p` operand iterates `N` times without a nil check.
  Calls in the operand still run once (`for i := range f()`). Conversions and
  composite literals are rejected in this index-only position; range over the
  operand itself.
- Every iteration has fresh iteration variables. Assigning to `i` or `v` never
  changes the iteration. `&v` may be taken; if it escapes the loop, each
  iteration's `v` is moved to the heap ([escape rules](GO_ALLOCATION.md#escape-rules)),
  so every kept pointer is distinct, as in Go 1.22.
- `for i = range n` and `for i, v = range x` assign to existing local variables;
  other targets (fields, elements, dereferences) are rejected. After the loop the
  variables hold the last assigned values, or are unchanged if no iteration ran.
- Unlabeled `break` and `continue` behave as in other loops. Labels, and range
  over strings, maps, channels and functions, are rejected.

**The `int` key.** Array, slice, untyped-constant and `len` ranges produce an
`int` key. It is an ordinary `int` value ([integers](GO_INTEGERS.md)): copy it,
do arithmetic with it (`s[i+1]`), take its address, or increment it. To get
another key type, range over a typed count: `for i := range uint64(len(s))`.

[Fixture](../examples/range-values/program.go) and validation: Check acceptance and
rejection tables, ordinary-Go/generated-C differential vectors (randomised plus
edge counts, empty views, nil pointers, write-during-range and panics), and real
SBF runs on `fogo-v0` and `solana-v3` (`TestRangeUint16SBF`, which skips when
platform-tools v1.57 or the managed runners are not installed).

## Error representation

The explicit error representation is an unsigned status code: zero means success,
and a nonzero handler status is returned unchanged by the adapter. The SDK's
encoded runtime/CPI statuses remain unchanged; an ordinary small handler code is
a Solana custom error. Built-in `error` (its `Error()` returns a string), named results/naked returns,
variadics and compound assignments remain unsupported. This is not an `error`
interface implementation. Runtime arithmetic/bounds failures still abort.

[Validation](../results/compiler/2026-10-05-control-values/README.md) includes
1,000 randomized ordinary-Go/generated-C vectors, 10 edge/error vectors, imported
multi-result calls and three actual-SBF runs of 141 scenarios with independent
native expectations. The [fixture](../examples/control-values/program.go) covers
receiver-free value updates, arrays in returned aggregates, source order, captures,
short-circuit cases, nested continue/break, bounds failures and rollback.

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" go test -ldflags=-linkmode=external \
  ./internal/compiler -run 'TestControl|TestImportedMultiple' -count=1
python3 scripts/control_verify.py --output results/compiler/new-controls
```

The experiment needs v1.51 LLVM and the pinned local SVM runner archive. Choose a
new output directory. This compiler fixture is separate from the bounded starter,
full token swap and dependency-heavy protocol workload.
[Constrained pointers/methods](GO_POINTERS.md) and [byte views](GO_VIEWS.md) are also
implemented. Schema-2/application authoring remains open in the [completion audit](MILESTONE2_ACCEPTANCE.md).
