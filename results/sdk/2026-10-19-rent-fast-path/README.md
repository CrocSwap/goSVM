# Exact fast path for `system.MinimumBalanceFor`

October 19, 2026. Local only; runners 0.5.1 (fogo-v0, LiteSVM 0.8.2) and 0.6.0
(solana-v3, LiteSVM 0.13.0), platform-tools v1.57. Contract:
[SDK2.md § Rent and account lifecycle](../../../docs/SDK2.md#rent-and-account-lifecycle).

## Why

`MinimumBalanceFor` reproduces Rust's `u64 -> f64` conversion, f64 multiply
and saturating cast with integer code, so SBF needs no float helpers. That
costs about 500 to 770 CU per call, almost all in the bit-length loops and the
two rounding stages. The pinned runners and current clusters use 2.0 (Fogo,
Solana before SIMD-0194) or 1.0 (after it). When `n = (128+size)*rate` is
below 2^53 the conversion is exact, and so is a product with 2.0 or 1.0, so
the answer is `2n` or `n`. Any other input still takes the emulation.

## Correctness

- `TestRentFastPathMatchesExact` (native) compares `MinimumBalanceFor` with
  the emulation alone (`minimumBalanceExact`) at both sides of `n = 2^53` and
  of uint64 overflow for every size limit, both thresholds, their neighbouring
  bit patterns, negative zeros, other thresholds and NaN, plus a million random
  inputs near the 2^53 boundary. The existing IEEE oracle test also passes.
- `TestRentSBF` runs 532 cases on each profile (thresholds 2.0, 1.0, their
  neighbours, 2.5, 0.5, zeros, -1, infinity, NaN; sizes 0 to 10 MiB + 1 and
  2^64 - 1; rates 0, 3,480, 6,960, the 2^53 and overflow limits and one past
  each) plus `MinimumBalance` from each runner's Rent sysvar. Every result and
  error code equals the host's.

## CU (whole transaction, `*-cu.json`; main `939b215`)

The probe decodes its input and writes the result with byte loops, about 260
CU of each total.

| call | fogo-v0 before | after | solana-v3 before | after |
| --- | ---: | ---: | ---: | ---: |
| `MinimumBalance(165)`, runner sysvar (2.0 / 1.0) | 889 | 399 | 897 | 396 |
| `MinimumBalance(10 MiB)` | 1,017 | 399 | 1,025 | 396 |
| `MinimumBalanceFor(3480, 2.0, 165)` | 763 | 273 | 763 | 273 |
| `MinimumBalanceFor(6960, 1.0, 165)` | 771 | 270 | 771 | 270 |
| `MinimumBalanceFor` at 2.0, `n = 2^53 - 128` | 1,029 | 273 | 1,029 | 273 |
| `MinimumBalanceFor` at 2.0, `n = 2^53` (exact path) | 803 | 807 | 803 | 807 |
| `MinimumBalanceFor(3480, 2.5, 165)` (exact path) | 759 | 767 | 759 | 767 |
| size over 10 MiB (`ErrRent`) | 261 | 261 | 261 | 261 |

Over all 532 cases: every fast-path input with a nonzero product costs 482 to
759 CU less, a zero product 3 to 6 CU less, and the exact path 2 to 8 CU more
(the threshold compares and a repeated bounds check). Errors before the
threshold check are unchanged.

## Reference ELFs

This change moves the full swap (fogo-v0 `7d162f10…`, solana-v3 `42e1deba…`).
`clock2` is unchanged. `rebuild/metrics.json` is the fresh-rebuild check
against the previous reference (the #25 integration rebuild), and
`scripts/verify_rebuild.py` now uses it as the default reference:

```sh
python3 scripts/verify_rebuild.py --output <new-directory> --cli build/gosvm
python3 scripts/check_rebuild_reference.py <new-directory>/metrics.json
```
