#!/usr/bin/env bash
# Build a standalone CLI, including the macOS/Go 1.22 loader workaround.
# GOSVM_VERSION stamps `gosvm version` (default dev). GOOS/GOARCH may select
# another Linux target; Linux builds are static (CGO_ENABLED=0). macOS builds
# link externally and are ad-hoc signed, so they must be built on macOS.
set -euo pipefail
cd "$(dirname "$0")/.."
output="${1:-build/gosvm}"
mkdir -p "$(dirname "$output")"
goos="${GOOS:-$(go env GOOS)}"
flags="-s -w -X main.cliVersion=${GOSVM_VERSION:-dev}"
if [[ "$goos" == darwin ]]; then
  if [[ "$(uname -s)" != Darwin ]]; then printf 'macOS CLI builds need a macOS host\n' >&2; exit 2; fi
  flags="-linkmode=external $flags"
else
  export CGO_ENABLED=0
fi
go build -trimpath -ldflags="$flags" -o "$output" ./cmd/gosvm
if [[ "$goos" == darwin ]]; then codesign --force --sign - "$output"; fi
