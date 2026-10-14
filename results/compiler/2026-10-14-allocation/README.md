# Invocation-heap allocation on SBF: fogo-v0 and solana-v3

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_ALLOCATION.md](../../../docs/GO_ALLOCATION.md).

## Question

Do `new`, `make`, `append`, `copy`, slice literals, typed slices, pointer and
slice fields and heap-moved locals behave on SBF exactly as in native Go, and do
the heap budget, its exact out-of-memory boundary and `RequestHeapFrame` behave
as documented?

## Method

`TestAllocationSBF` (`internal/compiler/alloc_sbf_test.go`) compiles
[examples/alloc-values](../../../examples/alloc-values/program.go) with a
Context entrypoint (account 0 is the 1 KiB output, instruction data is the
input) for each target with platform-tools v1.57, twice: the default 32 KiB
budget and `heap_bytes` 65536. It runs general fixtures through the pinned
runners (fogo-v0: gosvm-svm-runner 0.5.0 / LiteSVM 0.8.2; solana-v3: 0.6.0 /
LiteSVM 0.13.0):

- 102 vectors from `AllocVectorsForTest` (every mode: append growth for 18
  element kinds, make/new zero values and make traps, copy, slicing bounds,
  linked structures, escaping locals, literals, aliasing). Expected account
  bytes come from running the same `Run` natively; the 14 vectors that panic in
  Go must fail with `ProgramFailedToComplete`, log `SBF program panicked` once
  and roll back. Before running, the test checks on the host that each vector
  fits a 32 KiB heap.
- 32 KiB boundary: `make([]byte, 32760)` plus a write to its last byte succeeds;
  32,761 and 40,000 bytes abort.
- 64 KiB build: with `RequestHeapFrame(65536)` 65,528 and 40,000 bytes succeed
  and 65,529 aborts; a 128 KiB frame also works; with no frame or a 48 KiB frame
  the first allocation faults (`Access violation in heap section at address
  0x30000ffff of size 1`, checked by log count).

Every case asserts the exact error, the account state and the log line.

```sh
GOSVM_ALLOC_RESULTS=/tmp/alloc-results go test -ldflags=-linkmode=external \
  -run TestAllocationSBF ./internal/compiler/
```

(macOS needs the external linker flag; `make test` runs the test when the v1.57
backend and runners are installed and skips it otherwise.) Fixtures, ELFs and
runner reports stay local; [metrics.json](metrics.json) keeps the summary.

## Results

All 111 cases pass on both targets.

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| 32 KiB probe ELF SHA-256 | `21ac0e94…` | `4e6b6ee0…` |
| 64 KiB probe ELF SHA-256 | `f1d481d8…` | `91005d64…` |
| CU, `make([]byte, 32760)` + write | 175 | 169 |
| CU, 64 KiB frame + `make([]byte, 65528)` (includes the ComputeBudget instruction and heap cost) | 336 | 331 |
| Highest vector CU | 28,514 | 28,506 |

A heap access violation consumes the transaction's remaining compute
(200,000 CU here), unlike the abort trap.

ELF hashes are snapshots of this commit's example and runtime. Programs without
allocation are unchanged: the fresh-rebuild check
(`scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json`)
reproduces full-swap fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…`.
