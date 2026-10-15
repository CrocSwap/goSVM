# Standard-library packages on chain (2026-10-14/15)

`errors`, `math` (integer limits), `encoding/binary`, `unicode/utf8`,
`unicode` (case mapping, `IsSpace`), `strings`, `bytes` and `strconv` (no
floats) on chain, compiled from curated Go 1.22.0 source
([GO_STDLIB.md](../../../docs/GO_STDLIB.md)), with the two language features
they needed (variadic functions, pointers to pointers). Branch
`worktree-agent-a4919db3f7fd1c8b4`, merged with `main` at `48fb51f` (struct
`==`, type aliases, composite arrays, `panic`). Platform-tools v1.57; runners
0.5.0 (fogo-v0) and 0.6.0 (solana-v3), installed from
`results/svm/2026-10-05-runner-install-complete/` and
`results/svm/2026-10-09-runner-0.6.0-package/` into a scratch `GOSVM_CACHE`.
Local runners only; no network validation.

## Results

| Check | Result |
| --- | --- |
| `TestStdlibDifferential`: `examples/stdlib-values` natively (Go 1.22.0 stdlib) vs host-compiled C (32 KiB heap) | 30,906 vectors match, 4,048 Go panics (each an abort at the same point, with the same panic line) |
| `TestStdlibSBF`, fogo-v0 | 306 cases pass (49 aborts with rollback, 31 with a checked panic line); ELF `d3e55682…` |
| `TestStdlibSBF`, solana-v3 | 306 cases pass (same); ELF `4f6a4430…` |
| Every compiler SBF suite (`go test -run SBF -v ./internal/compiler`) | all pass on both targets, none skipped |
| Root `go test ./...` (non-short, scratch `GOSVM_CACHE`) | pass (after updating one generic-variadic rejection test) |
| Frozen check corpus | unchanged except the recorded `corpusChanges` (variadic entry 120, `**T` entries 3 and 190, the `fmt` import message 249) |
| `scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json` | identical, matches reference: full swap fogo-v0 `4697a9f5…`, solana-v3 `2b5f21d9…`; clock2 `0f768370…` / `54f34b65…` |

The SBF differential first failed (`strings.Replace` aborted on chain but
not on the host). The cause is a latent miscompilation: clang passes C struct
parameters `byval` and the SBF backend does not copy them, so a callee that
writes a string/slice/struct parameter can change its caller's variable. The
overlay now copies written aggregate parameters at entry; program code is
unchanged pending a decision
([GO_STDLIB.md](../../../docs/GO_STDLIB.md#aggregate-parameters-on-sbf)).

## CU of common calls

A probe program's call (`in[0] == 1`) minus the same ELF skipping it; "init"
is the skipped run minus the same template importing nothing (package
initialization on every invocation). Identical on both targets unless two
values are given (fogo-v0 / solana-v3). An empty `Process` costs 85 CU.

| Call | CU | Init |
| --- | --- | --- |
| `binary.BigEndian.Uint64` | 3 | 15 |
| `binary.LittleEndian.PutUint64` | 2 | 2 |
| `binary.Uvarint` (3 bytes) | 25 | 5 |
| `bytes.Index` (32 bytes) | 260 | 14 |
| `errors.Is`, match at depth 1 | 22 | 70 |
| `errors.Is`, no match through 2 wraps | 89 | 70 |
| `errors.As`, 2 wraps, pointer target | 56 | 71 |
| `strconv.Itoa` (2 digits) | 7 | 70 |
| `strconv.ParseUint` (8 / 20 digits, via `string(b)`) | 123 / 480 | 75 / 83 |
| `strconv.FormatUint` / `AppendUint` (20 digits) | 423 / 430, 414 | 70 / 71 |
| `strconv.Atoi` failing + `err.Error()` | 2,843 / 2,257 | 83 |
| `strconv.Quote` (12 bytes) | 993 / 861 | 84 / 83 |
| `strings.Index` (32 bytes) | 511 | 14 |
| `strings.Split` (4 fields) | 332 | 1 |
| `strings.ToUpper` (12 ASCII bytes) | 811 | 1 |
| `strings.EqualFold` (non-ASCII) | 544 / 543 | 1 |
| `strings.Builder` (4 writes + `String`) | 388 | 14 |
| `utf8.DecodeRuneInString` / `RuneCount` (64 bytes) | 56 / 742 | 4 / 19 |
| `unicode.ToUpper` (non-ASCII) | 116 | 14 |

`errors` and `strconv` cost about 70 CU per invocation to initialize
(`ErrUnsupported`, `ErrRange`, `ErrSyntax` are boxed errors in the globals
block); the other "init" values are layout noise of a few CU around the
skipped branch (those packages have no non-constant variables). Vector CU:
median 4,555 / 4,389, maximum 45,081 / 44,948.

## Library shakeout rerun (real standard library)

[`shakeout/setup.py`](shakeout/setup.py) builds pass 1 of the
[library shakeout](../2026-10-14-library-shakeout/README.md) (same library
versions, files and entry; only module import paths rewritten; the real
standard library, no stand-ins) from a module cache holding the four
modules; the historical `peel` tool (built from that directory, unchanged)
peels it; [`shakeout/summarize.py`](shakeout/summarize.py) counts the result.

| | `main` `48fb51f` | this branch |
| --- | --- | --- |
| Declarations removed before the remainder checks | 255 | 96 |
| Standard-library import rejections | 38 (incl. `errors`, `math`, `encoding/binary`, `strconv`, `strings`) | 18 (`math/big` ×8, `io` ×3, `fmt` ×2, `database/sql`, `database/sql/driver`, `encoding`, `encoding/json`, plus 2 non-stdlib modules) |
| uint256 functions that check | 83 of 155 | 134 of 155 |
| int256 | 51 of 71 | 60 of 71 |
| uint128 | 42 of 50 (unreached code counted) | 46 of 50 |
| uniswap-v3 utils | 14 of 40 (package dropped from the entry) | 31 of 40; 9 of the 10 entry points survive |

Historical reference: 248 removals (pass 1, real stdlib) and 252 (pass 2,
stand-ins) at `764e4a9`; 185 (pass 2) after package-level variables.

Remaining root causes on this branch (71 of the 96 removals are cascades):

| Blocker | Count | Examples |
| --- | --- | --- |
| Standard-library packages out of scope | 18 imports | `math/big` (`ToBig`/`FromBig`, uniswap's `big.Int` constants and `GetSqrtRatioAtTick`/`GetTickAtSqrtRatio`), `fmt`/`io`/`encoding/json`/`database/sql` (text and serialization interfaces) |
| Other modules | 2 imports | `constants.go`: `daoleno/uniswap-sdk-core/entities`, `go-ethereum/common` |
| Elided `&T{...}` of a non-struct `T` | 2 | `var multipliers = [...]*Int{{...}}` (`Int` is `[4]uint64`) in uint256/int256 decimal parsing; cascades to `MustFromDecimal`, `MustFromDec` and `GetTickAtSqrtRatioV2`'s constants |
| Array above the 1 KiB per-value limit | 1 | uint256 `var pows [60]Int`, used outside the in-place forms; cascades to `Log10` |
| Floats | 2 | `(*Int).Sqrt` (`math.Sqrt`), `(*Int).Float64` (`math.Float64frombits`) |

The raw peel logs are local (`.jsonl`); the counts are in
[metrics.json](metrics.json).
