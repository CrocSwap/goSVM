# Preserved swap corpus and shared-service lifecycle regression

Passed October 6, 2026 on the recorded macOS arm64 host. [summary.json](summary.json)
binds the source, commands, checks and artifact hashes. This proof uses the actual
CLI, fresh schema-2/SDK-2 scaffold, generated artifacts, pinned classic SPL Token
image, runner 0.5.0 and local feature profile.

The ELF is 68,320 bytes, SHA-256
`8d37edb41b22b2ed59d89a6625ebe7721797d428cf4df387eea307dd55c872ab`. Every frame is static; the maximum is 3,968 bytes. Instrumented
linking reproduces the exact executed ELF. No older results are overwritten.

All 108 preserved scenarios/110 transactions pass in three SBF runs, with
106 independent native vectors, the packaged 13-case CLI corpus, native lifecycle
handler tests and isolated ordinary-Go service tests/dependency checks. Both
legacy and managed service modes decode canonical types, run shared quote logic
and construct their actual generated instruction codecs without runtime/compiler
imports or duplicate model/math. Independent vectors verify the new managed pool
and create/close/swap argument formats while preserving the legacy formats.

The legacy wide swap now uses 46,016 CU. Its earlier swap-only module used
45,941 CU/21,536 ELF bytes. The expanded 68,320-byte program includes creation and
closing as well as swapping; it is not an equivalent ELF scope to the preserved
manual 8,064-byte/20,329-CU swap-only baseline. Full comparative measurements remain
open. See the [lifecycle proof](../2026-10-06-managed-swap-supported/README.md)
for the new managed instruction corpus executed against this same ELF.

```sh
python3 scripts/framework_swap_verify.py --output results/compiler/new-swap-proof
```

This local SBF result is not a fresh-validator check, production audit or external
trial. Native callbacks intentionally do not supply whole-transaction atomicity.
