# SDK-2 decoder regression: framework-swap

108 scenarios / 110 transactions pass three actual-SBF runs with the expanded 32-account decoder. Native module/helper checks and static frame verification also pass. See `summary.json` for pinned source, commands, frames and results.

The historical 17-account case now reaches the generated handler and returns account-count error 6000 instead of decoder 1001. `error-mapping.json` records this change; native validation includes the case and state/rollback assertions remain intact.

Read [the combined report](../../../docs/ACCOUNT_DECODER32.md). These are local LiteSVM regressions, not fresh-validator or clean-host acceptance.
