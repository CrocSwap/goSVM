# LiteSVM runner compatibility spike

This developer experiment exercises the existing SBFv3 Go, lean Rust, and Anchor
programs through a shared local LiteSVM process. It is the first slice of roadmap
milestone 1. The full validator remains the default and the compatibility oracle.
This is not a Developer ID signed or supported runner release. The subsequent
[transport comparison](../../results/svm/2026-10-05-transports/README.md) selected
stdio batches, now used by `gosvm test --svm`. This document also retains the
original HTTP compatibility-spike reproduction. See the
[protocol/workflow](../../docs/SVM_RUNNER_PROTOCOL.md) for the new CLI path.
The [pinned local package and installer](../../docs/SVM_RUNNER_PACKAGING.md)
now support sharing the macOS arm64 binary without Cargo in each test workflow.
Public release signing/downloads and other host validation remain open.

The adapter implements only the local fixture RPC methods used by
`internal/sbftest` and the token verifier. It executes real compiled ELFs,
signature/blockhash checks, and token CPIs. Readiness and confirmation responses
are synchronous test markers; they do not model consensus, banks, slots, or
commitment levels. Unknown methods and runner arguments fail explicitly.

## Reproduce

Prepare the six matched ELFs using [the Anchor instructions](../anchor/README.md).
The runner is a separate Rust host project; use host Cargo/Rust rather than the
SBF Rust compiler. Rust 1.86 or newer is required by LiteSVM 0.8.2. Commit the
lockfile: freely upgrading transitive Solana dependencies produced incompatible
hash types during bring-up. The resolved Agave runtime is 3.0.10; the validator
is 3.0.15. This difference is an explicit compatibility limit.

```sh
python3 scripts/svm_spike.py \
  --output results/svm/new-experiment \
  --samples 3 --cold-runner-build
```

Run from the repository root. The command refuses existing output/staging
directories. It builds the runner once, compiles/signs the Go verifier, copies
existing contract ELFs without rebuilding them, and runs both engines serially.
The empty-target runner build uses installed tools and cached crate sources;
downloads and Go contract build times are not part of fixture execution timings.

For each repetition, the validator captures its exact SPL Token ELF and active
feature IDs. The runner uses both inputs. Unknown feature IDs fail rather than
silently falling back. Feature activation slots are normalized to zero; sysvars
and wider runtime behavior are not proven equivalent. Normal runner startup
without a feature fixture uses LiteSVM's all-enabled defaults, which differ from
this validator snapshot by the Alpenglow feature. Matching requires the fixture.

Both engines retain the existing byte-exact application state checks and exact
failure/CPI/rollback assertions. The script also asserts identical reported CU,
errors, ELF/fixture hashes, token image hashes, and active feature IDs. It saves
each repetition, source/binary hashes, versions, bootstrap costs, footprint, and
startup/fixture/wall times. Validator confirmation latency is included in its
fixture wall time; the runner has no network/consensus confirmation delay.

## Use the prototype explicitly

```sh
cargo build --release --locked \
  --manifest-path benchmarks/svm-runner/Cargo.toml \
  --target-dir build/svm-runner-target
bash scripts/build-cli.sh build/gosvm
GOSVM_TEST_RUNNER="$PWD/build/svm-runner-target/release/gosvm-svm-runner" \
  build/gosvm test --sbf -dir examples/typed-swap
```

`GOSVM_TEST_RUNNER` selects the sidecar for generated-starter tests and
token comparisons. `GOSVM_VERIFY_BUILD_DIR` selects a separate verifier experiment
directory. `GOSVM_CAPTURE_TOKEN_ELF` captures the validator's token image;
`GOSVM_TEST_TOKEN_ELF` supplies it to the sidecar. `GOSVM_TEST_FEATURES` supplies
the JSON array of active feature IDs. These are experimental harness controls,
not stable public settings. Ordinary `gosvm test --sbf` remains validator testing.

## Architecture decision

LiteSVM is the first candidate because our acceptance fixtures include multiple
instructions, persistent state, and transaction rollback. Its
[transaction API](https://docs.rs/litesvm/0.8.2/litesvm/struct.LiteSVM.html)
provides simulation and submission without starting a validator. The chosen
0.8.2 release has an Agave 3.0 dependency family, close to the pinned test setup.
The compatibility decision comes from executing our fixtures, not from this API
description.

The prototype uses a persistent loopback HTTP process to reuse the existing
assertions without migrating transaction construction. One process handles a
whole token suite or a whole bounded backend suite; none is spawned per fixture.
Go developers can run an already-built binary without Cargo. Current source
bootstrap still requires Rust and is measured separately. No public downloadable
verified binary has been shipped.

The subsequent comparison measured a small embedded C ABI around the same pinned
core and found modest savings compared with stdio batches. Stdio is selected for
the first CLI path. The current [upstream Go project](https://github.com/LiteSVM/litesvm-go)
uses a different pure-Go engine and remains an independent compatibility option;
it has not been integrated here. Original HTTP overhead includes Go transaction
construction/assertions as well as transport, so the later controlled replay is
the appropriate comparison for choosing a transport.

## Remaining milestone work

The new CLI supplies fixture selection and an experimental versioned stdio
protocol. Runner 0.3.0 adds typed Clock/Rent controls and reusable VM checkpoints;
the CLI restores initial state between fixtures. See
[validation](../../results/svm/2026-10-05-controls-final/README.md).
Runner 0.4.0 added atomic reset overrides and
[general multi-account fixtures](../../docs/SVM_FIXTURES.md). Other sysvars,
public checked downloads/signing, and a supported host matrix remain open.
Runner 0.5.0 uses a provenance-bound
[rent-error precedence patch](../../third_party/litesvm-0.8.2/GOSVM_PATCH.md).
[Lifecycle validation](../../results/svm/2026-10-05-lifecycle-verified/README.md)
covers transaction-created mints/token accounts, a Go swap, close/recreate, rent
rejection and rollback. New-account `rent_epoch` metadata remains explicitly
different from the validator. Broader bank behavior, System CPI authoring,
resizing, and other sysvars remain open. Review the selected
protocol's compatibility guarantees before public alpha.
Keep full-validator tests accessible for behavior the sidecar cannot represent.
