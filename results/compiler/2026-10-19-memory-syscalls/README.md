# Bulk memory moves on the VM's memory syscalls

October 19, 2026. Local only (`network_validation: false`); runners 0.5.1
(fogo-v0, LiteSVM 0.8.2) and 0.6.0 (solana-v3, LiteSVM 0.13.0),
platform-tools v1.57. Design and contract:
[GO_ALLOCATION.md § Bulk memory on SBF](../../../docs/GO_ALLOCATION.md#bulk-memory-on-sbf).

## Why

goSVM moved bytes with C loops: `gosvm_move` (copy, append, strings) and the
hidden `memcpy`/`memset`/`memmove` that LLVM calls for aggregate assignment
and zeroing. On SBF a byte loop costs about 8 CU per byte and the aligned word
loop 6 CU per 8 bytes; the VM's `sol_memmove_`/`sol_memset_` cost
`max(10, n/250)` CU. Data-moving instructions cost up to 2× Rust (Hyperlane
mailbox Init: 25,182 CU in Go, 12,610 in Rust).

## What changed

On fogo-v0 and solana-v3 (SBF builds only; `-DGOSVM_MEMORY_SYSCALLS`), every
move of `n >= 1` bytes in these routines calls the syscall:

| Construct | Routine | Syscall |
| --- | --- | --- |
| `copy` (any element type, `copy(b, s)` from a string) | `gosvm_move` | `sol_memmove_` |
| `append` growth and spreads (`append(s[:i], s[j:]...)`) | `gosvm_move` | `sol_memmove_` |
| string concatenation, `string(b)`, `[]byte(s)` | `gosvm_move` | `sol_memmove_` |
| struct/array assignment, parameter copies (≥ 48 B; LLVM inlines smaller) | `memcpy`, `memmove` | `sol_memmove_` |
| zero-value assignment and zeroed locals (≥ 96 B) | `memset` | `sol_memset_` |
| `for i := range s { s[i] = 0 }`, `for j := e; j < uint64(len(s)); j++ { s[j] = 0 }` | emitted as `memset` | `sol_memset_` |
| duplicate account descriptor at entry (56 B) | entrypoint | `sol_memmove_` |
| `ResizeAccount` growth zero fill | SDK runtime | `sol_memset_` |

`sol_memcpy_` is never used for these: the runtime fails it with
`CopyOverlapping` on any overlap, including `src == dst`, which Go's `copy`
and LLVM's `memcpy` both allow. Same cost, so `sol_memmove_` everywhere.
`n == 0` (and `copy` onto itself) returns before any syscall, so nil/empty
slices never reach the VM. Host C and target-less legacy v1.51 builds compile
the original loops (`#else` branches; the legacy ELF is identical for every
threshold). The ELF on fogo-v0 now imports `sol_memmove_` and `sol_memset_`
(admitted in `legacySyscalls`); solana-v3 uses static hashes `0x434371f8` and
`0x3770fb22`.

## Threshold (measured)

[threshold-curves.json](threshold-curves.json): whole-transaction CU of one
`copy` of `n` bytes in `examples/memory-values` (mode 0), loops-only build vs
threshold 1 (selected rows):

| bytes | fogo-v0 aligned loop / syscall | unaligned loop / syscall | solana-v3 aligned | unaligned |
| ---: | ---: | ---: | ---: | ---: |
| 0 | 201 / 191 | 201 / 191 | 202 / 178 | 202 / 178 |
| 1 | – | 219 / 207 | – | 219 / 193 |
| 8 | 219 / 207 | 275 / 207 | 220 / 193 | 275 / 193 |
| 32 | 237 / 207 | 467 / 207 | 238 / 193 | 467 / 193 |
| 128 | 309 / 207 | 1,235 / 207 | 310 / 193 | 1,235 / 193 |
| 1,024 | 981 / 207 | 8,403 / 207 | 982 / 193 | 8,403 / 193 |

The syscall wins from 1 byte at every alignment, so the threshold is 1. With
it LLVM deletes the loops on SBF, which shrinks the routines enough to inline
(the "0 bytes" row is cheaper too). A mid threshold (64) was also measured: it
costs more than threshold 1 in total (fogo-v0 4,399,862 vs 3,814,391 CU over
the 5,480 vectors) and is kept only as a test variant. Against loops only, the
threshold-1 build saves 70% of total CU (fogo-v0 12,847,981 → 3,814,391;
solana-v3 13,238,668 → 3,869,125) and is worse on 33 (fogo-v0) and 35
(solana-v3) of 5,480 vectors: by at most 97 CU on fogo-v0 (empty-string
concatenation) and 1,006 CU on solana-v3 (mode 1, a 1,029-byte heap fill loop
in the same function, not a memory move, compiled worse after the smaller
routines changed LLVM's inlining). Those are second-order code-layout
effects; the Hyperlane and example fixtures have no step that got worse.

## CU before / after

### Hyperlane Go mailbox

`hyperlane-monorepo` `sealevel` at `665a68de7c` (copied; read-only source),
built with the integration CLI `843b1e0` and with `843b1e0` + this change,
fixtures `programs/mailbox/run-svm-fixtures.sh` (64 steps per profile; all
pass). Full table: [mailbox-cu.md](mailbox-cu.md).

| step | fogo-v0 before | after | solana-v3 before | after | Rust (fogo-v0) |
| --- | ---: | ---: | ---: | ---: | ---: |
| Init | 25,182 | 13,372 | 25,018 | 13,258 | 12,610 |
| Dispatch, empty body | 17,557 | 12,987 | 17,421 | 12,843 | 36,810 |
| Dispatch, 512-byte body | 26,029 | 13,272 | 25,893 | 13,128 | 82,969 |
| Process (default ISM) | 32,081 | 25,389 | 31,746 | 25,055 | 42,517 |
| Transfer ownership (tail zeroing) | 13,367 | 3,887 | 13,364 | 3,887 | 5,011 |
| all 64 steps | 1,047,475 | 696,597 (−33.5%) | 1,041,281 | 691,020 (−33.6%) | 2,041,407 |

Every step got cheaper (−11 to −12,765 CU). ELF: fogo-v0 60,504 → 51,872
bytes, solana-v3 58,696 → 48,360.

### goSVM examples

Each project copied, legacy configs migrated to `fogo,solana`, `gosvm test
--svm` (and the full swap's lifecycle fixtures); every fixture passes before
and after.

| example | profile | fixtures | steps | total CU before | after | change | worst step | ELF before → after |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| tutorial | fogo-v0 | svm | 9 | 65,784 | 57,237 | −13.0% | −6 | 16,000 → 15,704 |
| tutorial | solana-v3 | svm | 9 | 65,568 | 57,021 | −13.0% | −6 | 14,824 → 14,408 |
| escrow | fogo-v0 | svm | 42 | 351,802 | 308,135 | −12.4% | −12 | 26,384 → 26,096 |
| escrow | solana-v3 | svm | 42 | 350,742 | 307,075 | −12.4% | −12 | 25,160 → 24,744 |
| typed-swap | fogo-v0 | svm | 14 | 4,341 | 4,281 | −1.4% | 0 | 4,064 → 4,032 |
| typed-swap | solana-v3 | svm | 14 | 4,341 | 4,281 | −1.4% | 0 | 3,216 → 3,136 |
| full swap | fogo-v0 | svm | 15 | 405,676 | 364,953 | −10.0% | −32 | 68,808 → 68,528 |
| full swap | fogo-v0 | lifecycle | 36 | 1,512,713 | 1,353,047 | −10.6% | −32 | 68,808 → 68,528 |
| full swap | solana-v3 | svm | 15 | 355,330 | 314,607 | −11.5% | −32 | 69,200 → 68,800 |
| full swap | solana-v3 | lifecycle | 36 | 1,507,727 | 1,348,061 | −10.6% | −32 | 69,200 → 68,800 |

### Patterns from the Hyperlane ports

[patterns.go.txt](patterns.go.txt) on a 1 KiB owned account (identical on both
profiles):

| case | before | after |
| --- | ---: | ---: |
| empty instruction | 112 | 107 |
| zero 1,024-byte tail, `for j := off; j < uint64(len(data)); j++` | 5,232 | 124 |
| zero 1,024-byte tail, `for i := range tail` | 5,238 | 127 |
| zero 512-byte tail (either form) | 2,672 / 2,678 | 124 / 127 |
| `copy` 32 bytes, unaligned | 389 | 137 |
| `copy` 32 bytes, aligned | 138 | 134 |
| hand-written 32-byte copy loop (not rewritten) | 468 | 463 |
| `*p = Big{}` on 512 bytes, minus the same without it | 2,567 | 19 |

Hand-written copy loops (`for j := ...; j < n; j++ { d[o+j] = s[j] }`, as in
the SDK snapshot's `cpi`/`pda`/`system` builders) are not rewritten: a forward
byte loop over overlapping ranges is not a memmove. Use `copy`.

### SDK runtime

`TestMemoryRuntimeSBF` (both profiles, identical): growing a 200-byte account
to 10,440 bytes 53,529 → 941 CU; a duplicate account descriptor 1,329 → 888 CU
for the probe (about 440 CU per duplicate).

## Semantics evidence

- `examples/memory-values` against native Go (`memops_diff_test.go`,
  `memops_sbf_test.go`): 13,660 vectors as host C, for SDK 1 and SDK 2
  routines; 5,480 vectors (142 aborts with rollback) on each SBF profile,
  each built three ways (threshold 1, threshold 64, loops only); all match.
  Covers copy overlapping both ways, exact aliasing, zero length, nil, short
  sources, heap/string/read-only sources, six element types, append deletes,
  inserts and growth, aliasing struct and array assignment and swaps through
  pointers, zeroing, string building, both zeroing-loop forms and near misses
  (signed start that traps, nonzero store, value variable), and counts around
  each threshold (0–3, 7–9, 15–17, 63–65, 127–129, 255–257, up to 1,500).
- `TestMemoryRuntimeSBF`: duplicate descriptors and `ResizeAccount` zero
  fill after a shrink (stale bytes stay zero), to the 10 KiB limit and past it
  (error 2010), chosen vs loops.
- `TestMemoryReadOnlyWritesSBF`: `copy` into a read-only account fails with
  `ReadonlyDataModified` and into an account key succeeds without effect,
  syscall and loop alike, on both profiles.
- `TestMemoryLegacyUnchanged`: the legacy v1.51 ELF is byte-identical for
  thresholds 1, 64 and loops only. Against the previous compiler (`9ba2da9`),
  checked by hand: the target-less full swap builds to the same legacy ELF
  (`05f52dd2…`), and so does `examples/memory-values` on `legacy-v1.51-v3`
  (`6ad52755…`); its emitted C with the syscall blocks removed is the
  previous compiler's C (every new path, including both zeroing loops).
- `TestCorpusMatchesPreCheckCompiler`: every frozen corpus entry's C equals
  the recording once the added `#ifdef GOSVM_MEMORY_SYSCALL_*` blocks and
  declarations are removed (`withoutMemorySyscalls`); no hash was re-recorded.
- `TestZeroLoopForms` lists the rewritten and non-rewritten loop forms;
  `TestMemmoveSBF` (self-assignment through aliasing pointers) still passes.
- [rebuild/metrics.json](rebuild/metrics.json): fresh `-no-cache` rebuilds
  identical and clean; new reference ELFs: full swap fogo-v0 `9813e0d1…`
  (68,528 B), solana-v3 `94862a66…` (68,800 B); `clock2` fogo-v0 `d85d909e…`,
  solana-v3 `88611a50…`.
- Root suite (`go test ./...` with Go 1.22.0): see the handoff entry.

## Risks

- Only local runners: Fogo (Firedancer) is assumed to charge memory syscalls
  like Agave (`max(10, n/250)`), not measured here.
- Under direct mapping the syscalls translate whole ranges; a range spanning
  two regions would fail where a loop would fault at the boundary. Go slices
  never span regions, and both fail the transaction.
- Zero-length moves never reach the VM; a nil pointer with nonzero length is
  impossible in Go.
- The zeroing-loop rewrite is a pattern match in the backend; forms outside
  the two listed compile as written.
- Smaller, inlinable memory routines change LLVM's inlining and register
  allocation of surrounding code; in 35 of 5,480 probe vectors that made
  unrelated code up to 1,006 CU slower on solana-v3 (see Threshold). No
  Hyperlane or example step regressed.

## Reproduce

```sh
GOSVM_MEMORY_RESULTS=/tmp/mem go test -ldflags=-linkmode=external \
  -run 'TestMemory|TestZeroLoopForms|TestMemmove' ./internal/compiler
python3 scripts/verify_rebuild.py --output <new dir> --cli <gosvm>
```

`/tmp/mem/<profile>/case-cu.json` holds per-vector CU for the three builds.
Mailbox and example measurements: build the CLI before and after, copy each
project, run its fixtures with each CLI and compare the reports'
per-step `cu` (numbers above in [metrics.json](metrics.json)).
