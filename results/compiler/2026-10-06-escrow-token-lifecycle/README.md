# Generated Escrow token-lifecycle SDK regression

Passed October 6, 2026: 32 real-SBF scenarios in three repetitions,
plus the native/boundary/scaffold checks listed in [summary.json](summary.json).
The current SDK adds the checked token creation/closing helper snapshot. Existing
application behavior remains covered; these cases do not replace the full
[managed swap lifecycle](../2026-10-06-managed-swap-supported/README.md).

ELF: 25,280 bytes, SHA-256 `4b858bceb77129196635f0873d6c61357180de013a7f90106a802fe7042c93f3`.
Largest static frame: 1,984 bytes. Instrumented linking reproduces the exact
executed ELF. Commands and source/tool/artifact hashes bind this snapshot.

Local LiteSVM with the pinned feature profile is used. No fresh-validator,
independent developer-trial or production-audit claim is made. Historical
results remain unchanged.
