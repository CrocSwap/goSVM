# Maps on SBF: fogo-v0 and solana-v3

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_MAPS.md](../../../docs/GO_MAPS.md).

## Question

Do maps (make with and without hints, literals, lookups, comma-ok lookups,
assignment, `++`/`--` and `+=` on elements, `delete`, `len`, `clear`, nil
maps, keys of every admitted comparable kind including interfaces, maps in
fields, slices, arrays, pointers, interfaces and generic code) behave on SBF
exactly as in native Go for every result that does not depend on iteration
order, do nil-map writes and unhashable interface keys abort and roll back,
does map growth consume exactly the documented heap bytes, and what does an
operation cost?

## Method

`TestMapSBF` (`internal/compiler/maps_sbf_test.go`) compiles
[examples/map-values](../../../examples/map-values/program.go) with a Context
entrypoint (account 0 is the 1 KiB output, instruction data is the input) for
each target with platform-tools v1.57 and the default 32 KiB heap, and runs
general fixtures through the pinned runners (fogo-v0: gosvm-svm-runner 0.5.0 /
LiteSVM 0.8.2; solana-v3: 0.6.0 / LiteSVM 0.13.0). Every transaction sets a
1,400,000 CU limit.

- 270 vectors from `MapVectorsForTest` (30 per mode): random operation
  scripts over 20 key kinds and 12 element kinds, aliasing and nil maps,
  unhashable interface keys, generics, large maps (up to 380 keys), heap use,
  forms and evaluation order, and compute probes. Expected account bytes come
  from running the same `Run` natively; the 37 vectors that panic in Go
  (nil-map writes, hashing an uncomparable dynamic type) must fail with
  `ProgramFailedToComplete`, log `SBF program panicked` once and roll back.
- 18 heap-boundary cases from the documented accounting (`mapHeapLimit`):
  `map[uint64]uint64` without a hint and with hints 1, 7, 384, 385, 768 and
  769, `map[string]struct{}` with concatenated keys, a slice of fresh maps,
  and an insert/delete loop that never grows. The predicted count fits; one
  more aborts with rollback (9 out-of-memory cases, one of them `make` with a
  hint of 769).
- 30 compute probes (mode 8): 0 and 256 operations of each kind on maps of 8,
  64 and 384 keys (string keys: 8 and 48), and the same loop without a map.

```sh
GOSVM_MAP_RESULTS=/tmp/map-results go test -ldflags=-linkmode=external \
  -run TestMapSBF ./internal/compiler/
```

(macOS needs the external linker flag; `make test` runs the test when the v1.57
backend and runners are installed and skips it otherwise. Here the runners were
installed from the 0.5.0 and 0.6.0 packages into a scratch `GOSVM_CACHE`.)
Fixtures, ELFs and runner reports stay local; [metrics.json](metrics.json)
keeps the summaries.

Host-side, `TestMapDifferential` compares 5,670 vectors (787 Go panics)
natively and as host-compiled C with the default heap, plus 37 large-map
vectors (up to 3,072 keys and 19,200-operation scripts) with a 256 KiB heap,
and checks that the 3,073rd key runs out of memory there; `TestMapHeap` checks
19 heap boundaries against the accounting.

## Results

All 318 cases pass on both targets (46 aborts with rollback: 37 Go panics and
9 out-of-memory boundaries).

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Probe ELF SHA-256 | `1c9d45e0…` | `4f2108e1…` |
| Highest vector CU | 116,253 | 109,128 |
| `map[uint64]uint64`, 384 keys fit (385th aborts) | 50,183 CU | 41,848 CU |
| Insert a new key (8 / 64 / 384 keys) | 68.8 / 71.2 / 62.6 | 69.8 / 72.2 / 63.6 |
| Lookup, present (8 / 64 / 384) | 57.0 / 60.1 / 60.4 | 57.0 / 60.1 / 60.4 |
| Lookup, absent (8 / 64 / 384) | 50.7 / 55.0 / 94.1 | 50.7 / 55.0 / 94.1 |
| `delete` + reinsert (8 / 64 / 384) | 149.9 / 187.3 / 309.2 | 150.9 / 188.3 / 310.2 |
| String-key lookup, present (8 / 48 keys) | 116.5 / 196.1 | 114.5 / 194.1 |
| Loop without a map | 8.0 | 8.0 |

CU per operation is (CU of 256 operations − CU of none) / 256 and includes
the loop and key computation (the last row). Maps of 384 keys are at load
factor 3/4, where linear-probing runs are longest: misses and deletes (which
shift the run back) cost more there.

ELF hashes are snapshots of this commit's example and runtime. Programs without
maps are unchanged: the fresh-rebuild check
(`scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json`)
reproduces full-swap fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…` and clock2
on both targets; `TestStringSBF` (`998161cc…`, `70174787…`),
`TestInterfaceSBF` (`c7daf643…`, `8e88eb9e…`; dispatch `06da97f1…`,
`7caa887b…`) and `TestAllocationSBF` (`21ac0e94…`, `4e6b6ee0…`; 64 KiB
`f1d481d8…`, `91005d64…`) reproduce their recorded probe ELFs, and
`TestMixedSBF` passes; the frozen corpus keeps every entry except its one map
program.
