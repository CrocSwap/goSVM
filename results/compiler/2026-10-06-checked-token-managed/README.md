# Checked Token token-lifecycle SDK regression

Passed October 6, 2026: 117 real-SBF scenarios in three repetitions,
plus the native/boundary/scaffold checks listed in [summary.json](summary.json).
The current SDK adds the checked token creation/closing helper snapshot. Existing
application behavior remains covered; these cases do not replace the full
[managed swap lifecycle](../2026-10-06-managed-swap-supported/README.md).

ELF: 14,784 bytes, SHA-256 `59e069fa76f0439579ff1c43b7a1ba01b7a105f5b6b5c05494089032f2b9002d`.
Largest static frame: 3,648 bytes. Instrumented linking reproduces the exact
executed ELF. Commands and source/tool/artifact hashes bind this snapshot.

Local LiteSVM with the pinned feature profile is used. No fresh-validator,
independent developer-trial or production-audit claim is made. Historical
results remain unchanged.
