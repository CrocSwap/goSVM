# SVM transport comparison

This standalone Go module benchmarks HTTP, stdio, and a C ABI dynamic library
around the same pinned Rust runner core. It is intentionally separate from the
root CLI/module; ordinary project builds and fast tests do not link this library
or require cgo.

From the repository root, prepare the six matched Anchor ELFs, then run:

```sh
python3 scripts/svm_transport.py --output results/svm/new-transport-experiment --samples 7
```

The command refuses existing evidence directories, builds the Rust runner/library
and Go driver, captures four complete fixture traces, checks the captures against
the immutable earlier validator snapshot, and replays each trace through all six
transport/batch combinations in seeded shuffled order. It uses the exact token
image and feature list from the earlier experiment. No fresh validator is run.

Initialization and replay are timed separately. Replay includes serialization
and complete response checking but excludes Go fixture construction/signing and
all shutdowns. Embedded-library loading happens once before timing. Batch size
changes grouping, not call/transaction order. JSON object order is ignored;
numbers retain exact precision, and account data/log strings match exactly.

The experiment requires host Rust, Go, Clang/cgo, and POSIX dynamic loading. Only
macOS arm64 has been executed. The C ABI uses Rust-owned opaque handles and
response strings; callers serialize access and free each exactly once. No Go
pointers are retained by Rust. This ABI is a benchmark surface, not supported SDK.

Focused tests are separate from root tests:

```sh
(cd benchmarks/svm-transport && GOWORK=off go test -ldflags=-linkmode=external ./...)
cargo test --release --locked --manifest-path benchmarks/svm-runner/Cargo.toml \
  --target-dir build/svm-transport-target
```

External linking works around the pinned Go 1.22/macOS loader issue. Linux can use
ordinary linking but has not been verified. See
[the measurements](../../results/svm/2026-10-05-transports/README.md) and
[the protocol decision](../../docs/SVM_RUNNER_PROTOCOL.md).
