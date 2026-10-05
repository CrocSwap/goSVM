#!/usr/bin/env bash
# Build a standalone CLI, including the macOS/Go 1.22 loader workaround.
set -euo pipefail
cd "$(dirname "$0")/.."
output="${1:-build/gosvm}"
mkdir -p "$(dirname "$output")"
flags='-s -w'
if [[ "$(uname -s)" == Darwin ]]; then flags='-linkmode=external -s -w'; fi
go build -trimpath -ldflags="$flags" -o "$output" ./cmd/gosvm
if [[ "$(uname -s)" == Darwin ]]; then codesign --force --sign - "$output"; fi
