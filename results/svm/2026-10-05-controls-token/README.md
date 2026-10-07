# Runner 0.3.0 token regression

October 5, 2026. The updated runner reproduced the preserved validator snapshot
for 315 full-token simulations, six committed swaps, six rollback checks, and
42 bounded cases across Go, lean Rust, and Anchor. Reports match on case results,
errors, CU, ELF/fixture hashes, token image, and active feature IDs. The full
token suite uses real SPL Token CPIs.

Captured traces then matched complete responses through HTTP, stdio, and
same-core embedding, each with batches of one and 64, in one shuffled repetition.
See [`summary.json`](summary.json), [`replays.json`](replays.json), and capture
reports. This is a compatibility regression, not a repeated timing comparison;
the shared desktop also ran root tests during preparation. No new transport
ranking is claimed.

This script reused the original validator snapshot and did not rerun the token
suite on a fresh validator. The separate
[control experiment](../2026-10-05-controls-final/README.md) ran a fresh validator
for the 14-case starter. The dependency-heavy protocol was not run. Captured
sources precede the last CLI-help/test refinements; hashes bind this snapshot.

Reproduce using [`scripts/svm_transport.py`](../../../scripts/svm_transport.py)
into a new directory; see [methodology](../../../benchmarks/svm-transport/README.md).
