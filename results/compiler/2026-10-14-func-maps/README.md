# Maps with function values, and strings with function values, on SBF

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_FUNCTIONS.md](../../../docs/GO_FUNCTIONS.md#with-maps),
[GO_MAPS.md](../../../docs/GO_MAPS.md#with-function-values).

## Question

After merging maps with function values, closures, `defer` and named
results: do the two combine with Go's results (closures capturing maps,
maps of function values, function types over maps, deferred calls changing
maps and named map results, method values of map-backed types, generics),
with the documented heap costs and traps, on the host and on SBF? And does the
earlier strings-with-function-values fixture, until now checked only on the
host, hold on SBF too?

## Method

- `TestFuncMapsDifferential` (`internal/compiler/funcmaps_diff_test.go`)
  runs [examples/func-maps](../../../examples/func-maps/program.go)
  natively and as host-compiled C (32 KiB heap): 9,060 vectors over six
  modes, 2,487 of which panic in Go (nil-map writes in closures and deferred
  calls, unhashable keys including a function value as an `any` key, calls of
  the nil function value an absent key yields, value methods through nil
  pointers, uncomparable keys) and must abort at the same point.
- `TestClosureMapHeap`: exact out-of-memory boundaries with 280 bytes
  allocated before the loop (Run's 32-byte `W`; the captured map's moved
  8-byte reference, its 24-byte header and 8 slots of 24 bytes; a 24-byte
  `Counter{}` header): 676 closures each capturing a fresh map (8 + 24 + 16
  bytes) fit and the 677th aborts; 2,030 closures capturing an outer map
  (16); 1,015 loop `defer touch(m, 0)` records (16 + 8 + 8; the deferred
  writes hit an existing key, so they never grow the table); 2,030 method
  values of a map-backed type (8 + 8); capture-free literals allocate nothing.
- `TestFuncCombinationsSBF` (`internal/compiler/funccombos_sbf_test.go`)
  builds both fixtures for fogo-v0 and solana-v3 (platform-tools v1.57,
  default heap) and runs them through the pinned runners (fogo-v0:
  gosvm-svm-runner 0.5.0; solana-v3: 0.6.0), installed into a scratch
  `GOSVM_CACHE` from `results/svm/2026-10-05-runner-install-complete/` and
  `results/svm/2026-10-09-runner-0.6.0-package/`. Expected account bytes come
  from running the same `Run` natively; Go panics and out-of-memory cases must
  fail with `ProgramFailedToComplete`, log `SBF program panicked` once and
  roll back.

```sh
GOSVM_CACHE=<scratch cache with both runners> GOSVM_COMBO_RESULTS=/tmp/combo \
  go test -ldflags=-linkmode=external -run 'TestFuncMapsDifferential|TestClosureMapHeap|TestFuncCombinationsSBF' ./internal/compiler/
```

## Results

| Fixture | Cases per target | Aborts with rollback | fogo-v0 ELF | solana-v3 ELF |
| --- | --- | --- | --- | --- |
| func-strings | 311 (300 vectors, 11 heap cases) | 118 (113 Go panics, 5 out of memory) | `e2e63d72…` | `7cf14582…` |
| func-maps | 369 (360 vectors, 9 heap cases) | 118 (114 Go panics, 4 out of memory) | `99662e27…` | `280a454c…` |

Every case matched native Go and the heap model on both targets. Full
hashes are in [metrics.json](metrics.json).

The merge left every earlier recorded probe ELF unchanged (allocation,
interfaces, strings, function values, maps), and the full-swap ELFs stay
fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…`. Fixtures, ELFs and runner
reports stay local.

The local runners are not a public network; see
[target profiles](../../../docs/TARGET_PROFILES.md).
