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

`switch` supports scalar/bool tags, supported scalar-array tags, expressionless
conditions, init statements, multiple case expressions, and default anywhere in
the source. Case calls run in order and stop once a case matches. Struct comparisons,
type switches, `fallthrough` and labeled branches remain unsupported. An unlabeled
break exits the nearest switch or loop. An unlabeled continue targets the nearest
loop, including from a nested switch, and executes the loop post statement once.

The explicit error representation is an unsigned status code: zero means success,
and a nonzero handler status is returned unchanged by the adapter. The SDK's
encoded runtime/CPI statuses remain unchanged; an ordinary small handler code is
a Solana custom error. Built-in `error`, interfaces, named results/naked returns,
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
