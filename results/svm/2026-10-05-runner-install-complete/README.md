# Pinned runner installation validation

October 5, 2026, local macOS arm64. The current CLI installed the pinned runner
0.5.0 package into an isolated managed cache, then selected it without a runner
flag, environment override or runner on PATH. The 14-case bounded starter,
108-scenario full token suite and six-scenario/26-transaction lifecycle suite
passed with their recorded fixture outcomes, CU, errors, logs and ELF hashes.

Only Go was on the validation PATH: Cargo, rustc, host Clang and the validator
were unavailable there. The starter used the existing pinned LLVM by absolute
path. No Rust compilation or download occurred during installation or testing.
These are local installation/regression checks, not fresh-machine verification
or a new latency benchmark. Full-token and lifecycle programs retain their
distinct manual wire adapters and historical fixture sets; the dependency-heavy
protocol and Basanos were untouched.

## Package and evidence

- [Pinned archive](gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz): 3,165,750 bytes,
  SHA-256 `a73c0b01a4c6f6655b305567b0e7c294f53e4e3b0082f3825c8299334be4ee0f`.
- [Package pin](pin.json): binary and all 331 member hashes/sizes.
- [Source and toolchain provenance](provenance.json), including the exact
  Cargo lock, vendored LiteSVM patch and package recipe inputs.
- [Cargo dependency/license inventory](dependencies.json): 286 packages, 118
  without a packaged license file. Available texts are retained inside the archive.
- [Validation summary](summary.json): actual commands, timings, source hashes,
  package/frontend/runner hashes, installed footprint and scope.
- SVM reports: [bounded](starter.json), [full token](full-token.json),
  [lifecycle](lifecycle.json). These equal the prior recorded case results.
- [Repository checks and repeatability](checks.json).

The binary is the same 6,886,240-byte runner used in the previous workflow
experiment; packaging does not change the runtime or protocol identity. Its
dynamic dependencies are system libiconv and libSystem. The package contains
notices/provenance as well as the executable, so installation footprint exceeds
binary size. The exact installed byte count is in the summary.

Two repeated final package builds produced the same archive and pin. Deterministic
tar/gzip metadata makes fixed inputs repeatable; this does not assert identical
builds across compiler versions or machines. Earlier package/installer development
evidence is preserved in [the pilot](../2026-10-05-runner-install/README.md).

## Failure and recovery checks

The CLI rejects a missing download configuration without creating a cache,
rejects a corrupted archive before extraction/execution, installs a valid package,
and verifies an idempotent second install without rewriting the binary. `doctor`
recognizes the selected managed runner. A changed binary fails both `status`
and project tests; failed tests remove their stale passing report. Editing the
receipt to match the changed binary still fails against the frontend's embedded
pin. Restoring the original bytes/receipt permits the same starter suite again.
No installation lock or staging directory remains after handled failures.

Root unit tests additionally reject traversal, absolute paths, unexpected/missing
members, duplicates, symlinks/hardlinks, size/hash mismatches, bad gzip CRC,
nonexecutable installed files and failed smoke checks. Cancellation before
publication leaves no installed root. Busy-installer tests preserve the other
writer's lock. Selection tests verify explicit/environment precedence, PATH
fallback when uninstalled and rejection of a damaged managed cache.

## Reproduce and release limits

Rebuild the frontend, then use a new result directory:

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" bash scripts/build-cli.sh build/gosvm
python3 scripts/runner_install_verify.py \
  --output results/svm/new-runner-install \
  --archive results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz
```

The script requires the earlier matched token artifact and captured fixture
directories. It never overwrites historical results or modifies repository
program sources. Package construction is documented in
[the installation guide](../../../docs/SVM_RUNNER_PACKAGING.md).

This is an experimental local artifact, not a public, notarized or supported
distribution. No public download endpoint exists. Project license selection,
dependency/runtime notice review, supported-host/clean-machine validation and
release signing remain open. The regression uses captured-validator-compatible
fixtures; it does not add a fresh validator run under restricted RPC binding.
Milestone 1 remains open.
