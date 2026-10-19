# Word compares for hand-written byte loops

October 19, 2026. Local only; runners 0.5.1 (fogo-v0, LiteSVM 0.8.2) and 0.6.0
(solana-v3, LiteSVM 0.13.0), platform-tools v1.57. Contract:
[GO_ARRAYS.md § Hand-written byte loops](../../../docs/GO_ARRAYS.md#hand-written-byte-loops).

## Why

Programs compare keys and discriminators with loops like
`for i := 0; i < 32; i++ { if a[i] != b[i] { return false } }`. Clang keeps
these a byte at a time on SBF, because each byte may return, so an equal
32-byte compare costs about 320 CU. Little-endian loads
(`v |= uint64(b[i]) << (8*i)`) were the other suspect: clang already combines
them into one `ldxdw`, with a constant or a variable offset, so they are left
alone.

## Correctness

- `TestByteLoopDifferential` runs `examples/byte-loop-values` natively and as
  host C built with the word paths (`GOSVM_BYTE_LOOP_WORDS`) and without:
  2,050 vectors over five compare shapes (arrays, slices, an array pointer,
  bytes 4 to 12, `int8` elements with a body that breaks) and five load
  shapes, with equal operands, bit 0 and bit 7 flipped at every byte, every
  operand length from 0 to 32 (with and without an earlier difference) and
  nil pointers. Outputs and panics match native Go both ways.
- `TestByteLoopSBF` runs the same vectors on both profiles: 1,741 complete
  with Go's output and 309 trap and roll back, as in Go.
- `TestByteLoopsC` checks the five compares get the path and nine near-miss
  loops do not; `TestCorpusMatchesPreCheckCompiler` removes the paths and
  matches every recorded C hash.

## CU (whole transaction, `*/before-summary.json` is main `939b215`)

`probes` in the summaries: a program per shape that compares input bytes
0..31 with 32..63 and writes one byte. The empty probe costs 98 CU.
Identical on both profiles.

| shape | equal, before | after | first byte differs, before | after |
| --- | ---: | ---: | ---: | ---: |
| `[]byte`, 32 bytes | 422 | 112 | 106 | 110 |
| `[]byte` vs `*[32]byte` | 512 | 148 | 135 | 152 |
| `[32]byte` arrays (Rust `Ord` body, plus two copies) | 523 | 175 | 180 | 183 |
| `[]byte`, 13 bytes | 152 | 109 | 99 | 106 |
| `[]byte`, 8 bytes | 132 | 106 | 99 | 106 |

An equal 32-byte compare drops from about 320 CU to about 15. A difference
costs 3 to 17 CU more: the word compare runs before the loop finds it.

`cu` in the summaries is the example's modes in one program. Its compare
modes save as above, but the untouched loads and the big-endian control also
cost 8 to 18 CU more: the extra code changes how clang lays out the shared
`Run`. The probes isolate each loop.

## Reference ELFs

This change moves the full swap (fogo-v0 `963c8f52…`, solana-v3 `cd49e4f1…`).
`clock2` is unchanged. `rebuild/metrics.json` is the fresh-rebuild check
against the previous reference (the #25 integration rebuild), and
`scripts/verify_rebuild.py` now uses it as the default reference:

```sh
python3 scripts/verify_rebuild.py --output <new-directory> --cli build/gosvm
python3 scripts/check_rebuild_reference.py <new-directory>/metrics.json
```
