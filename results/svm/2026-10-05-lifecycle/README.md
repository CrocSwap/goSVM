# Unpatched lifecycle investigation

The fresh validator 3.0.15 passed the six-scenario, 26-transaction lifecycle corpus.
The unpatched LiteSVM 0.8.2 fast run stopped at underfunded token initialization:
its post-execution rent check replaced the original SPL instruction error.
The validator fixtures, ELF, features, full result and logs are retained here.
No summary or performance claim was produced by this incomplete experiment.

See [patched validation and limits](../2026-10-05-lifecycle-verified/README.md).
