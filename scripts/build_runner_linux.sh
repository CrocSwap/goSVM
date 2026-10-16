#!/usr/bin/env bash
# Build reproducible Linux runner release archives in a pinned container.
#
#   bash scripts/build_runner_linux.sh OUTPUT_DIR [amd64|arm64] [0.5.1 0.6.0]
#
# package_runner_release.py stages the sources at a fixed path and remaps
# embedded paths, so the binary does not depend on the host checkout; the
# checkout is mounted at /gosvm and Cargo's home at /cargo; rustc/cargo come from the pinned
# image (Rust 1.98.1, the toolchain recorded for the macOS packages); crates
# come from Cargo.lock (checksummed) and LiteSVM from the vendored/pinned
# sources. Writes OUTPUT_DIR/<version>-linux-<arch>/ with the archive and its
# pin. The same command runs in .github/workflows/release.yml. Requires
# Docker; arm64 runs natively on arm64 hosts (or under emulation, slowly).
set -euo pipefail
IMAGE="rust:1.98.1-bookworm@sha256:93ce27a88655056a51dbdd8f5f2d7ddc071c7b0070fb288a37b5a285fc83971e"
output="${1:?usage: build_runner_linux.sh OUTPUT_DIR [amd64|arm64] [versions...]}"
arch="${2:-amd64}"
shift $(( $# >= 2 ? 2 : 1 ))
versions=("$@")
if [[ ${#versions[@]} -eq 0 ]]; then versions=(0.5.1 0.6.0); fi
case "$arch" in amd64|arm64) ;; *) echo "arch must be amd64 or arm64" >&2; exit 2 ;; esac
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
case "$output" in "$root"/*) rel="${output#"$root"/}" ;; *) echo "OUTPUT_DIR must be inside the checkout" >&2; exit 2 ;; esac
# A named volume keeps the crate cache between runs; it never enters the archive.
docker volume create gosvm-runner-cargo >/dev/null
for version in "${versions[@]}"; do
  docker run --rm --platform "linux/$arch" \
    -v "$root:/gosvm" -v gosvm-runner-cargo:/cargo -w /gosvm \
    -e CARGO_HOME=/cargo -e GOSVM_RUNNER_BUILD_IMAGE="$IMAGE" -e CARGO_TERM_COLOR=never \
    "$IMAGE" bash -c '
      set -euo pipefail
      command -v python3 >/dev/null || (apt-get update -qq && apt-get install -y -qq python3 >/dev/null)
      python3 scripts/package_runner_release.py --version "$0" \
        --target-dir "/gosvm/build/runner-release-target/linux-$1" --output "/gosvm/$2/$0-linux-$1"
      chown -R "$3" "/gosvm/$2/$0-linux-$1" /gosvm/build/runner-release-target /gosvm/build/modern-runtime-vendor 2>/dev/null || true
    ' "$version" "$arch" "$rel" "$(id -u):$(id -g)"
done
ls -l "$output"/*/gosvm-svm-runner-*.tar.gz
