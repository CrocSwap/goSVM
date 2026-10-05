#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tools="${SBF_TOOLS:-$HOME/.cache/solana/v1.51/platform-tools}"
export SBF_LLVM="${SBF_LLVM:-$tools/llvm}"
mkdir -p build
printf 'module gosvm-build-artifacts\n\ngo 1.22\n' > build/go.mod
bash scripts/build-cli.sh build/gosvm
build/gosvm -arch v3 -o build/tokenswap-go.so examples/tokenswap/swap.go
PATH="$tools/rust/bin:$PATH" CARGO_TARGET_DIR="$PWD/build/tokenswap-rust-target" \
  cargo-build-sbf --manifest-path baselines/tokenswap-rust/Cargo.toml \
  --sbf-out-dir "$PWD/build/tokenswap-rust-v3" --tools-version v1.51 --arch v3 \
  --offline --no-rustup-override
cp build/tokenswap-rust-v3/tokenswap_rust.so build/tokenswap_rust.so
