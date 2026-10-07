# LiteSVM rent error compatibility patch

This directory retains the crates.io LiteSVM 0.8.2 package, under Apache 2.0,
with one local source change. `GOSVM_PROVENANCE.json` records its registry archive
checksum, upstream Git revision, and every original package file hash. Registry
cache marker/checksum files are omitted. The standard Apache license text is
included as `LICENSE-APACHE`; Cargo declares this license for the upstream crate.

In `src/lib.rs`, post-execution rent checks run only when `process_message`
succeeds. Previously those checks could replace an existing instruction failure
with `InsufficientFundsForRent`. A transaction containing System `CreateAccount`
with one lamport below exemption, then SPL `InitializeAccount3`, demonstrates
this: validator 3.0.15 returns instruction 1, `Custom:0` (NotRentExempt), whereas
unpatched LiteSVM returns the later rent-state error. Both retain the same logs
and CU. The patch preserves the first error and keeps successful-execution rent
checks, signature verification, fee collection, and rollback unchanged.

The runner uses this path through `[patch.crates-io]` and identifies itself as
0.5.0 with the rent-error patch. Transitive package versions remain pinned to the
previous lock; only the runner version and LiteSVM source selection changed.
No registry cache files were edited. This is an experimental local compatibility
patch, not a released upstream version.

Reproduce with `scripts/svm_lifecycle.py`. Its failed-initialization case checks
simulation, submission status, unchanged funder balance, absent created account,
and unchanged mint, against a fresh validator and three fast-runner repetitions.
It also checks that successful underfunded creation still fails rent validation.

`rent_epoch` remains an explicit metadata difference: LiteSVM initializes new
accounts to zero, and the validator bank uses `u64::MAX`. The patch does not
normalize it. Fixtures check separate, exact engine expectations and retain
both fixture files. See the lifecycle results and root handoff for scope.
