#!/bin/sh
# Install a released gosvm CLI binary (no Go or Clang needed to install it).
#
#   sh install-release.sh [--version vX.Y.Z[-pre]] [--dir DIR]
#
# Downloads gosvm-<version>-<platform>.tar.gz and SHA256SUMS from the GitHub
# release (or GOSVM_RELEASE_BASE_URL: https://, http:// or file://, laid out as
# <base>/<version>/<file>), checks the archive's SHA-256 against SHA256SUMS and
# installs gosvm into DIR (default $HOME/.local/bin). The checksum file comes
# from the same release, so this detects corrupted or truncated downloads, not
# a compromised release. Release copies of this script default to their own
# version. Building from a checkout instead: bash scripts/install.sh.
set -eu
version="${GOSVM_VERSION:-__GOSVM_RELEASE_VERSION__}"
dir="$HOME/.local/bin"
while [ $# -gt 0 ]; do
  case "$1" in
    --version) version="$2"; shift 2 ;;
    --dir) dir="$2"; shift 2 ;;
    -h|--help) sed -n '2,13p' "$0" 2>/dev/null || true; exit 0 ;;
    *) printf 'unknown argument %s\n' "$1" >&2; exit 2 ;;
  esac
done
case "$version" in
  *RELEASE_VERSION__|"") printf 'choose a release: --version vX.Y.Z (see https://github.com/CrocSwap/goSVM/releases)\n' >&2; exit 2 ;;
esac
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) platform=darwin-arm64 ;;
  Linux-x86_64|Linux-amd64) platform=linux-amd64 ;;
  Linux-aarch64|Linux-arm64) platform=linux-arm64 ;;
  *) printf 'no gosvm release for %s; build from source: bash scripts/install.sh\n' "$(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac
base="${GOSVM_RELEASE_BASE_URL:-https://github.com/CrocSwap/goSVM/releases/download}"
base="${base%/}"
name="gosvm-$version-$platform"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
fetch() {
  case "$1" in
    file://*) cp "${1#file://}" "$2" ;;
    *) if command -v curl >/dev/null 2>&1; then curl -fsSL --retry 3 -o "$2" "$1"
       elif command -v wget >/dev/null 2>&1; then wget -q -O "$2" "$1"
       else printf 'need curl or wget\n' >&2; exit 1; fi ;;
  esac
}
printf 'Downloading %s/%s/%s.tar.gz\n' "$base" "$version" "$name"
fetch "$base/$version/$name.tar.gz" "$tmp/$name.tar.gz"
fetch "$base/$version/SHA256SUMS" "$tmp/SHA256SUMS"
expected="$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$tmp/SHA256SUMS")"
if command -v sha256sum >/dev/null 2>&1; then actual="$(sha256sum "$tmp/$name.tar.gz" | awk '{print $1}')"
else actual="$(shasum -a 256 "$tmp/$name.tar.gz" | awk '{print $1}')"; fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  printf 'SHA-256 mismatch for %s.tar.gz (expected %s, got %s); not installing\n' "$name" "${expected:-none}" "$actual" >&2
  exit 1
fi
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
mkdir -p "$dir"
cp "$tmp/$name/gosvm" "$dir/.gosvm.tmp.$$"
chmod 755 "$dir/.gosvm.tmp.$$"
mv "$dir/.gosvm.tmp.$$" "$dir/gosvm"
printf 'Installed %s/gosvm (%s)\n' "$dir" "$("$dir/gosvm" version)"
case ":$PATH:" in *":$dir:"*) ;; *) printf 'Add %s to PATH, e.g. export PATH="%s:$PATH"\n' "$dir" "$dir" ;; esac
printf 'Next: gosvm toolchain install && gosvm doctor\n'
