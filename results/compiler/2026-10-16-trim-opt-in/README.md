# Opt-in reachability trimming (2026-10-16)

Question: with the user's decision that whole-package checking stays the
default, can a program explicitly opt dependency packages in to checking and
compiling only what `Process` reaches, so that the real libraries of
[the real-libraries experiment](../2026-10-16-real-libraries/README.md)
compile **unmodified** from the module cache, with the same results and CU as
the trimmed copies used there?

Design and rules: [GO_PACKAGES.md#trimming](../../../docs/GO_PACKAGES.md#trimming)
(implementation `internal/compiler/trim.go`). Compiler: this branch (parent
`7bee8b0`); Go 1.22.0; platform-tools v1.57; runners 0.5.0 (fogo-v0) and
0.6.0 (solana-v3) from the retained packages, installed into a scratch
`GOSVM_CACHE`. Modules: holiman/uint256 v1.3.1, KyberNetwork/int256 v0.1.4,
KyberNetwork/uniswapv3-sdk-uint256 v0.5.5, lukechampine.com/uint128 v1.3.0,
and (type information only) go-ethereum v1.10.20, uniswap-sdk-core v0.1.5
and their requirements, all from the module cache, offline.

## Summary

- **Opt-in, scoped, reported.** `"trim"` in `gosvm.json` or repeated
  `-trim <pattern>` (import path or `path/...`). The example names the four
  library modules; its own package stays fully checked. Without the opt-in
  every program behaves exactly as before (the frozen corpus is unchanged;
  the full-swap reference ELFs are byte-identical, [rebuild](rebuild/metrics.json));
  the default rejection of each library now ends with a hint naming the
  opt-in (`TestRealLibrariesWholePackages`).
- **The unmodified modules compile.** `examples/real-libraries` builds from
  the module cache with no `replace` and no copies. Trimming drops 62
  declarations, 19 of them variable specifications with initializers
  ([trim-report.json](trim-report.json), the build's
  `<output>.trim.json`), and leaves `math/big`, `fmt`, `database/sql`,
  `encoding/json`, go-ethereum and uniswap-sdk-core as type-only imports
  (neither compiled nor initialized).
- **Same results.** Host: 22,126 vectors (489 Go panics) match native Go for
  both forms (opt-in and the historical trimmed copies). SBF: 1,188 cases per
  target (32 aborts with rollback, 24 with a panic log line) match native Go
  for both forms on fogo-v0 and solana-v3.
- **Same call CU; initialization 27 CU cheaper.** Every bench operation's
  call CU is identical between the forms on both targets; every total is
  27 CU lower under the opt-in, because `int256.MaxI256 = &Int{...}`, which
  the example never references, is a dropped initializer (the trimmed copy
  keeps it: it is supported code). The ELF is 256 bytes smaller on both
  targets.

## Trim report of the example

| Package | Dropped | Dropped initializers | Type-only imports |
| --- | ---: | ---: | --- |
| `holiman/uint256` | 21 | 6 (the `var _ I = (*Int)(nil)` assertions) | `database/sql`, `database/sql/driver`, `encoding`, `encoding/json`, `fmt`, `math/big` |
| `KyberNetwork/int256` | 7 | 1 (`MaxI256`) | `math/big` |
| `uniswapv3-sdk-uint256/constants` | 9 | 8 (`FactoryAddress`, `AddressZero`, `NegativeOne`, `Zero`, `One`, `Q96`, `Q192`, `PercentZero`) | go-ethereum `common`, uniswap-sdk-core `entities`, `math/big` |
| `uniswapv3-sdk-uint256/utils` | 21 | 4 (`One`, `Q32`, `MinSqrtRatio`, one blank) | go-ethereum `accounts/abi`, `common`, `crypto`, uniswap-sdk-core `entities`, `math/big` |
| `lukechampine.com/uint128` | 4 | 0 | `fmt`, `math/big` |

The build prints one warning line per package and the report's path:

```text
gosvm: warning: trimmed github.com/holiman/uint256: dropped 21 declarations (6 initializers); type-only imports, not compiled or initialized: database/sql, database/sql/driver, encoding, encoding/json, fmt, math/big
...
gosvm: warning: full list of dropped declarations: <output>.trim.json
```

Compared with the trimmed copies (`examples/real-libraries/trim/trim.json`,
which drop only what goSVM rejects), trimming also drops supported but
unreached code (`int256.MaxI256`) and keeps the
reached methods that dynamic dispatch needs; the kept code compiles to the
same call CU. `constants` is compiled on chain because `utils`' reached
code uses `constants` (it was "promoted" from type information).

## CU (fogo-v0 / solana-v3)

Main program, idle vector: 26,970 / 26,992 opt-in vs 26,997 / 27,019
trimmed copies. Bench call CU (each operation minus the same vector calling
nothing) is identical for all 35 operations, for example `ComputeSwapStep`
exact in 17,199 / 17,168, `GetTickAtSqrtRatioV2` (price 1) 8,014 / 7,873
([sbf-fogo-v0-opt-in.json](sbf-fogo-v0-opt-in.json),
[sbf-solana-v3-opt-in.json](sbf-solana-v3-opt-in.json); the trimmed-copy
runs are the `*-trimmed-copies.json` files and reproduce the historical
table).

**Initialization probes measure something else under trimming.** The
probes import a package and reference one constant or nothing, so under
trimming the unreferenced initializers are dropped:

| Probe (init CU above its base) | Trimmed copies | Opt-in |
| --- | ---: | ---: |
| `uint256` | 484 | 198 |
| `int256` | 434 | 198 |
| `uint128` | 15 | 30 |
| `utils` (all four packages) | 25,442 | 30 |
| an app using only `ComputeSwapStep` | 25,442 | 5,398 |
| an app using only the tick math | 26,276 | 20,967 |

(fogo-v0; solana-v3 within 5 CU.) `uint128` costs 15 CU more because the
upstream package imports `errors` (for a dropped declaration), which the
copy no longer imported, so `errors.ErrUnsupported` is initialized; overlay packages are compiled and initialized whenever
imported, as Go would. The app rows show the real effect for applications:
an initializer runs only when reached code references its variable, so a
program using `ComputeSwapStep` alone no longer pays for the tick-math
constants (`sqrtConst1`–`21`, the `MustFromDec` magic numbers). This is the
behavioural difference the opt-in accepts and reports; programs that need
every initializer to run do not opt in. ELF sizes shrink with it
(`ComputeSwapStep` only: 50,520 vs 70,920 bytes on fogo-v0).

## Tests

- `TestRealLibrariesDifferential` (both forms), `TestRealLibrariesSBF`
  (both forms × both targets, CU tables), `TestRealLibrariesWholePackages`
  (default rejection with the hint; each probe compiles under the opt-in).
- `TestTrimDropsUnreachable`, `TestTrimRejections` (reached floats, `go`
  in `init`, recursion, a method reached through an interface, a reached
  unsupported import, a reached left-out overlay symbol: same messages as
  without trimming), `TestTrimEntryPackage`, `TestTrimModules` (type-only
  and promoted module packages, missing modules, cache keys),
  `TestTrimWarnings`, `TestValidateTrim`; `TestTrimmedUses`
  (`gosvm features` `uses.trimmed`); `TestTrimSetting` (`gosvm.json`).
- `make test` passes (compiler package 1,105 s); `go test -v
  ./internal/compiler`: 222 top-level tests pass, none skipped; `go vet
  ./...` clean; the frozen corpus needs no new `corpusChanges` entry.
- `scripts/verify_rebuild.py`: fogo-v0 `a66c5a97…`, solana-v3 `b419245b…`
  and `clock2` identical to the reference ([rebuild](rebuild/metrics.json)).

## Reproduction

```sh
(cd examples/real-libraries && GOWORK=off go mod download)
GOSVM_CACHE=<scratch> build/gosvm runner install -archive <0.5.0 package>   # and the 0.6.0 package
GOSVM_CACHE=<scratch> GOSVM_REALLIBS_RESULTS=<dir> go test -ldflags=-linkmode=external \
  -run 'TestRealLibrar|TestTrim' -count=1 ./internal/compiler
(cd examples/real-libraries && gosvm -target fogo-v0 -o /tmp/rl.so \
  -trim github.com/holiman/uint256 -trim github.com/KyberNetwork/int256 \
  -trim 'github.com/KyberNetwork/uniswapv3-sdk-uint256/...' -trim lukechampine.com/uint128 .)
python3 scripts/verify_rebuild.py --output <new dir>
```

[metrics.json](metrics.json) has the compact numbers.
