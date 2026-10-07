# LiteSVM compatibility and latency results

October 5, 2026. The first roadmap runner spike passed the existing matched
bounded and token suites with identical application outcomes, error categories,
CPI checks, and CU. This supports continuing with a shared prebuilt runner.
Milestone 1 remains open: architecture, fixture controls, host verification,
and distribution are not settled.

## Matched execution

Three serial repetitions ran all six existing SBFv3 artifacts: Go, lean Rust,
and Anchor 0.32.2, for the generated bounded workload and full token swap.
Per engine and repetition, the existing harness checked:

- 315 token simulations, six committed swaps, and six token rollback checks.
- 42 bounded simulations and submissions, including persistence and rollback.
- Exact errors, expected pool/token account bytes, CPI invocation/success counts,
  and CU. Both engines retained the original assertions.

Across three repetitions this is 945 token simulations and 126 bounded cases
per engine, plus 18 committed token swaps and 18 token rollback checks. The
dependency-heavy protocol workload was not run.

The runner loaded the validator's exact SPL Token ELF and 237 active feature IDs,
captured in each `sample-*` directory. The script asserted identical program,
fixture and token hashes, reported errors, CU, and feature IDs. Default LiteSVM
enables one extra feature, Alpenglow; explicit fixtures remove that difference.
Unknown requested features fail explicitly.

The Go wide token swap remains 20,329 CU, lean Rust 20,392, Anchor 19,431. The
successful bounded swap remains 486, 348, and 768 CU respectively. These are
different workloads; changing the test engine did not change their CU ordering.

## Latency and build cost

Medians of three observations on the shared Apple M2/macOS arm64 desktop:

| Measure | Full validator | LiteSVM sidecar |
| --- | ---: | ---: |
| Whole token comparison process | 9.22 s | 0.87 s |
| Token startup observed by Go harness | 2.23 s | 0.070 s |
| Token fixture phase | 6.09 s | 0.169 s |
| Whole bounded comparison, three backends | 25.44 s | 0.188 s |

Whole-process times include fixture preparation, runtime version checks,
startup, execution/assertions, reporting, and shutdown. The bounded comparison
starts one engine for each backend. These use already-built contract ELFs and
a prebuilt verifier/runner; contract builds, native tests, CLI generation,
and installation are excluded.

Internal token VM simulation/submission calls took 0.0875, 0.1388, and 0.0865 s.
This excludes HTTP transport and Go fixture/assertion work. Validator fixture
time includes confirmed-transaction polling; the sidecar has no consensus step.

Runner source bootstrap took **99.15 s** from an empty Cargo target with installed
host Rust 1.98.1 and cached crate sources. Its retained target immediately after
that build was **386.2 MiB**. The stripped runnable binary is **6,800,064 bytes
(6.49 MiB)**. Go verifier compilation took 1.56 s, excluding signing. Download
and signed-release installation costs were not measured; no runner was published.
Source bootstrap requires Cargo, while running an existing binary does not.

Token runner process observations ranged from 0.50 to 1.93 s; bounded runs from
0.12 to 0.24 s. Validator always preceded the runner, CPU load was not isolated,
and there are only three observations. These establish lower overhead for these
fixtures, not portable latency guarantees or superiority over all Rust test tools.

## Compatibility limits

Locked LiteSVM 0.8.2 uses Agave 3.0.10 and solana-sbpf 0.12.2, versus validator
3.0.15 and platform-tools v1.51 contract artifacts. Feature IDs and observed
results agree; the entire runtime versions do not. Activation slots are
normalized to zero. Clock/sysvars, initialization, rent/fees, closing/resizing,
and broader application behavior have not been established as equivalent.

The persistent loopback HTTP adapter exposes the small fixture RPC surface.
Readiness and confirmed status are synchronous test markers, not simulated
bank/consensus behavior. State assertions cover application account data, not
fee-payer/rent economics. Signed downloads, clean-machine installation, other
hosts, selection/snapshots, and embedded Go bindings remain untested.

Bring-up required a compatible lockfile after Solana patch updates produced
incompatible hash types, and explicit token-image capture after bundled Token
hashes differed. Both were resolved before these measurements. Exploratory
runs under ignored `build/svm-spike` are not this evidence.

## Validation and reproduction

Root `go test ./...`, `go vet ./...`, and separate generated-module tests passed.
The Rust release-profile test checks unknown-feature rejection and exact subset
selection. A rebuilt CLI passed all 14 starter fixtures through the sidecar,
including persistent state and atomic rollback.

See [runner methodology](../../../benchmarks/svm-runner/README.md) and
[`summary.json`](summary.json) for reproduction, every sample, versions,
binary/source hashes, and footprint distinctions. Reproduction needs the six
documented matched ELFs and writes to a new directory. Historical hashes bind
this checkpoint, not future source edits.

Next: compare batched/stdio and embedded execution, choose the runner protocol,
then add a CLI testing level with fixture controls. The separate
[binding design](../../../docs/MULTI_ACCOUNT_DESIGN.md) scopes the private alpha.
