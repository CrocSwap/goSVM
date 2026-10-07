# Clock/Rent and checkpoint validation

October 5, 2026. Runner 0.3.0 adds typed Clock/Rent controls, one reusable VM
checkpoint, reset, and explicit blockhash expiry. Fast starter tests restore the
initial checkpoint before every fixture. All 14 cases match a fresh validator
3.0.15 run on CU, errors, ELF, fixture hashes, and asserted application state.

The compiled SBFv3 probe passed three repeated integration runs. It reads real
Clock/Rent syscalls into an owned account; serialized sysvar account bytes are
checked separately. Coverage includes a slot of 9,007,199,254,740,993, negative
timestamps, updated Rent values, successful mutations, failed mutations with
rollback, and repeated restoration of both initial and non-genesis checkpoints.
The successful probe consumed 735 CU; this measures a runtime probe, not a Go/Rust
application comparison.

Reset restored state, payer fees, sysvars, duplicate-transaction history,
submission statuses, and blockhashes. An expired blockhash failed until reset
restored it. The same signed transaction failed as a duplicate before reset and
succeeded afterward. This caught a LiteSVM clone behavior: an empty history map
loses spare capacity, silently disabling duplicate checks. New/cloned VMs now
explicitly configure history capacity. Rejected controls leave both sysvars
unchanged; incomplete CLI controls fail and remove stale passing reports.

Fast reports preserve integer precision. The first
[attempt](../2026-10-05-controls/README.md) caught large-slot rounding in Go's
untyped metadata decoding. The final path retains the raw JSON values.

## Evidence

- [`controls.log`](controls.log): three compiled-probe integration runs.
- [`rust-tests.log`](rust-tests.log): feature, protocol, invalid-control atomicity,
  and reusable-checkpoint tests.
- [`cli-full.json`](cli-full.json), [`cli-sysvars.json`](cli-sysvars.json), and
  [`validator.json`](validator.json): 14 fixtures under default/explicit sysvars
  and a fresh validator. The starter does not itself consume Clock/Rent; the
  separate probe establishes syscall behavior.
- [`sysvars.json`](sysvars.json): an executable example for `-svm-sysvars`.
- [`cli-rejection.log`](cli-rejection.log): rejected incomplete controls, with no
  stale report left behind. [`cli-restored.log`](cli-restored.log) records the
  final complete passing run.
- [`summary.json`](summary.json): source/artifact hashes, repeated probe CU,
  host, scope, and limitations.

Root `make test`/vet, generated-starter tests, and the separate transport-module
tests passed. A [separate token regression](../2026-10-05-controls-token/README.md)
preserves full-token/CPI and three-backend bounded compatibility evidence.
Its snapshot comparisons and transport replay are distinct from this fresh
starter-validator run.

Reproduce into a new directory with
[`scripts/svm_controls.py`](../../../scripts/svm_controls.py); see
[methodology](../../../benchmarks/svm-controls/README.md) and
[protocol](../../../docs/SVM_RUNNER_PROTOCOL.md).

Only Clock/Rent and in-process checkpoints are covered. Controls do not advance
consensus, SlotHashes, or epochs automatically. The probe is C; Go SDK sysvar
authoring, general multi-account fixture files, lifecycle/rent compatibility,
verified downloads, and other hosts remain open. No comparative latency or
memory-footprint claim is made. Basanos was not changed.
