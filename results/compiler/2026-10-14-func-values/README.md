# Function values, closures and defer on SBF: fogo-v0 and solana-v3

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_FUNCTIONS.md](../../../docs/GO_FUNCTIONS.md).

## Question

Do function values (declared functions, generic instances, method values and
expressions, literals capturing by reference), calls through them, Go 1.22
per-iteration loop variables, `defer` (ordering, evaluation time, named
results, loops) and named results behave on SBF exactly as in native Go; do
nil function calls abort and roll back; do closure environments, method
values and loop defer records consume exactly the documented heap; and what
does a call through a function value cost? Separately: would C function
pointers (`callx`) have worked instead of compare-and-branch dispatch?

## Method

`TestFuncValuesSBF` (`internal/compiler/funcs_sbf_test.go`) compiles
[examples/func-values](../../../examples/func-values/program.go) with a
Context entrypoint (account 0 is the 1 KiB output, instruction data is the
input) for each target with platform-tools v1.57 and the default 32 KiB heap,
and runs general fixtures through the pinned runners (fogo-v0:
gosvm-svm-runner 0.5.0 / LiteSVM 0.8.2; solana-v3: 0.6.0 / LiteSVM 0.13.0),
installed into a scratch `GOSVM_CACHE` from the local packages in
`results/svm/2026-10-05-runner-install-complete/` and
`results/svm/2026-10-09-runner-0.6.0-package/`:

- 324 vectors from `FuncVectorsForTest` covering all nine modes (captures,
  loop variables, method values, method expressions, containers and
  interfaces with nil calls, defer, generics, closure environments,
  higher-order functions) and every panic selector. Expected account bytes
  come from running the same `Run` natively; the 100 vectors that panic in Go
  (nil function calls, nil deferred values and interfaces, value methods
  through nil pointers, bounds failures, uncomparable comparisons, division by
  zero) must fail with `ProgramFailedToComplete`, log `SBF program panicked`
  once and roll back. Deferred calls in the fixture record into a log, never
  the output, because natively a panic runs them and on chain it does not.
- Heap boundaries (48 bytes moved before the loop, 32,760 usable): 2,044
  closures capturing one variable (16 bytes) fit and the 2,045th aborts; 1,022
  capturing three (32 bytes); 1,022 value-receiver method values of a 24-byte
  struct (32 bytes); 2,044 pointer-receiver method values (16 bytes); 1,363
  deferred calls with a `uint64` argument in a loop (24-byte records); 5,000
  declared-function values and capture-free literals allocate nothing.
- Call cost: a separate program builds 100 elements and runs
  `acc = step(acc, ks[j])` (direct), `acc = fs[j](acc, ks[j])` (a function
  value holding `step`, loaded per element), `acc = cs[j](acc)` (a closure
  capturing its `k`), or the body inline; n = 0 runs give the baseline.

```sh
GOSVM_CACHE=<scratch cache with both runners> GOSVM_FUNC_RESULTS=/tmp/func-results \
  go test -ldflags=-linkmode=external -run TestFuncValuesSBF ./internal/compiler/
```

The indirect-call experiment (a scratch test, not kept) compiled
`f := a` or `f := b` (selected by input) then `f(x)`, replaced the generated
dispatcher's body with a call through a `volatile` C function pointer (code
relocation) or a `const` table of function pointers (data relocation), linked
it exactly as `gosvm build` does and ran two fixtures per variant per target.
Fixtures, ELFs and runner reports stay local; [metrics.json](metrics.json)
keeps the summary.

## Results

All 336 cases pass on both targets (105 aborts with rollback: 100 Go panics
and 5 out-of-memory boundaries).

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Probe ELF SHA-256 | `e6519f9c…` | `fa1777bf…` |
| Highest vector CU | 8,899 | 9,120 |
| CU, 2,044 one-capture closures | 57,460 | 57,453 |
| CU, 1,363 deferred calls in a loop | 56,097 | 50,638 |
| CU per element, direct call `step(acc, k)` | 46.11 | 46.11 |
| CU per element, function value `fs[j](acc, k)` | 50.11 | 50.11 |
| CU per element, closure `cs[j](acc)` | 52.10 | 52.10 |
| CU per element, inlined body | 46.10 | 46.10 |

LLVM inlines the direct call, so it costs the same as the inlined body. A
call through a function value costs about 4 CU more (read the target ID,
compare, call), a closure about 6 (also load the captured variable's address
from the environment and the variable through it). The dispatchers here have a
single target; each further target of the called signature adds a compare.

Indirect calls (per transaction, `a` / `b`; identical on both targets):

| Variant | fogo-v0 | solana-v3 |
| --- | --- | --- |
| goSVM compare-and-branch dispatcher | 111 / 109 | 111 / 109 |
| `volatile` C function pointer (`callx`) | 117 / 115 | 117 / 115 |
| `const` function-pointer table (`callx`) | 120 / 118 | 120 / 118 |

All variants ran correctly. Clang emits `callx` with the address loaded by
`lddw`; fogo-v0 needs `R_SBF_64_RELATIVE` dynamic relocations (in code, and in
`.data.rel.ro` for the table), solana-v3 links absolute addresses without
relocations. Indirect calls work on both profiles, but compare-and-branch
dispatch was cheaper, needs no relocations on either target, and dispatches
over the same static target set the recursion check proves acyclic, so it was
kept ([why](../../../docs/GO_FUNCTIONS.md#dispatch)).

Host evidence: `TestFuncValuesDifferential` matches native Go on 13,932
vectors (4,463 panics) with the 32 KiB heap; `TestClosureEnvironmentHeap`
checks the boundaries above exactly. `TestAllocationSBF`, `TestInterfaceSBF`
and `TestMixedSBF` pass unchanged on both targets.

ELF hashes are snapshots of this commit's example and runtime. Programs without
function values are unchanged: the fresh-rebuild check
(`scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json`)
reproduces full-swap fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…`, and the
frozen corpus keeps every entry's C hash except 14 recorded function-value,
`defer` and named-result programs (listed in `corpusChanges`).
