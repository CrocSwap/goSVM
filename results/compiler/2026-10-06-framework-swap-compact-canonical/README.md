# Canonical compact SDK: generated full swap and isolated ordinary-Go service

PASS: 108 scenarios in 3 actual-SBF repetitions, 110 transactions per repetition; native/scaffold and stack checks also pass.

ELF: 68,320 bytes, SHA-256 `8d37edb41b22b2ed59d89a6625ebe7721797d428cf4df387eea307dd55c872ab`. The hash matches the preceding SDK checkpoint. This workload retains its existing general signer calls; it does not opt into the compact helper.

[Summary and commands](summary.json) bind current source hashes and individual logs; SBF reports retain exact cases/errors/CU/rollback. See [canonical adoption](../../../docs/COMPACT_SIGNER_ADOPTION.md) for the compact Whirlpools integration proof.

Reproduce into a new directory: `python3 scripts/framework_swap_verify.py --output results/compiler/NEW-EXPERIMENT`. Root tests alone do not cover these separate application modules.
