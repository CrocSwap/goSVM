# Compile-time evaluation of package initialization (2026-10-16)

Question: can goSVM run package initialization once, at compile time,
instead of on every invocation, without changing what any program observes?
The [real-libraries results](../2026-10-16-real-libraries/README.md#initialization)
measured 25,442 CU of initialization per invocation for a program importing
uniswapv3-sdk-uint256's `utils`, more than a swap step.

Design: [GO_GLOBALS.md](../../../docs/GO_GLOBALS.md#compile-time-evaluation);
backend view: [BACKEND_INTERFACE.md](../../../docs/BACKEND_INTERFACE.md#compile-time-evaluated-initialization);
contract: [ERROR_CONTRACT.md](../../../docs/ERROR_CONTRACT.md#contracted-runtime-behaviour)
(changelog 2026-10-16, announce to Qushta). Compiler: this branch (parent
`7bee8b0`); Go 1.22.0; platform-tools v1.57; runners 0.5.0 (fogo-v0) and
0.6.0 (solana-v3) from the retained packages, installed into a scratch
`GOSVM_CACHE`. Workloads are named exactly; the real libraries are the
trimmed module copies of `examples/real-libraries` (same modules and
versions as the earlier results).

## Summary

- **Check evaluates the longest prefix of initialization** (each package's
  variable-initializer statements, then its `init` functions, in Go's
  order) that completes without a trap or panic, an SDK or native-library
  call, a map keyed by pointers or interfaces, or more than 1,048,576
  interpreter steps. The rest runs on every invocation as before.
- **The result is a heap image, installed at entry** (`sol_memcpy_`, then
  stores of pointers to read-only data). Objects keep the offsets the
  run-time allocations would have had, so **heap accounting is unchanged**
  and nothing but CU and ELF bytes is observable. Moving never-written
  objects to read-only data was rejected: it would change heap budgets (a
  contract change) to save a few CU (the `utils` image installs in 114 CU,
  most of it storing the pointers to the sentinel errors' message literals).
- **CU:** `utils` initialization 25,442 → **114** (fogo-v0) and 25,445 →
  **114** (solana-v3); a `ComputeSwapStep` transaction 45,063 → **18,439**.
- **Correctness:** `examples/const-init` and the earlier fixtures match
  fresh native Go processes on the host and on both targets, with and
  without compile-time evaluation; the live heap after initialization is
  word-for-word the run-time one (map tables aside, whose layout depends
  on insertion history) for five programs.
- **No other change:** the full-swap ELFs rebuild byte-identically
  (`a66c5a97…`, `b419245b…`; [rebuild/metrics.json](rebuild/metrics.json));
  only the corpus's two empty-`init` programs' C changed (the call is gone).

## Initialization CU per invocation

Idle invocation minus an empty program (97 CU), both ways, measured in this
run by `TestRealLibrariesInitSBF` ([fogo-v0](fogo-v0/real-libraries.json),
[solana-v3](solana-v3/real-libraries.json)); `runtime-init` is
`Program.RuntimeInit`, the previous behaviour:

| Imports | fogo-v0 runtime-init | fogo-v0 image | solana-v3 runtime-init | solana-v3 image |
| --- | ---: | ---: | ---: | ---: |
| `uint128` | 15 | 7 | 15 | 7 |
| `int256` | 434 | 52 | 434 | 52 |
| `uint256` | 484 | 67 | 484 | 67 |
| `constants` (and `uint256`) | 3,706 | 67 | 3,706 | 67 |
| **`utils`** (all four packages) | **25,442** | **114** | **25,445** | **114** |
| an app using only the tick math | 26,276 | 114 | 26,281 | 114 |

The runtime-init numbers reproduce the earlier results exactly. The image
cost is the copy (at least 10 CU; one per 250 bytes beyond 2,500) plus
about 3 CU per pointer into read-only data (string literals of the
sentinel errors); `utils`' image is 4,032 bytes of heap. Images of up to
four words are stored directly (`uint128`: 7 CU).

Example programs (`TestConstInitSBF`, `TestGlobalsSBF`):

| Program | fogo-v0 runtime-init | fogo-v0 image | solana-v3 runtime-init | solana-v3 image |
| --- | ---: | ---: | ---: | ---: |
| `examples/const-init` (39 of 42 steps evaluated) | 9,986 | 773 | 9,415 | 776 |
| `examples/global-values` (42 of 42) | 2,179 (historical) | 244 | 2,183 (historical) | 244 |

## Whole transactions (examples/real-libraries)

Total CU of one bench vector of the full example (decode, call, write),
`TestRealLibrariesInitSBF`; outputs match native Go both ways:

| Vector | fogo-v0 runtime-init | fogo-v0 image | solana-v3 runtime-init | solana-v3 image |
| --- | ---: | ---: | ---: | ---: |
| idle (initialization only) | 26,997 | 373 | 27,019 | 374 |
| none (decode and write only) | 27,864 | 1,240 | 27,868 | 1,223 |
| `GetSqrtRatioAtTickV2` (tick 1 / 500000 / MinTick) | 30,311 / 33,116 / 32,601 | 3,687 / 6,492 / 5,977 | 30,312 / 33,117 / 32,605 | 3,667 / 6,472 / 5,960 |
| `GetTickAtSqrtRatioV2` (price 1 / ~500000 / max) | 35,878 / 40,392 / 41,815 | 9,254 / 13,768 / 15,191 | 35,741 / 40,238 / 41,629 | 9,096 / 13,593 / 14,984 |
| **`ComputeSwapStep`** exact in, within the range | **45,063** | **18,439** | **45,036** | **18,391** |
| `ComputeSwapStep` exact in, reaching the target | 38,329 | 11,705 | 38,317 | 11,672 |
| `ComputeSwapStep` exact out | 45,708 | 19,084 | 45,676 | 19,031 |

The per-call costs of the earlier table are unchanged; initialization
drops out.

## ELF sizes

| Program | fogo-v0 runtime-init | fogo-v0 image | solana-v3 runtime-init | solana-v3 image |
| --- | ---: | ---: | ---: | ---: |
| the example | 296,480 | 279,760 | 302,152 | 285,840 |
| app using only `ComputeSwapStep` | 70,920 | 46,872 | 70,040 | 46,640 |
| imports `utils`, calls nothing | 34,632 | 8,992 | 32,592 | 7,616 |

The parsers and initializer code of evaluated steps are no longer linked
when nothing else calls them; the image data is smaller.

## Correctness

| Run | Size | Result |
| --- | --- | --- |
| `TestConstInitDifferential` (host C, image and runtime-init, vs one fresh native process per vector) | 1,022 vectors, 551 Go panics | all match |
| `TestConstInitSBF` (fogo-v0 and solana-v3, both ways) | 122 cases per target and mode (55 aborts) | all match |
| `TestInitImageHeapAccounting` (host) | const-init, global-values, global-value-types, stdlib-values, real-libraries | same in-use count; every live word of the block and image objects identical to running every initializer (map slot arrays excepted) |
| `TestInitEvalStops`, `TestInitImageCheckedProgram`, `TestInitEvalPartialPackage`, `TestInitEvalRuntimeInit` | 11 stop programs plus structure checks | each stop reason as specified; same output both ways |
| `TestInitEvalEdgesSBF` ([edges](fogo-v0/edges.json)) | panic after an evaluated prefix; 64 KiB heap budget with and without a heap frame | same log line `panic: too big`; same access violation at `0x30000ffff` without a frame, both ways |
| Existing suites with images | `TestGlobalsDifferential` (2,042), `TestGlobalValueTypesDifferential`, `TestGlobalInitPanics`, `TestStdlibDifferential`, `TestRealLibrariesDifferential` (22,126), all 25 SBF suites on both targets | all pass, none skipped |

`examples/const-init` covers computed constants parsed from text (with a
`MustParse` that panics on bad input), one object held by two variables,
by a map and by a slice; a slice of and a pointer into a package array;
maps built, grown and shrunk at initialization (string, struct and integer
keys, function-valued elements); closures over heap-moved locals; method
values with pointer and value receivers; sentinel errors through
`errors.Join`, `errors.Is` and `errors.As`; interface values of each
representation (boxed, pointer, zero-size); strings concatenated and
converted at initialization; and a pointer-keyed map that ends the
evaluated prefix, after which an `init` function changes evaluated
variables at run time. Its mutation scripts then append to, grow, write
through and delete from the installed state.

Build time: Check on the real-libraries example took 200–270 ms with and
without evaluation on a loaded machine; interpreting `utils`' 71 steps
takes 13,578 interpreter steps, within that noise.

## With opt-in trimming

Rebased together with [opt-in trimming](../2026-10-16-trim-opt-in/README.md),
`TestRealLibrariesSBF` runs the example both from the trimmed copies and
from the unmodified modules under the opt-in: both forms cost idle 373 /
374 CU and `ComputeSwapStep` 18,439 / 18,391 on fogo-v0 / solana-v3; the
opt-in's `utils` initialization probe is 19 CU (it drops unreferenced
initializers). Summaries: [with-trim](with-trim).

## Reproduction

```sh
(cd examples/real-libraries && GOWORK=off go mod download)
GOSVM_CACHE=<scratch> build/gosvm runner install -archive <0.5.0 package>   # and the 0.6.0 package
GOSVM_CACHE=<scratch> GOSVM_CONSTINIT_RESULTS=<dir> go test -ldflags=-linkmode=external \
  -run 'TestConstInit|TestInitEval|TestInitImage|TestRealLibrariesInitSBF' -count=1 ./internal/compiler
python3 scripts/verify_rebuild.py --output <new dir>
```

The JSON files here are the tests' summaries; the ELFs, fixtures and runner
reports they keep are local-only.
