# Explicit target and Bend consumer acceptance — October 7, 2026

**Local acceptance passes for fogo-v0 and modern solana-v3 using the content-pinned
platform-tools v1.57 backend on macOS arm64.** Historical v1.51 experimental v3,
old runners and benchmark results retain their original meaning.

| Workload | Scenarios | Transaction steps | Repetitions / profile |
| --- | ---: | ---: | ---: |
| Same generated full token swap | 13 | 15 | 3 |
| Same managed swap lifecycle | 29 | 36 | 3 |
| Actual Clock accessor / sysvar controls | 12 | 15 | 3 |
| Linked Go/Bend pure-library fixture, v0 only | 9 | 9 | 3 |

Each fixture checks simulation and submission, exact errors and account bytes /
metadata, including failed CPI and transaction rollback. Lifecycle coverage
includes real System creation, token initialization/transfers, PDA signing,
close/reuse and rent failures. Clock covers actual syscall bytes and reward/time
controls. One Bend transaction succeeds then encounters malformed input and
must roll back. Native cgo tests additionally cover accepted/refused/stop results,
malformed canonical encodings, nil/short buffers and overlapping slices.

The full swap ELF is 69,032 bytes for v0 and 69,352 bytes for modern v3.
The actual linked Go/Bend image is 4,664 bytes. ELF hashes and per-case CU are
identical across repetitions. These are **compatibility measurements**, not a
matched CU comparison: the profiles use different local runtime engines/features.

The accepted Bend manifest is copied unchanged into
[bend-producer.manifest.json](bend-producer.manifest.json), SHA-256
`ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de`.
The producer remains v1.53 / fogo-v0, while the caller is v1.57 / fogo-v0.
GoSVM links the actual selected object; it supplies its own memory helpers and
binds library calls within the final image. The producer's
`finalElfAuthorized:false` flag remains unchanged. Solana v3 linking refuses this
v0 library. A new root, producer/compiler pin or target needs fresh qualification.

The newly scaffolded schema-1 starter also passes all 14 native/SBF fixtures on
both profiles, including guards and atomic rollback.

Additional checks pass: root `make test` with the opt-in producer integration,
`go vet ./...`, the independent native Go service, six modern-runner unit tests,
exact migration byte preservation for schema 1/2, backend content/cache mutation,
backend import/status/idempotency and tampered-receipt rejection. Library mutation
checks reject wrong target/tools/root/width/fingerprint/selection, wrong host,
changed object bytes, divergent rehashed native adapters and extra native sources.

[metrics.json](metrics.json) retains compact CU/error observations, ELF/fixture /
runner hashes and frontend/SDK source pins. [qualification.json](qualification.json)
records backend hashes, consumer fixture/binding pins and additional checks. Raw
reports/logs and executable artifacts are local-only; no regenerated binary,
archive or Cargo build directory is added to Git.

## Reproduce

Prepare a historical 0.5.0 rent-patched runner according to its existing packaging
docs; it is not replaced by this work. Prepare the separate modern runner and CLI:

```sh
bash scripts/build-cli.sh build/target-gosvm
python3 scripts/build_modern_runner.py
python3 scripts/verify_target_profiles.py \
  --output results/compiler/my-new-target-run \
  --v0-runner /path/to/historical-0.5-runner \
  --v3-runner build/modern-runtime-target/release/gosvm-svm-runner \
  --bend-manifest ../bendSVM/build/interop/fogo-v0/20261007T134003Z-0bdd2144/library.manifest.json
```

The output directory must be new. The script verifies unchanged application,
module, generated client and IDL bytes across profile selection, collects three
runs per corpus, performs native cgo tests and refuses a v3 mixed image. Pin the
same producer manifest explicitly if using another artifact location. Artifacts
must accompany its manifest. A changed frontend pin produces a separate dependency
output namespace and requires a new results directory.

## Qualification boundary

V0 uses the historical LiteSVM 0.8.2 rent-patched engine with the observed local
validator 3.0.15 features. Modern v3 uses LiteSVM 0.13.0 / Agave 4.0 / SBPF 0.14.4,
all features enabled and the equivalent isolated rent-error precedence patch.
The unpatched modern engine replaced one failed CPI error with a later rent
failure; the pinned build script preserves the first instruction error while
retaining successful-execution rent validation. Original fixtures were not relaxed.

This qualifies these representative local programs and this mixed final image.
It does not qualify every host/backend package, arbitrary Go/Bend roots or
compiler combinations, a modern Solana validator, or either public network.
**No Fogo/Solana network deployment or network transaction validation was performed.**
Bend solana-v3 remains unavailable. See [profiles](../../../docs/TARGET_PROFILES.md)
and [library integration](../../../docs/BEND_LIBRARIES.md).
