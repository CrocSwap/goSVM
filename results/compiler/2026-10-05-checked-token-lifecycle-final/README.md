# Current checked token/PDA/CPI regression

October 5, 2026, macOS arm64. [Summary](summary.json): PASS.
After the generated framework migration and token copying changes, this proof
repeats the complete checked-transfer/generic-CPI corpus: 116 native vectors,
1,011 native-Go/C CPI-boundary vectors and 117 compiled-SBF scenarios in three
runs, including 15 expected failures. It covers unsigned/PDA-signed transfers,
multiple typed/key/bump seeds, two signer groups, current balances, generic System
transfers, exact downstream errors and failed-CPI/transaction rollback.

The ELF is 14,784 bytes, SHA-256
`59e069fa76f0439579ff1c43b7a1ba01b7a105f5b6b5c05494089032f2b9002d`.
Its largest static frame is 3,648 bytes; stack-instrumented linking reproduces the
artifact hash. Source, runner, fixture and command identities are bound by the
summary. SDK-2 scaffolding/native tests and helper tests pass separately.

This preloaded compiler fixture differs from the
[generated full swap](../2026-10-05-framework-swap-lifecycle-final/README.md), bounded
starter and dependency-heavy protocol. There is no fresh-validator, clean-host,
initialization/closing or external developer-trial claim. The
[earlier proof](../2026-10-05-checked-token-supported/README.md) is preserved with
its own original source/artifact hashes and frame measurements.

```sh
python3 scripts/checked_token_verify.py --output results/compiler/new-checked-token-proof
```

Choose a new directory and use the pinned local runner/archive and installed
v1.51 LLVM described by the [SDK reference](../../../docs/SDK2.md).

This rerun uses the lifecycle SDK and preserves the preloaded workload.
The separate [lifecycle proof](../2026-10-05-lifecycle2-supported/README.md)
validates rent/System/owned lifecycle boundaries; it does not provide generated
application initialization/close policy. Historical result directories retain
their own original measurements and source hashes.
