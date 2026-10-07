# Whirlpools arithmetic controls

October 6, 2026. Copied fixed-fee classic-token handler, events omitted, O2. Original benchmark source/pins remain preserved. Each variant passes native tests and all 166 SBF scenarios three times, with exact state/errors/CPI/rollback checks. Ratios are medians of paired successful-case CU against unchanged scoped Rust.

| Variant | CU ratio | ELF bytes | Success regressions vs 1.70× | Failed-path regressions |
| --- | ---: | ---: | ---: | ---: |
| baseline | 1.702520× | 84,208 | 0 | 0 |
| bounded-muldiv | 1.408072× | 84,120 | 0 | 0 |
| normalized-delta-a | 1.665499× | 84,696 | 14 | 0 |
| combined | 1.411664× | 82,064 | 7 | 0 |

See [summary.json](summary.json) for commands, pins and counts; per-case reports remain beside it. The recorded experiment.py.txt preserves the exact driver used for this run. The [accepted target report](../../../docs/WHIRLPOOLS_133_TARGET.md) explains which alternatives were adopted.
