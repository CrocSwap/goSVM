# SVM transport comparison results

October 5, 2026. Seven seeded shuffled repetitions reproduced complete fixture
responses across HTTP, stdio, and same-core embedding, each with batches of one
and 64 calls. Select batched stdio for the initial CLI workflow: embedding saves
only a few milliseconds on these fixtures and adds a native integration boundary.

## Matched behavior

The refactored HTTP runner passed the existing 315 token simulations, six
committed swaps, six token rollback checks, and 42 bounded cases. Captured reports
agree with the earlier validator snapshot on case results/errors, CU, ELF hashes,
and feature IDs. This experiment reuses that snapshot; it does not rerun a validator.

Four traces capture initial fixtures/programs and every non-timing RPC response:
414 calls for the full token suite and 71 for each bounded backend. Replays compare
complete JSON values, preserving integer precision, account data, errors, CU, CPI
logs, signatures, and submission/status responses. Object key order is irrelevant.
All six combinations and all seven repetitions agree. The same pinned LiteSVM
0.8.2/Agave 3.0.10 core serves every mode; features and SPL Token image match the
earlier snapshot. The dependency-heavy protocol workload was not tested.

## Replay measurements

Median fixture replay milliseconds, excluding initialization and shutdown:

| Transport and batch size | Full token | Bounded Go | Bounded lean Rust | Bounded Anchor |
| --- | ---: | ---: | ---: | ---: |
| HTTP 1 | 215.16 | 10.96 | 11.88 | 11.42 |
| HTTP 64 | 145.91 | 4.12 | 4.88 | 4.63 |
| Stdio 1 | 150.14 | 5.88 | 5.30 | 6.64 |
| Stdio 64 | 147.86 | 4.84 | 4.43 | 5.18 |
| Embedded 1 | 141.06 | 3.92 | 4.34 | 4.66 |
| Embedded 64 | 140.21 | 3.18 | 3.28 | 4.05 |

For token batches of 64, startup medians were 24.91 ms HTTP, 23.94 ms stdio, and
16.04 ms embedded. For the bounded Go starter they were 13.24, 11.65, and 6.32 ms.
Embedded library loading is excluded; child startup and VM initialization are
included. These replay times include response validation but exclude Go fixture
construction/signing, unlike the previous whole-verifier measurements. They
should not be compared as if they measured the same complete workflow.

The measured sidecar binary was 6,835,264 bytes; the dynamic library 6,336,208;
the experimental cgo comparison driver 5,448,016. These are different artifact
roles, not a full installed-builder comparison. The CLI uses a separate runner
binary and does not need cgo to use it. No signed/downloadable distribution was
produced. Timing used the shared M2 desktop with warm source/tool caches and
without isolated CPU/load control. Differences of a few milliseconds are not
portable guarantees.

## CLI validation

A new `gosvm test --svm` path passed all 14 starter fixtures with identical CU,
errors, ELF, and application-state checks versus the validator snapshot. The
full run and a selected `rollback$` run are saved separately. The rollback
selection ran exactly one fixture and checked persisted rollback with 765 CU.

The initial full fast-runner phase took 0.063 s in one observation, excluding
native tests and contract compilation. This is a smoke observation, not a
repeated end-to-end benchmark. Root tests/vet, protocol failure/cancellation
tests, Rust session tests, and focused nested comparison-module tests passed.
Root tests on this pinned macOS/Go combination now use `make test` for external
linking when test binaries import the HTTP harness.

The CLI pins the observed local validator feature profile, supports fixture-name
selection, rejects empty selections/conflicting modes, and removes stale fast
reports before a new run. It does not provide clock/sysvar controls or verified
downloads. See [the protocol decision](../../../docs/SVM_RUNNER_PROTOCOL.md).

## Evidence and reproduction

[`replays.json`](replays.json) contains every observation; [`summary.json`](summary.json)
records medians, artifact/source/trace hashes, scope, and limitations. The first
measurement used equivalent manual build/capture commands; the saved bootstrap
field says so rather than presenting unrecorded build times. The subsequent
[script reproduction](../2026-10-05-transport-reproduction/summary.json) passed all
modes in one repetition. See [methodology](../../../benchmarks/svm-transport/README.md)
for fresh-directory reproduction. Later metadata/CLI diagnostics refinements
are separate from the measured source hashes; this remains a historical snapshot.

The embedded ABI is an experiment around the pinned Rust core. It is not a test
of the changed upstream pure-Go engine. Current coverage remains preinitialized
bounded/token workloads; runtime-version, sysvar, lifecycle, fee/rent, packaging,
and other-host limits from the first spike still apply.
