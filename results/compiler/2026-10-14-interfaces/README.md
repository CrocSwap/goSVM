# Interface values on SBF: fogo-v0 and solana-v3

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_INTERFACES.md](../../../docs/GO_INTERFACES.md).

## Question

Do interface values (dynamic dispatch, boxing copies, type assertions, type
switches, comparisons, interfaces in fields, slices, arrays and behind pointers,
generic dynamic types) behave on SBF exactly as in native Go, do the three new
traps abort and roll back, does boxing consume exactly the documented heap
bytes, and what does a dynamic call cost?

## Method

`TestInterfaceSBF` (`internal/compiler/iface_sbf_test.go`) compiles
[examples/iface-values](../../../examples/iface-values/program.go) with a
Context entrypoint (account 0 is the 1 KiB output, instruction data is the
input) for each target with platform-tools v1.57 and the default 32 KiB heap,
and runs general fixtures through the pinned runners (fogo-v0:
gosvm-svm-runner 0.5.0 / LiteSVM 0.8.2; solana-v3: 0.6.0 / LiteSVM 0.13.0):

- 520 vectors from `IfaceVectorsForTest` covering all nine modes (dispatch,
  boxing, assertions, type switches, comparisons, nil calls, containers,
  generics, boxing loops), including every pair of the 12 dynamic types `pick`
  returns for assertions and comparisons and every nil-call combination.
  Expected account bytes come from running the same `Run` natively; the 180
  vectors that panic in Go (failed assertions, uncomparable comparisons, nil
  interface calls, value methods and fields through nil pointers) must fail
  with `ProgramFailedToComplete`, log `SBF program panicked` once and roll back.
- Boxing boundaries: 4,095 conversions of `uint64`, `int8` or `Sq` (8 bytes
  each) and 1,365 of `[3]uint64` (24 bytes) fit the 32,760 usable bytes; one
  more aborts with rollback; 5,000 conversions of a pointer or an empty struct
  allocate nothing and complete.
- Dispatch cost: a separate program builds `ts []T` and `is []I` of 100
  elements (each `T` boxed) and runs `acc = ts[j].M(acc)` (direct),
  `acc = is[j].M(acc)` (dynamic; the type ID is loaded per element), or the
  method body inline; n = 0 runs give the baseline.

```sh
GOSVM_IFACE_RESULTS=/tmp/iface-results go test -ldflags=-linkmode=external \
  -run TestInterfaceSBF ./internal/compiler/
```

(macOS needs the external linker flag; `make test` runs the test when the v1.57
backend and runners are installed and skips it otherwise.) Fixtures, ELFs and
runner reports stay local; [metrics.json](metrics.json) keeps the summary.

## Results

All 530 cases pass on both targets (184 aborts with rollback: 180 Go panics and
4 out-of-memory boundaries).

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Probe ELF SHA-256 | `c7daf643…` | `8e88eb9e…` |
| Highest vector CU | 29,411 | 30,348 |
| CU, 4,095 `uint64` conversions | 98,427 | 102,515 |
| CU, 1,365 `[3]uint64` conversions | 42,462 | 43,820 |
| CU per element, direct call `ts[j].M` | 38.14 | 38.14 |
| CU per element, dynamic call `is[j].M` | 43.15 | 43.15 |
| CU per element, dynamic call, alternating `T`/`U` | 41.65 | 41.65 |

Per element the measurement includes building both slices and boxing; the
difference between the dynamic and direct rows, about 5 CU, is the cost of a
dynamic call with two implementing types (load the type ID and the boxed
receiver, compare, branch). The alternating row runs `U.M`, a cheaper body,
for half the elements, behind the second ID test. LLVM inlines the
dispatcher here; with a loop-invariant interface value it also hoists the type
test out of the loop (an earlier variant of the probe measured 6.02 against
4.02 CU per iteration).

ELF hashes are snapshots of this commit's example and runtime. Programs without
interface values are unchanged: the fresh-rebuild check
(`scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json`)
reproduces full-swap fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…`, and the
frozen corpus keeps every non-interface entry's C hash.
