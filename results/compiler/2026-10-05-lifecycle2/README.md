# Lifecycle fixture bring-up attempt

**Not a passing proof.** Native/C checks, compilation and stack linking passed;
the fixture placed `simulation_accounts` outside `expect`, and the CLI rejected
that unknown field before SBF execution. Original logs and `passed:false` summary
are preserved. The [final proof](../2026-10-05-lifecycle2-supported/README.md) uses
the correct fixture shape and includes explicit rent funding before growth.
