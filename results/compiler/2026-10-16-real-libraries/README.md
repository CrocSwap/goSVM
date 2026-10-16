# Real third-party libraries on chain (2026-10-16)

Question: do real, published Go libraries run correctly on chain when a
program consumes them as ordinary module dependencies, and what do they cost?

| Module | Version | `go.sum` (h1) | Packages |
| --- | --- | --- | --- |
| `github.com/holiman/uint256` | v1.3.1 | `JfTzmih28bittyHM8z360dCjIA9dbPIBlcTI6lmctQs=` | `uint256` |
| `github.com/KyberNetwork/int256` | v0.1.4 | `SbnhxqcsZXrP+5pSkolpSo2ViEWiq3K/hjfl5OQgX/4=` | `int256` |
| `github.com/KyberNetwork/uniswapv3-sdk-uint256` | v0.5.5 | `ZoJULTqk99mmpJyrcAw2NXIni8BFPbm7IZ0JTEY6rDo=` | `utils`, `constants` |
| `lukechampine.com/uint128` | v1.3.0 | `cDdUVfRwDUDovz610ABgFD17nXD4/uDgVHl2sC3+sbo=` | `uint128` |

The SHA-256 of every upstream source file, and of every trimmed file built
from it, is in [`examples/real-libraries/trim/trim.json`](../../../examples/real-libraries/trim/trim.json).
Compiler: this branch (parent `959ea9d`); Go 1.22.0; platform-tools v1.57;
runners 0.5.0 (fogo-v0) and 0.6.0 (solana-v3) from the retained packages,
installed into a scratch `GOSVM_CACHE`.

## Summary

- **Module consumption works.** `examples/real-libraries` requires the four
  modules in `go.mod`, imports their real paths, and goSVM's offline resolver
  (`go list -find`, `GOPROXY=off`, read-only module mode) finds them in the
  module cache. The Uniswap SDK's heavy requirements (go-ethereum,
  uniswap-sdk-core, …) are needed only by packages and files the on-chain
  program does not use: with a module cache holding only the four modules
  the resolver still resolves them (pruned module graph). No resolver change
  was needed.
- **No package compiles unmodified as a whole** (below): each has at least
  one file importing `math/big`, `fmt`, `database/sql`, `encoding/json` or
  go-ethereum, or using floats, and goSVM checks every file of a reachable
  package. The measurement therefore uses **trimmed copies**: upstream files
  minus the rejected declarations (each kept declaration byte-identical),
  resolved through ordinary `go.mod` `replace` lines and generated
  reproducibly from the module cache (`examples/real-libraries/trim`). Nothing
  was hand-edited. Per-function reachability, which would make the copies
  unnecessary, was **not** implemented (a product decision; see
  recommendations).
- **Four in-scope gaps fixed** (they would otherwise have removed the Uniswap
  tick math, uint256's decimal parsing and `Log10`): package `io` (for
  `io.EOF`), elided `&T{...}` elements of array type, read-only pointer
  parameters for package-level tables, and pointer conversions `(*T)(p)`.
- **Correctness: no mismatch from goSVM.** 22,126 host vectors and 1,188 SBF
  cases per target (fogo-v0 and solana-v3) match the unmodified libraries run
  natively, byte for byte, including errors (identity and text), panics
  (aborts with the contracted log line) and rollback. One difference was found
  and is a **gc optimizer effect**, not a goSVM bug (below).
- **Cost:** the Uniswap math costs 2.4k–5.3k CU per sqrt price from a tick,
  8k–14k per tick from a price and 10k–18k per `ComputeSwapStep`, **plus
  25.4k CU of package initialization on every invocation** (computed
  constants such as `uint256.MustFromHex`), which is more than a swap step.
  A program calling only `ComputeSwapStep` is a 70,920-byte ELF (fogo-v0).

## Whole-package compilation

The first rejection when a program imports each unmodified package from the
module cache (`TestRealLibrariesWholePackages` pins them):

| Package | First rejection |
| --- | --- |
| `uint256` | `conversion.go:8:2: unsupported import "database/sql"` |
| `int256` | `conversion.go:6:2: unsupported import "math/big"` |
| `uint128` | `uint128.go:6:2: unsupported import "fmt"` |
| `constants` | `constants/constants.go:4:2: unsupported import "math/big"` |
| `utils` | `utils/calldata.go:4:2: unsupported import "math/big"` |

What had to be dropped (`trim.json` records every declaration, with the
error that rejected it or the earlier removal it used):

| Package | Files kept | Functions kept | Root causes (non-cascade) | Dropped declarations |
| --- | --- | --- | --- | --- |
| `uint256` | 5 of 5 | 144 of 155 | imports `database/sql`, `database/sql/driver`, `encoding`, `encoding/json`, `fmt`, `math/big` (×2); `math.Float64frombits` (floats) | `ToBig`, `IntoBig`, `FromBig`, `MustFromBig`, `SetFromBig`, `CmpBig`, `Float64`, `Format`, `MarshalSSZInto`, `UnmarshalSSZ`, `Value`, six `var _ I = (*Int)(nil)` assertions |
| `int256` | 3 of 3 | 66 of 71 | imports `math/big`; `math.Sqrt` (floats) | `FromBig`, `MustFromBig`, `SetFromBig`, `ToBig`, `Sqrt` |
| `uint128` | 1 of 1 | 46 of 50 | imports `fmt`, `math/big` | `Big`, `Scan`, `FromBig`, `FromString` |
| `constants` | 1 of 1 | (no functions) | imports `math/big`, go-ethereum `common`, uniswap-sdk-core `entities` | `FactoryAddress`, `AddressZero`, `NegativeOne`, `Zero`, `One`, `Q96`, `Q192`, `PercentZero` |
| `utils` | 7 of 13 | 32 of 48 | imports `math/big` (×4), go-ethereum, uniswap-sdk-core | whole files `calldata.go`, `compute_pool_address.go`, `encode.go`, `max_liquidity_for_amounts.go`, `price_tick_conversions.go`, `token.go`; `GetSqrtRatioAtTick`, `GetTickAtSqrtRatio`, `GetAmount0Delta`, `GetAmount1Delta` (the `*big.Int` wrappers) and the `big.Int` variables |

Everything dropped is a `big.Int`/`fmt`/SQL/JSON conversion, an
Ethereum-specific helper or a float conversion; none of it is reached by the
uint256/int256 arithmetic or the V2 Uniswap math. Before this work, the
[stdlib shakeout](../2026-10-14-stdlib/README.md#library-shakeout-rerun-real-standard-library)
also lost `MustFromDecimal`, `MustFromDec`, `Log10` and
`GetTickAtSqrtRatioV2` (to the four gaps below); with them fixed, the peel
of the full packages (every non-test file, real standard library) needs 88
removals (29 root causes: 27 imports and 2 float uses; the rest cascades) and only `int256.Sqrt` and
`uint256.Float64` are lost to a language rule (floats).

## Gaps fixed

| Gap | Library use | Fix | Tests |
| --- | --- | --- | --- |
| `io` not available | `uint256.fromDecimal` and `int256.fromDecimal` return `io.EOF`; they back `MustFromDecimal`/`MustFromDec`, which the Uniswap constants call | `io` overlay: `io.go` extract, unchanged (errors, `Seek*`, every interface, `ReadAtLeast`, `ReadFull`, `WriteString`); left-out symbols reported by name ([GO_STDLIB](../../../docs/GO_STDLIB.md)) | `io` group in `examples/stdlib-values` (`TestStdlibDifferential` 34,542 vectors; `TestStdlibSBF`), API/provenance/rejection tests |
| Elided `&T{...}` with array `T` | `var multipliers = [5]*Int{nil, {...}}` in uint256 and int256 (`Int` is `[4]uint64`) | Check rewrites the elided element into `&T{...}` (`elided.go`; [GO_ARRAYS](../../../docs/GO_ARRAYS.md#pointer-literals)) | `TestRealLibraryForms*` (`elided-array-pointers`), `TestAllocationRejections` |
| `&pows[i]` makes a table writable | uint256 `Log10`: `z.Lt(&pows[t-20])` on the 1,920-byte `var pows = [60]Int{...}` | **Read-only pointer parameters**: an address passed only to parameters that read through it keeps the variable read-only (`readonlyptr.go`; [GO_GLOBALS](../../../docs/GO_GLOBALS.md#read-only-pointer-parameters)); the table stays zero-cost read-only data | `TestGlobalReadOnly` (8 new cases), `TestLargeTables*`, `TestRealLibraryForms*` |
| Pointer conversions | `GetTickAtSqrtRatioV2`: `(*int256.Int)(&tmp)` | `(*T)(p)` with identical underlying base types ([GO_POINTERS](../../../docs/GO_POINTERS.md#conversions)) | `TestRealLibraryForms*` (`pointer-conversions`, escape to heap, nil) |

Each form was checked against native Go (host C, one fresh native process per
input) and on SBF for both targets. **Byte identity:** no frozen-corpus
program's C changed (only entry 249's unsupported-import message, which now
lists `io`), and the fresh rebuild ([`rebuild/metrics.json`](rebuild/metrics.json),
`scripts/verify_rebuild.py`) reproduces the reference full-swap ELFs
`a66c5a97…` (fogo-v0) and `b419245b…` (solana-v3) and `clock2`.

## Differential correctness

`examples/real-libraries` writes every result of 152 operations in 8 groups
(uint256 arithmetic with aliased operands; unary/shift/bytes/SSZ; text:
`Dec`, `Hex`, `FromHex`, `FromDecimal`, JSON, `Scan`, `Must*` panics; int256;
tick math; full math and sqrt-price math with `ComputeSwapStep`; uint128;
bench). Operands: 40% edge values (0, 1, small, max, max−1/−2, powers of two
±1 at 2^31/32/63/64/65/95/96/97/127/128/129/159/160/161/191/192/193/254/255,
the Uniswap price limits ±2, small negatives), the rest random of random bit
length or sparse; prices mostly in `[MinSqrtRatio, MaxSqrtRatio)` with zero
and boundary prices; liquidities up to and beyond 2^128; signed amounts;
generated hex/decimal/JSON/scientific text, valid and not. The tick sweep runs
`GetSqrtRatioAtTickV2` and the tick→price→tick round trip (and one price on
each side) for every tick within 40 of `MinTick`, `MaxTick` and 0, 2,000
ticks spread across `[-887272, 887272]`, the four ticks just outside it and
±2^63.

| Run | Vectors | Result |
| --- | --- | --- |
| Host C vs native Go (`TestRealLibrariesDifferential`) | 22,126 (489 Go panics) | all match |
| fogo-v0 SBF vs native Go (`TestRealLibrariesSBF`) | 1,188 (32 aborts with rollback, 24 with a panic log line) | all match |
| solana-v3 SBF vs native Go | 1,188 (same) | all match |
| Language forms (`TestRealLibraryForms*`) | 3 programs × 64 inputs host, × 24 SBF per target | all match |

Uniswap outcomes in the host run (successes, errors): `GetSqrtRatioAtTickV2`
2,569/79, `GetTickAtSqrtRatioV2` 258/82, `MulDivV2` 253/87,
`GetNextSqrtPriceFromInput` 236/104, `…FromOutput` 76/264,
`ComputeSwapStep` 641/39, `AddDeltaInPlace` 142/198 (`GetAmount0DeltaV2`
cannot fail: 340/0). A sample of the batch-oracle results was rechecked with
one fresh native process per vector.

**Mismatch found (not a goSVM bug).** The first run differed on
`uint256.MarshalText`/`MarshalJSON`: the returned slice's capacity was 51 in
Go and 64 in goSVM for a 51-digit number. Go 1.22's escape analysis makes
`[]byte(s)` a **zero-copy** conversion (capacity `len(s)`, sharing the
string) when it proves the bytes are never written, which depends on gc's
inlining (the same conversion in a non-inlined function has the size-class
capacity 64). goSVM always copies with the size-class capacity, as
contracted. Documented in [GO_STRINGS](../../../docs/GO_STRINGS.md#allocation);
the example writes these slices' lengths and bytes, not capacities.

Other observations: the `min`/`max` builtins and named pointer types
(`type P *T`) are still unsupported (no library here needs them).

## CU per call

`TestRealLibrariesSBF`'s bench group: each operation's CU minus the same
vector shape calling nothing (decoding and writing identical), on the pinned
runners. Inputs are fixed ([`reallibs_sbf_test.go`](../../../internal/compiler/reallibs_sbf_test.go)
`realLibsBenches`): random values of the stated sizes; prices from
`sqrt(1.0001^tick)·2^96`; liquidity 10^24.

| Operation | fogo-v0 | solana-v3 |
| --- | ---: | ---: |
| uint256 `Add` (256-bit) | 35 | 35 |
| uint256 `Mul` (256 × 256-bit) | 376 | 376 |
| uint256 `Div` (256 / 64-bit) | 825 | 822 |
| uint256 `Div` (256 / 192-bit, Knuth D) | 1,368 | 1,365 |
| uint256 `Div` (256 / 128-bit, Knuth D) | 1,560 | 1,557 |
| uint256 `MulDivOverflow` (200 × 200 / 150-bit) | 3,848 | 3,848 |
| uint256 `MulMod` (256-bit) | 7,874 | 7,874 |
| uint256 `Exp` (64-bit exponent) | 30,962 | 30,962 |
| uint256 `Exp` (256-bit exponent) | 121,038 | 121,038 |
| uint256 `Sqrt` (256-bit / 64-bit) | 10,010 / 110 | 9,872 / 109 |
| uint256 `Log10` (200-bit) | 109 | 109 |
| uint256 `Dec` (78 digits) | 7,569 | 7,565 |
| uint256 `SetFromHex` (32 digits; one `MustFromHex`) | 766 | 766 |
| uint256 `SetFromDecimal` (49 digits) | 3,021 | 3,037 |
| int256 `Mul` (128-bit signed) / `Quo` (256 / 128-bit) | 399 / 1,659 | 399 / 1,656 |
| int256 `SetFromDec` (37 digits; one `MustFromDec`) | 2,050 | 2,052 |
| `GetSqrtRatioAtTickV2` (tick 1 / 500000 / MinTick) | 2,447 / 5,252 / 4,737 | 2,444 / 5,249 / 4,737 |
| `GetTickAtSqrtRatioV2` (price 1 / tick ≈500000 / MaxSqrtRatio−1) | 8,014 / 12,528 / 13,951 | 7,873 / 12,370 / 13,761 |
| `utils.MulDivV2` (512-bit product) | 2,922 | 2,921 |
| `GetAmount0DeltaV2` (round up) | 4,493 | 4,490 |
| `GetNextSqrtPriceFromInput` | 4,845 | 4,829 |
| `ComputeSwapStep` exact in, within the range | 17,199 | 17,168 |
| `ComputeSwapStep` exact in, reaching the target | 10,465 | 10,449 |
| `ComputeSwapStep` exact out | 17,844 | 17,808 |
| uint128 `MulWrap` / `QuoRem` (÷100-bit / ÷64-bit) | 51 / 390 / 167 | 51 / 390 / 167 |

## Initialization

Go-exact: every initializer of every imported package runs on **every
invocation** ([GO_GLOBALS](../../../docs/GO_GLOBALS.md#cost)). Programs that
import one package and call nothing on the measured path (idle invocation
minus an empty program's 97 CU); replicas declare initializers copied
verbatim from the SDK in their own package (own cost above the named base):

| Imports / initializers | fogo-v0 | solana-v3 |
| --- | ---: | ---: |
| `uint128` (`Max`) | 15 | 15 |
| `int256` (3 errors, `multipliers`) | 434 | 434 |
| `uint256` (8 errors, `multipliers`, `io`'s 5 errors; `pows`/`pows64` read-only, 0) | 484 | 484 |
| `constants` above `uint256` | 3,222 | 3,222 |
| · replica `TickSpacings` (7-entry map) | 477 | 477 |
| · replica `Q96U256 = Exp(2, 96)` | 2,754 | 2,754 |
| **`utils` (with uint256, int256, constants)** | **25,442** | **25,445** |
| · replica: `sqrtConst1`–`21`, `MaxUint256` (22 `MustFromHex`) | 10,872 | 10,872 |
| · replica: `Uint128Max`, `Uint160Max`, `MaxUint160` (3 `MustFromHex`) | 1,661 | 1,661 |
| · replica: `MaxSqrtRatioU256` (`MustFromDecimal`, 49 digits) | 2,085 | 2,085 |
| · replica: `magicSqrt10001`, `magicTickLow`, `magicTickHigh` (3 `int256.MustFromDec`) | 4,536 | 4,541 |
| · remainder: 11 sentinel errors and 3 `NewInt` in `utils` | ≈2,150 | ≈2,150 |
| a program using only `ComputeSwapStep` (not called) | 25,442 | 25,445 |
| a program using only the tick math (not called) | 26,276 | 26,281 |
| the whole example (idle vector) | 26,900 | 26,922 |

The replicas are approximations of the same code in another program (LLVM
inlines and folds the parsers of constant strings differently per program:
the tick-math-only program's initialization is 834 CU dearer than the
`utils`-only one). So a single `GetSqrtRatioAtTickV2` call costs about 2.4k–
5.3k CU, but a transaction that makes it pays about **28k–31k CU**, of which
25.4k–26.3k is initialization.

## ELF sizes

| Program | fogo-v0 | solana-v3 |
| --- | ---: | ---: |
| The example (every operation above, 8 packages incl. `strconv`, `io`, `errors`) | 296,480 | 302,152 |
| Only `ComputeSwapStep` | 70,920 | 70,040 |
| Only `GetSqrtRatioAtTickV2` + `GetTickAtSqrtRatioV2` | 53,040 | 51,872 |
| Imports `utils`, calls nothing | 34,632 | 32,592 |
| Imports `uint256` / `int256` / `uint128`, calls `NewInt`/`From64` | 8,184 / 7,496 / 2,712 | 7,040 / 6,416 / 1,880 |
| Empty program | 2,384 | 1,552 |

Example ELF SHA-256: fogo-v0 `e31170215da0e502…`, solana-v3
`c81aca147808ad1f…` ([sbf-fogo-v0.json](sbf-fogo-v0.json),
[sbf-solana-v3.json](sbf-solana-v3.json)).

## Rust comparison

Not done. A like-for-like reference would be a Uniswap v3 tick-math crate
(for example `uniswap_v3_math`), which builds on alloy/ruint 256-bit types
and std-oriented error crates: neither goSVM's pure `no_std` Rust library
path (ABI v1) nor a quick `cargo-build-sbf` port fits in this task's budget.
The repository's Whirlpools Rust baselines use Q64.64 `u128` math, a
different algorithm, so their CU is not comparable.

## Recommendations

1. **Compile-time evaluation of initializers** is the largest win. 25.4k CU
   of every invocation that imports `utils` is spent recomputing constants
   (`MustFromHex` of literal strings, `MustFromDecimal`, `MustFromDec`,
   `Exp(2, 96)`). Check could evaluate an initializer whose call graph is
   deterministic, allocation-only and panic-free for its constant arguments,
   and emit the result as read-only data when the variable (and, through the
   new read-only pointer analysis, its pointee) is never written: most of
   these pointers are only read (`ratio.Set(sqrtConst1)`, `mulShift(&r,
   sqrtConst3)`). It must stay Go-exact: an initializer that would panic must
   still panic on every invocation. Expected effect: utils initialization
   from 25.4k to the error boxes (≈1k), i.e. a swap step at ~11–19k CU
   instead of ~36–44k.
2. **Per-function reachability** (checking and compiling only what `Process`
   reaches) would let all five packages compile unmodified: every dropped
   declaration above is unreachable from the math. Tradeoffs to decide:
   (a) Go compiles every function, so a package goSVM accepts would no longer
   prove that all its code fits the subset; (b) unreachable code still has to
   type-check, so unsupported imports (`math/big`, `fmt`, go-ethereum and its
   whole dependency graph) must resolve and be type-checked from source or
   export data, not rejected at the import; (c) package-level **initializers
   are always reachable in Go**: `constants.NegativeOne = big.NewInt(-1)` or
   `FactoryAddress = common.HexToAddress(...)` run at init in native Go, so
   dropping them deviates from Go unless they are proven effect-free (or the
   rule only covers functions and methods, which still leaves `constants`
   uncompilable). Until decided, the trim manifest is the reproducible
   workaround.
3. The `[]byte(s)` zero-copy capacity difference cannot be matched without
   reproducing gc's inlining; keep it documented.

## Reproduction

```sh
(cd examples/real-libraries && GOWORK=off go mod download)
GOSVM_CACHE=<scratch> build/gosvm runner install -archive <0.5.0 package>   # and the 0.6.0 package
GOSVM_CACHE=<scratch> GOSVM_REALLIBS_RESULTS=<dir> go test -ldflags=-linkmode=external \
  -run 'TestRealLibrar' -count=1 ./internal/compiler
python3 scripts/verify_rebuild.py --output <new dir>
```

The trim manifest was derived by peeling a workspace of the full upstream
packages (every non-test file) with the historical
[`peel`](../2026-10-14-library-shakeout/peel/main.go) tool and comparing
declarations; `go run ./trim -update` rewrites its trimmed-file hashes.
[metrics.json](metrics.json) has the compact numbers.
