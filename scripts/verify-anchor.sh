#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p results/anchor
if [[ "$(uname -s)" == Darwin ]]; then
  go build -ldflags=-linkmode=external -o build/verify ./cmd/verify
  codesign --force --sign - build/verify
else
  go build -o build/verify ./cmd/verify
fi
build/verify anchor | tee results/anchor/token-verification.log
build/verify anchor-bounded | tee results/anchor/bounded-verification.log
cp build/anchor/tokenswap-verification.json results/anchor/token-verification.json
for backend in go lean-rust anchor; do
  cp "build/anchor/bounded-$backend/build/sbf-results.json" "results/anchor/bounded-$backend-verification.json"
done
