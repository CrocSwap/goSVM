# Word-at-a-time `==` on byte arrays

October 19, 2026. Local only; runners 0.5.1 (fogo-v0, LiteSVM 0.8.2) and 0.6.0
(solana-v3, LiteSVM 0.13.0), platform-tools v1.57. Contract:
[GO_ARRAYS.md](../../../docs/GO_ARRAYS.md).

## Correctness

`TestByteArrayEqualSBF` compares `[8]byte`, `[13]byte`, `[20]byte`,
`[32]byte` (also as a struct field), `[64]byte`, `[13]int8`, `[32]int8`,
`[32]bool` and `[8]uint16` pairs: equal, and with bit 0 and bit 7 flipped at
every position. All 437 comparisons match Go on both profiles.
`TestByteArrayEqualC` checks that only one-byte integer arrays of at least 8
elements get the word comparison, and `TestCorpusMatchesPreCheckCompiler`
that it is the only change to the frozen corpus's C.

## CU (whole transaction, equal inputs; `*/before-summary.json` is main `939b215`)

| arrays | before | after | saved |
| --- | ---: | ---: | ---: |
| `[8]byte` | 165 | 162 | 3 |
| `[13]byte` | 241 | 197 | 44 |
| `[20]byte` | 290 | 196 | 94 |
| `[32]byte` and a struct holding one (2 comparisons) | 979 | 183 | 796 |
| `[64]byte` | 1,053 | 207 | 846 |
| `[32]int8` | 1,165 | 739 | 426 |
| `[32]bool` (unchanged lowering) | 1,342 | 1,340 | 2 |

fogo-v0; solana-v3 is the same within 1 CU. The transactions also copy the
inputs, which #22 (merged in #25) moved to `sol_memmove_`, so these totals are
lower than on `79cf25c` (1,548 to 750 for the `[32]byte` row) but the saving
per comparison is unchanged: about 400 CU per equal `[32]byte` pair. Short
arrays were already cheap.

## Hyperlane Go programs

Measured before #25, on `79cf25c`: `hyperlane-monorepo` `sealevel` at
`1d4694c13f` (which already compares account keys with an unrolled XOR, not
`==`), built with `hyperlane-integration-2` with and without this branch, all
23 Rust functional suites, 584/584 pass both ways. Total CU: fee 1.2% less,
IGP 1.4% less, synthetic token 1.6% less, others within 0.5%; per-call median
unchanged. Programs that compare keys with `==` gain more.

## Reference ELFs

This change moves the full swap (fogo-v0 `24c23925…`, solana-v3 `83f72984…`).
`clock2` is unchanged. `rebuild/metrics.json` is the fresh-rebuild check
against the previous reference (the #25 integration rebuild), and
`scripts/verify_rebuild.py` now uses it as the default reference:

```sh
python3 scripts/verify_rebuild.py --output <new-directory> --cli build/gosvm
python3 scripts/check_rebuild_reference.py <new-directory>/metrics.json
```
