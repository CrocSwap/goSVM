# Generated escrow Clock regression

Passed October 5, 2026 with the Clock-enabled frontend and SDK-2 snapshot.
[summary.json](summary.json) binds the actual sources, commands, native checks,
SBF executions and artifact hashes. The program reproduces its previous ELF
byte-for-byte: `4b858bceb77129196635f0873d6c61357180de013a7f90106a802fe7042c93f3` (25,280 bytes).

All 32 scenarios/42 transactions pass in three runs, with fresh CLI scaffold,
unchanged generated artifacts, native lifecycle tests, 27 invalid-declaration
checks, the multi-init precondition test and independent packaged fixture
agreement. [Previous proof/methodology](../2026-10-05-escrow-supported/README.md)
records the cancellable SOL escrow scope and limitations. Clock is unreachable
from this example; rent-funded init, claim/cancel, authorized close/reuse and
failure/transaction rollback behavior remain unchanged.

```sh
python3 scripts/escrow_verify.py --output results/compiler/new-regression
```

Use a new result directory. This is local LiteSVM evidence, not a fresh validator
or external developer trial.
