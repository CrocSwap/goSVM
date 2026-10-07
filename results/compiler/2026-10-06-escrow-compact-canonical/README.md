# Canonical compact SDK: generated SOL hashlock escrow lifecycle

PASS: 32 scenarios in 3 actual-SBF repetitions, 42 transactions per repetition; native/scaffold and stack checks also pass.

ELF: 25,280 bytes, SHA-256 `4b858bceb77129196635f0873d6c61357180de013a7f90106a802fe7042c93f3`. The hash matches the preceding SDK checkpoint. This workload retains its existing general signer calls; it does not opt into the compact helper.

[Summary and commands](summary.json) bind current source hashes and individual logs; SBF reports retain exact cases/errors/CU/rollback. See [canonical adoption](../../../docs/COMPACT_SIGNER_ADOPTION.md) for the compact Whirlpools integration proof.

Reproduce into a new directory: `python3 scripts/escrow_verify.py --output results/compiler/NEW-EXPERIMENT`. Root tests alone do not cover these separate application modules.
