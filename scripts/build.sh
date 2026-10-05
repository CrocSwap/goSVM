#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tools="${SBF_TOOLS:-$HOME/.cache/solana/v1.51/platform-tools}"
arch="${SBF_ARCH:-v3}"
export SBF_LLVM="${SBF_LLVM:-$tools/llvm}"
mkdir -p build
# Keep generated experiments outside `go test ./...` package discovery.
printf 'module gosvm-build-artifacts\n\ngo 1.22\n' > build/go.mod
bash scripts/build-cli.sh build/gosvm
build/gosvm -arch "$arch" -o build/amm-go.so examples/amm/amm.go
PATH="$tools/rust/bin:$PATH" CARGO_TARGET_DIR="$PWD/build/rust-target" \
  cargo-build-sbf --manifest-path baselines/rust/Cargo.toml --sbf-out-dir "$PWD/build/rust-$arch-deploy" \
  --tools-version v1.51 --arch "$arch" --offline --no-rustup-override
cp "build/rust-$arch-deploy/amm_rust.so" build/amm_rust.so
