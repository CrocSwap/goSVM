# SVM account lifecycle validation

October 5, 2026, macOS arm64. Six scenarios containing 26 transactions passed
three full fast-runner repetitions against the captured validator oracle, plus
a restored final run. Ten expected failures per run check exact categories and
indices, committed rollback, account bytes, ownership, lamports, executable state,
and rent epoch. Reports are linked below. Milestone 1 remains open.

The primary scenario creates and initializes two mints and four token accounts
using the native System program and the validator's exact classic SPL Token ELF.
It mints reserves, executes the Go full-token swap with two real CPIs, drains the
user output account, closes it with a rent refund, and recreates the same address.
Failures cover account reuse, double initialization, wrong mint/close authority,
nonempty close, close followed by a failing instruction, bad allocation size,
insufficient rent, and missing creation signature. A separate probe covers
System transfer, allocate, and assign. Go pool state is still genesis seeded;
this adds testing support rather than Go-authored initialization APIs.

## Evidence and differences

- [Summary, source and binary hashes](summary.json).
- [Validator oracle](validator-results.json), [exact validator fixtures](validator-fixtures.json),
  [captured validator log](captured-validator.log), [features](validator-features.json).
- [Fast fixture file](fixtures.json) with independent layout/arithmetic expectations.
- Fast reports [one](runner-0.json), [two](runner-1.json), [three](runner-2.json),
  and [restored full report](runner-results.json). All case/step CU, errors, logs,
  and checked submission flags match the oracle exactly.
- [Selection](selected-results.json), [rent-epoch rejection](rent-epoch-rejection.log),
  [incorrect close simulation rejection](close-state-rejection.log).
- [Existing 108-scenario full-token regression](general-regression.json),
  [14-case bounded starter](starter.json), [three-case project general workflow](project.json).
- [Rust session tests](rust-tests.log). Root/nested Go checks are recorded in
  [checks.json](checks.json).

Two observations are explicit. First, a successful close returns a zero-lamport
System-owned account in simulation, then an absent account on committed reads.
`simulation_accounts` overrides check each phase precisely. An explicit
`fresh_blockhash` step lets a previously rejected close be retried after state
changes without bypassing signature/duplicate checks.

Second, LiteSVM creates accounts with `rent_epoch=0`; the validator uses
`18446744073709551615`. Separate fixture files assert each engine's exact value.
The script verifies that their only expectation differences are `rent_epoch`,
and proves the validator expectations are rejected by the runner. This is a
metadata compatibility limit, not full whole-account equality. No field is
silently normalized or ignored by the CLI.

The new underfunded initialization case also exposed upstream LiteSVM 0.8.2
replacing SPL's instruction-1 `Custom:0` error with a later
`InsufficientFundsForRent`. Runner 0.5.0 includes a single source patch to run
post-execution rent checks only after successful instruction execution. The
[vendored patch and provenance](../../../third_party/litesvm-0.8.2/GOSVM_PATCH.md)
retain the original package, archive checksum and file hashes. Transitive lock
versions remain unchanged. Successful underfunded creation is still rejected by
rent validation; failed initialization preserves its original error and rollback.
The [unpatched result](../2026-10-05-lifecycle/runner-0.log) preserves the discrepancy.

## Reproduce and scope

Normally run a fresh oracle and three sidecar repetitions:

```sh
python3 scripts/svm_lifecycle.py --output results/svm/new-lifecycle --samples 3
```

This snapshot used the already captured oracle:

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" python3 scripts/svm_lifecycle.py \
  --output results/svm/new-captured-lifecycle --samples 3 \
  --validator-evidence results/svm/2026-10-05-lifecycle
```

The original validator run passed all 26 transactions earlier in this session.
The environment then switched to restricted execution and denied local RPC
binding. The [attempted final fresh run](../2026-10-05-lifecycle-final/validator.log)
records that restriction. This run binds the copied oracle by report hash and
asserts its exact main ELF, token ELF, features, fixtures, and results. It does
not claim that the final patched source ran under a newly started validator.
The patched runner itself executes all transactions again through stdio.

The script builds the original manual Go token swap into isolated staging;
it has no Anchor wire adapter. Its 8,000-byte ELF and this scenario's CU should
not replace the historical 8,064-byte matched Anchor comparison or bounded
starter results. The dependency-heavy protocol was not exercised. Times in
reports are diagnostics, not workflow latency targets or portable rankings.
Go System CPI authoring, pool initialization/closing, resizing, broader bank rent
collection, other sysvars, supported hosts, and packaging remain open. Basanos
source and build outputs were untouched.
