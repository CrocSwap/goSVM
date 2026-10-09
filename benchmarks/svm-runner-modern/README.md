# Modern local SVM runner

This separate experimental runner accepts modern Solana SBPFv3 images. It keeps
the historical `benchmarks/svm-runner` and its result packages unchanged.
Cargo.lock pins LiteSVM 0.13.0, Agave feature-set 4.0.0 and SBPF 0.14.4. The wire
protocol and byte-exact transaction/account/return-data checks are unchanged.

```sh
python3 scripts/build_modern_runner.py
build/modern-runtime-target/release/gosvm-svm-runner --version
GOSVM_TEST_RUNNER="$PWD/build/modern-runtime-target/release/gosvm-svm-runner" \
  build/gosvm test -dir examples/full-swap -target solana-v3 --svm \
  -svm-fixtures examples/full-swap/testdata/lifecycle.json \
  -- -ldflags=-linkmode=external
```

Rust/Cargo is needed to bootstrap this local test engine; compiling applications
still uses GoSVM and the Clang/LLD backend. No signed modern runner package or
managed download is published here. `scripts/package_modern_runner.py --output DIR
[--binary existing-build]` produces a deterministic local package and a reviewable
pin; the frontend embeds that pin (`internal/runner/pins/0.6.0-darwin-arm64.json`)
and `gosvm runner install -archive` accepts only the pinned bytes. A rebuild from
another checkout path or toolchain yields a different binary and needs a new pin. `CARGO_TARGET_DIR` can choose another build
location. `--offline` uses the pinned registry archive and already cached dependencies;
`--archive <litesvm-0.13.0.crate>` selects an existing archive explicitly.

The build script reconstructs the LiteSVM crate into ignored `build/` from archive
SHA-256 `55763dae5b5f992c34eb39c49333f90dd6d5b851f878429ed2b08667b329baee`.
Original `src/lib.rs` must match
`edf9e06f7fca37b50b3bbd0d7ee22f1463db65a9deb3dd81628b3c0c018ccc4b`.
It applies one guarded rent-check change: only validate post-execution rent after
instructions succeed. This preserves an existing failed-CPI error and keeps
successful-execution rent validation, fees and rollback. The unchanged lifecycle
fixtures exercise both kinds of failure. Registry caches are never patched, and
crate binaries are not added to Git. Prepared source changes cause bootstrap
refusal. The upstream crate is Apache-2.0; its original notices are retained in
the reconstructed package.

After preparation, run runner unit checks with:

```sh
CARGO_TARGET_DIR=build/modern-runtime-target cargo test --release --locked \
  --manifest-path benchmarks/svm-runner-modern/Cargo.toml
```

Its default all-enabled local feature set is a test configuration, not an
observation of Solana or Fogo. Modern v3 tests require this runner identity;
historical tests require the old runner. For v0 with this engine, explicitly
supply a compatible `GOSVM_TEST_FEATURES` file. See [target profiles](../../docs/TARGET_PROFILES.md).
