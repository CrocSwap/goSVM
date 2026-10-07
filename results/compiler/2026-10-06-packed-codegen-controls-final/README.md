# Isolated packed-codegen controls

PASS: unchanged pinned frontend/SDK/application, controlled changes only in copied lowered C. Every variant passes 12,000 independently encoded host/UBSan bounds and partial-write vectors and 166 actual-SBF handler cases × three runs. Baseline ELF matches exactly. Account bytes, errors, CPIs/Token consumed CU and rollback remain checked; caller consumed and remaining-budget log numbers change.

| Variant | Ratio | ELF bytes | Median successful saving CU | Successful regressions | Failed regressions |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 1.737883 | 84192 | 0 | 0 | 0 |
| read-words | 1.742357 | 85352 | -8 | 61 | 14 |
| write-words | 1.702520 | 84208 | 746 | 0 | 0 |
| compare-pointers | 1.737883 | 84192 | 0 | 0 | 0 |
| combined | 1.706915 | 85368 | 644 | 0 | 14 |

Only the write-only variant was adopted. These diagnostic transformations are deliberately bound to the preserved emitted symbols/signatures; they are not a generic compiler pass. [Summary](summary.json) records source/tool hashes and commands, with all C/native/SBF reports retained. See [canonical implementation](../../../docs/PACKED_STORE_OPTIMIZATION.md).
