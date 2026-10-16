#!/usr/bin/env bash
# Fresh-user walkthrough on macOS: empty HOME (so empty OS cache, Go caches,
# ~/.cache/solana), only system PATH + Go, release assets from a local HTTP
# mirror standing in for GitHub; platform-tools from Anza's real release.
set -euo pipefail
W="${1:?usage: walkthrough.sh WORK_DIR (serve build/release-staging on 127.0.0.1:8765 first)}"
rm -rf "$W"; mkdir -p "$W/home"
export HOME="$W/home"
export PATH="/usr/local/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"
unset GOSVM_CACHE SBF_LLVM GOSVM_TEST_RUNNER GOPATH GOCACHE GOMODCACHE
export GOSVM_RELEASE_BASE_URL=http://127.0.0.1:8765
cd "$HOME"
step() { printf '\n$ %s\n' "$*"; local t=$SECONDS; "$@"; printf '[%ss]\n' $((SECONDS-t)); }
step sh -c 'curl -fsSL http://127.0.0.1:8765/v0.1.0-alpha.1/install-release.sh | sh'
export PATH="$HOME/.local/bin:$PATH"
step gosvm version
step gosvm toolchain install
step gosvm doctor
step gosvm new my-swap
cd my-swap
step gosvm check
step gosvm test
step gosvm build
step gosvm test --svm
step gosvm runner status
du -sh "$HOME/Library/Caches/gosvm"/* 2>/dev/null || true
