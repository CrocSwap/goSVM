# Ordered Clock and Rent fixture validation

October 5, 2026, macOS arm64. General fixture steps now accept a `sysvars` object
before their transaction. The six-scenario/15-transaction corpus passed the
initial independent state/error checks, repeated runs and a CU-pinned replay.
The unchanged runner 0.5.0 is installed from the previously pinned package.
This adds fixture authoring and compatibility evidence, not a new runtime binary
or Go SDK sysvar APIs.

## Checked behavior

The 1,664-byte SBFv3 C probe is byte-identical to the earlier Clock/Rent probe.
It calls real Clock/Rent syscalls, writes their values into an owned account and
can write then fail. Expectations are encoded independently with Python's
little-endian packing: 40-byte Clock, 17-byte serialized Rent, and 72-byte probe
state including ABI padding/counter. Sysvar accounts are watched outside the
transaction message and checked separately from syscall output.

Coverage includes:

- Clock-only, Rent-only and combined updates, with omitted controls inherited
  until another update or scenario reset.
- A slot above 2^53, uint64 maximum, int64 minimum/maximum and fractional Rent
  thresholds, preserved exactly in bytes and reports.
- Two-instruction atomic rollback and single-instruction mutation/failure,
  followed by successful reads of the still-current controlled sysvars.
- Repeated fresh-state scenarios, standalone selection and reversed scenario
  order reproducing the same results.
- Explicit blockhash renewal for an otherwise identical transaction. A negative
  test proves Clock changes alone leave duplicate history/blockhash intact.
- Whole-suite preflight rejection of incomplete, empty, null and out-of-range
  controls even in an unselected scenario, with stale passing reports removed.

Host controls apply before transaction construction. Failed transactions roll
back application writes; they do not undo the already applied host controls.
Each next scenario resets accounts, sysvars, fees, history and blockhash to the
initial checkpoint. These controls do not imply coherent bank/consensus advancement.

Successful single probes consume 735 CU, failed single probes 734 CU, and the
two-instruction failing transaction 1,469 CU. CU was calibrated only after the
first independent byte/error checks passed, then checked in subsequent runs.
These are runtime-probe measurements, not a Go/Rust comparison or fresh validator
CU equivalence. Uncontrolled fixtures retain their existing result shape.

## Evidence and reproduction

- [Summary](summary.json): commands, source/artifact/fixture hashes and scope.
- [Initial fixture file](fixtures-initial.json) and [CU-pinned fixture file](fixtures.json).
- Reports [initial](ordered-0.json), [second](ordered-1.json), [third](ordered-2.json)
  and [pinned replay](ordered-results.json); the initial report binds the initial
  fixture hash, and subsequent reports bind the CU-pinned file.
- [Standalone selection](selected.json), [reversed fixtures](reversed.json)
  and [reversed results](reversed-results.json).
- [Duplicate rejection](duplicate-rejection.log), [incomplete controls](incomplete-rejection.log),
  [empty controls](empty-rejection.log), [null controls](null-rejection.log),
  [bad Rent](bad-rent-rejection.log).
- Existing [108-scenario full-token](full-token.json),
  [six-scenario/26-transaction lifecycle](lifecycle.json) and
  [14-case starter](starter.json) regressions retain exact prior case results.
- [Repository checks](checks.json).

```sh
python3 scripts/svm_scenarios.py --output results/svm/new-scenarios --samples 3
```

Use the repository's installed pinned Go/LLVM, existing token artifact and recorded
local runner archive. The script builds an isolated frontend, installs the runner
into a separate cache, compiles the probe and refuses existing result/staging
directories. Original Go programs and Basanos are not edited. The earlier
[pilot](../2026-10-05-scenarios/README.md) is preserved.

The local RPC bind probe returned `Operation not permitted`. No new validator
process ran, and no parity claim is made for arbitrary controlled Clock/Rent
values. Existing application regressions retain their captured validator evidence.
Other sysvars, SDK authoring, supported hosts and public distribution remain open.
The [bounded milestone 1 checklist](../../../docs/SVM_RUNTIME_COVERAGE.md)
separates these future features from the final compatibility/clean-host gates.
