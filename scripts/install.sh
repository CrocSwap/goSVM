#!/usr/bin/env bash
# Local source bootstrap. The installed executable includes its SDK and templates.
set -euo pipefail
if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
  printf 'Usage: bash scripts/install.sh [destination-directory]\nDefault: $HOME/.local/bin\nThen run gosvm toolchain install and gosvm doctor (macOS arm64, Linux x86_64/arm64).\nFor a released binary without Go/Clang: sh scripts/install-release.sh --version vX.Y.Z\n'
  exit 0
fi
if [[ $# -gt 1 ]]; then printf 'Expected at most one destination directory\n' >&2; exit 2; fi
root="$(cd "$(dirname "$0")/.." && pwd)"
destination="${1:-$HOME/.local/bin}"
mkdir -p "$destination"
destination="$(cd "$destination" && pwd)"
cd "$root"
staging="$(mktemp "$destination/.gosvm-install-XXXXXX")"
trap 'rm -f "$staging"' EXIT
bash scripts/build-cli.sh "$staging"
chmod +x "$staging"
mv "$staging" "$destination/gosvm"
printf 'Installed %s/gosvm\nAdd %s to PATH, then run gosvm toolchain install and gosvm doctor.\n' "$destination" "$destination"
