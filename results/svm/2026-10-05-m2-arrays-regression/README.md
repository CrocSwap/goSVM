# Existing-workload regression after fixed-array support

October 5, 2026. All isolated installation/workflow checks pass after the first
milestone 2 array compiler increment. The CLI is source-installed; old application
ELFs are freshly rebuilt, rather than taken from a previous SBF build directory.

The 14-case generated starter, three-case project-general example, 108 full-token
scenarios/110 transactions, six lifecycle scenarios/26 transactions and six sysvar
scenarios/15 transactions match their recorded results exactly. Their ELF and
dependency hashes are unchanged. The metadata-negative check still rejects the
validator's distinct rent epoch and clears a stale report. Native project tests
are forced with `-count=1`; only Go is on the test PATH, with verified managed
backend/runner selection. The runtime package remains at 0.5.0.

- [Summary, source hashes, diagnostics and command logs](summary.json).
- [Starter](starter.json), [project-general](project-general.json),
  [full-token](full-token.json), [lifecycle](lifecycle.json), [sysvars](sysvars.json).
- [Backend receipt](backend-receipt.json), [runner receipt](runner-receipt.json)
  and [metadata rejection](metadata-rejection.log).

The backend is copied from this existing desktop. This is a regression against
captured evidence, not a clean-machine or fresh-validator test. The user closed
milestone 1 by skipping those additional checks; this run does not reopen them
or claim they passed. Historical milestone 1 reports are preserved. See
[array semantics evidence](../../compiler/2026-10-05-arrays-final/README.md).
