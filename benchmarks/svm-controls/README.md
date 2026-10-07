# SVM control validation

This runtime-only SBFv3 C probe reads Clock and Rent through real syscalls,
writes their fields into an owned account, and can mutate then fail to check
rollback. It does not add sysvar authoring APIs to the Go compiler or SDK.

From the repository root, with pinned tools already installed:

```sh
python3 scripts/svm_controls.py --output results/svm/new-controls-experiment
```

The script refuses existing output/staging directories. It builds the locked
runner, compiles the probe with platform-tools v1.51, runs the opt-in Go
integration test three times, and checks ordinary/controlled starter fixtures
against a fresh validator 3.0.15 run. It also rejects incomplete controls and
checks stale-report removal. Logs, hashes, and separate fast/validator reports
are saved. `--llvm` selects an explicit backend path; source bootstrap requires
host Rust with cached locked dependencies, Go, Clang, and the validator.

The test checks serialized sysvar accounts and SBF syscall output independently,
including a slot above 2^53 and negative timestamps. Repeated restoration must
restore application/payer balances, transaction history, submission statuses,
sysvars, and blockhashes. A new snapshot captures non-genesis state. Expiring a
blockhash rejects the old hash until reset restores it. Identical signed
transactions must be rejected as duplicates before reset and succeed afterward.

The probe uses fixed syscall hashes from the SBF murmur3 convention. Only macOS
arm64 has been executed. This is correctness evidence, not a comparative CU,
memory, timing, full lifecycle, or general rent-collection benchmark. The
generated starter remains schema 1 with one account; broader fixture authoring
and Go SDK sysvar APIs are separate work.

See [protocol](../../docs/SVM_RUNNER_PROTOCOL.md) and
[latest evidence](../../results/svm/2026-10-05-controls-final/README.md).
