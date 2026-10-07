# Final milestone 2 developer trial kit and maintainer rehearsal

The self-contained `milestone2-trial.tar.gz` is 6,451,677 bytes and includes
70 hash-bound files: matching CLI, pinned runner archive, full-swap/escrow/service
modules and SDK snapshots, documentation, expected reports, verifier and feedback
template. `package.json` records the archive/manifest SHA-256; `manifest.json`
inside the kit binds complete input files and frontend sources.

**Maintainer rehearsal PASS**, extracted outside the checkout into a new
`/private/tmp/gosvm-m2-trial-*` directory. The runner installs into an isolated
cache and both apps check, run forced native tests and reproduce their exact
authoritative ELF hashes. All three packaged SBF suites match expected
case/error/CU/log reports: 13 preloaded swap, 29 managed lifecycle and 32 escrow
scenarios. The separate ordinary-Go service tests and both actual run modes pass,
with no compiler/runtime SDK imports. Generated files and app sources stay exact.
Logs, runtime reports and summary are preserved in `maintainer-rehearsal/`.

This is an existing-host private trial with installed Go 1.22 and v1.51 LLVM.
It does not establish independent developer acceptance, clean-host installation,
public distribution or deployment. `external_trial_complete` and
`independent_developer_acceptance` remain false. No completed external feedback
report exists yet.

Give an independent developer the archive and
[trial instructions](../../../docs/MILESTONE2_TRIAL.md). They extract it in a new
directory, run `python3 verify.py --llvm /absolute/path/to/platform-tools/llvm`,
and return the completed `REPORT.md` plus logs/runtime reports. Review usability,
unsupported features, assistance and any blocking issue before closing the gate.

This package refreshes the trial documentation to include completed measurements
and the final open gate. The previous passing kit is preserved separately.
