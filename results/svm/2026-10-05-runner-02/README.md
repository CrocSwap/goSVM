# Runner 0.2.0 validation

October 5, 2026. The final experimental CLI uses runner 0.2.0, LiteSVM 0.8.2,
and ordered stdio batches. All six transport/batch combinations agreed in seven
seeded shuffled repetitions. The CLI passed all 14 compiled starter fixtures
with exactly matching CU, errors, and ELF hash against a fresh validator 3.0.15
run. Each path also checked simulated/submitted state and transaction rollback.

## Transport follow-up

Median fixture replay milliseconds, excluding initialization and shutdown:

| Transport and batch size | Full token | Bounded Go | Bounded lean Rust | Bounded Anchor |
| --- | ---: | ---: | ---: | ---: |
| HTTP 1 | 480.90 | 19.04 | 33.43 | 45.41 |
| HTTP 64 | 316.13 | 10.09 | 9.17 | 9.43 |
| Stdio 1 | 355.47 | 16.55 | 14.30 | 26.04 |
| Stdio 64 | 290.13 | 7.75 | 7.35 | 9.55 |
| Embedded 1 | 320.52 | 7.86 | 7.04 | 9.07 |
| Embedded 64 | 291.99 | 6.73 | 6.56 | 8.84 |

Stdio/embedded batch-64 startup medians were 38.66/32.83 ms for tokens and
20.22/12.85 ms for bounded Go. Embedding has a small startup advantage; fixture
replay differences are small or mixed. This supports the original
[stdio decision](../../../docs/SVM_RUNNER_PROTOCOL.md).

Shared-desktop load varied substantially between this follow-up and the
[first comparison](../2026-10-05-transports/README.md). Do not infer portable
latency or a universal transport ranking. These are warm replay measurements,
including response validation but excluding fixture construction/signing;
embedded library loading and shutdown are excluded. All transports use the same
pinned Rust core, not the separate upstream pure-Go engine. The dependency-heavy
protocol workload was not run.

The script captured 315 token simulations, six committed swaps, six token
rollback checks, and 42 bounded cases, then compared reports with the preserved
original validator snapshot. This script itself did not start a fresh validator;
the separate CLI validation below did. Full trace replays compare account data,
CU, errors, CPI logs, signatures, and submission/status responses exactly, with
integer precision preserved.

## CLI and protocol validation

- [`cli-full.json`](cli-full.json) and [`cli-full.log`](cli-full.log): all 14
  starter fixtures using runner 0.2.0 and the observed 237-feature local profile.
- [`fresh-validator.json`](fresh-validator.json) and
  [`fresh-validator.log`](fresh-validator.log): fresh full-validator starter run;
  per-case reports and ELF hash exactly match the fast run.
- [`cli-selected.json`](cli-selected.json) and [`cli-selected.log`](cli-selected.log):
  `-svm-run 'rollback$'` selects exactly one fixture, with 765 CU and checked rollback.
- [`old-runner-rejection.log`](old-runner-rejection.log): the new CLI rejects the
  earlier HTTP-only 0.1.0 binary and removes the stale passing report.
- [`rust-tests.log`](rust-tests.log): locked release Rust tests pass.

Root `make test` and `go vet ./...`, generated-starter tests, and separate
transport-module tests/vet passed for this source revision. Client tests cover
malformed responses, missing/mismatched IDs, cancellation, feature overrides,
selection failures, and stale-report removal. The macOS root test target uses
external linking for the pinned Go 1.22 toolchain.

[`summary.json`](summary.json) records all medians, source/trace/artifact hashes,
cached build times, and measurement limitations. [`replays.json`](replays.json)
contains individual observations. Reproduce into a new directory using
[`scripts/svm_transport.py`](../../../scripts/svm_transport.py); see
[methodology](../../../benchmarks/svm-transport/README.md).

The CLI is still experimental and schema-1 starter-specific. Controlled sysvars,
snapshots/general fixtures, lifecycle/rent coverage, verified packaging, and
other-host validation remain open. Full-validator testing remains available.
