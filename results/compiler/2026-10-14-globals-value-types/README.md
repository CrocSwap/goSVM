# Package-level variables with value types — October 14, 2026

This merge combines two features developed in parallel from `764e4a9`:
[package-level variables and `init`](../../../docs/GO_GLOBALS.md) (`852c980`,
[results](../2026-10-14-globals/README.md)) and
[struct `==`, type aliases, arrays of composite elements and `panic`](../../../docs/GO_TYPES.md)
(`7837ba2`, [results](../2026-10-14-value-types/README.md)). Local runners only;
no public network.

## What the merge adds

- **Large package-level tables.** The 1,024-byte per-value array limit
  protects SBF stack frames. A package-level variable is never in a frame
  (it is read-only data or in the globals block), so Check now admits an
  array type above the limit when no value of the type is ever copied: only
  package-level variables have it, and they are only indexed, passed to
  `len`/`cap` or ranged over by index; a read-only variable's literal is
  constant data ([large tables](../../../docs/GO_GLOBALS.md#large-tables)).
  This admits holiman/uint256's `var pows = [60]Int{...}` (1,920 bytes).
  Everything else keeps the same rejection at the same position.
- **Combination evidence**: `examples/global-value-types` (two packages, with
  a native `oracle`), the initialization-panic probes and the large-table
  forms, below.
- [ERROR_CONTRACT](../../../docs/ERROR_CONTRACT.md): a panic during
  initialization logs its line and aborts before `Process`; the panic
  section gained a backend-reproduction subsection (requested by Qushta).

## Method

- `TestGlobalValueTypesDifferential`: 1,402 vectors, each against a fresh
  native process (`examples/global-value-types/oracle`) and host-compiled C
  (32 KiB heap); output, result, panic and the panic's log line must match.
  The example covers read-only and writable arrays of structs and arrays,
  three tables above the per-value limit (`pows [60]Int`, a 4,800-byte
  read-only `[300]Pair`, a 5,120-byte writable `[160]Int`), `G[i].F = v`,
  `G[i][j]++` and `pkg.G[i].F -= x`, struct and array `==`/`!=` and `switch`
  on globals, globals compared through interfaces, alias-typed variables
  (`type Int = wide.Int`, `type Grid = [4][4]uint64`) and a function that
  initializers call and `Run` makes panic (`panic: wide.Limit(…)`).
- `TestGlobalInitPanics`: eight programs whose initialization panics (a
  variable initializer calling a panicking function, a named value in
  `init`, a signed value after composite-array writes, `panic(q == o)`, an
  alias-typed table checked by an initializer, an imported package's
  initializer, `panic(nil)`, and an index-out-of-range runtime error). Each
  is built natively as its own module (exit status 2; the first stderr line
  is the contracted line, or `panic: runtime error: …` for the trap) and as
  host C (the same line, no line for the trap, and an abort with
  `Process`'s output untouched).
- `TestGlobalValueTypesSBF`: 151 of the vectors plus an idle invocation, and
  the eight probes, on fogo-v0 and solana-v3 (platform-tools v1.57; runners
  0.5.0 and 0.6.0 installed from the retained packages into a scratch
  `GOSVM_CACHE`; 1,400,000 CU). Completions must leave native bytes; panics
  must fail with `ProgramFailedToComplete`, log `SBF program panicked` once,
  roll back and log `Program log: <line>` exactly once.
- `TestLargeTablesAccepted` (4 forms run on the host) and
  `TestLargeTablesRejected` (16 forms), `TestGlobalValueTypesPlacement`
  (read-only placement of the example's tables) and a features test
  (`internal/features`) for a program using both sides' flags.

```sh
GOSVM_CACHE=<scratch cache with both runners> GOSVM_GLOBAL_RESULTS=<dir> \
  go test -ldflags=-linkmode=external \
  -run 'GlobalValueTypes|GlobalInitPanics|LargeTables' -v ./internal/compiler/
GOSVM_CACHE=<scratch cache> make test
python3 scripts/verify_rebuild.py --output <new dir> \
  --reference results/compiler/2026-10-14-v157-rebuild/metrics.json
```

## Results

All pass. Host: 1,402 vectors match fresh native processes (112 Go panics:
5 table-index runtime errors, 107 in mode 5, of which 83 are `panic` calls
whose line matches and 24 uncomparable-interface runtime errors); the eight
initialization-panic probes match native Go.

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Example ELF SHA-256 | `157aed94…` | `95b85844…` |
| Cases (aborts with rollback / panic lines) | 152 (11 / 8) | 152 (11 / 8) |
| Idle invocation (initialization + entry) | 632 CU | 632 CU |
| Highest completed vector | 11,967 CU | 11,967 CU |
| Initialization-panic probes | 8 of 8 | 8 of 8 |

Validation of the merged tree: `make test` passed for every root package
with both runners installed; all 18 SBF and rebuild tests of
`internal/compiler` (50 tests and subtests) passed on both targets, none
skipped; `go vet ./...` is clean. The frozen corpus: the merge changed no
verdict or C hash recorded by either side; 62 rejections remain (guard 60).
Every ELF hash recorded by the SBF tests in the earlier October 14 results
(allocation, compound assignment, function values, func-maps, globals,
interfaces, maps, math/bits, strings, value types) rebuilt identically; the
Go+Bend fixtures of `2026-10-14-bend-fogo-v157` were not rebuilt (they need
the pinned Bend producer). `verify_rebuild.py` rebuilt full swap (fogo-v0
`4697a9f5…`, solana-v3 `2b5f21d9…`) and `clock2` byte-identically from two
fresh directories, matching the v1.57 reference.

Fixtures, ELFs and runner reports stay local; [metrics.json](metrics.json)
keeps the summaries.
