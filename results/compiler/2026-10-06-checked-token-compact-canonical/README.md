# Canonical compact SDK: checked classic-token and generic CPI fixture

PASS: 117 scenarios in 3 actual-SBF repetitions; native/scaffold and stack checks also pass.

ELF: 14,784 bytes, SHA-256 `59e069fa76f0439579ff1c43b7a1ba01b7a105f5b6b5c05494089032f2b9002d`. The hash matches the preceding SDK checkpoint. This workload retains its existing general signer calls; it does not opt into the compact helper.

[Summary and commands](summary.json) bind current source hashes and individual logs; SBF reports retain exact cases/errors/CU/rollback. See [canonical adoption](../../../docs/COMPACT_SIGNER_ADOPTION.md) for the compact Whirlpools integration proof.

Reproduce into a new directory: `python3 scripts/checked_token_verify.py --output results/compiler/NEW-EXPERIMENT`. Root tests alone do not cover these separate application modules.
