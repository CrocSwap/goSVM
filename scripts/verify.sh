#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ "${1:-}" == tokens ]]; then
  bash scripts/build-tokens.sh
elif [[ "${1:-}" != scale && "${1:-}" != protocol ]]; then
  bash scripts/build.sh
fi
# Go 1.22's internal linker predates macOS 26. External linking plus ad-hoc
# signing avoids its LC_UUID/dyld failure; other hosts use the normal Go linker.
if [[ "$(uname -s)" == Darwin ]]; then
  go build -ldflags=-linkmode=external -o build/verify ./cmd/verify
  codesign --force --sign - build/verify
else
  go build -o build/verify ./cmd/verify
fi
build/verify "$@"
