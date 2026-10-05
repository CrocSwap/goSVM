#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tools="${SBF_TOOLS:-$HOME/.cache/solana/v1.51/platform-tools}"
python3 scripts/protocol_generate.py
# Dependency downloads are deliberately completed outside timed builds. Keeping
# host Cargo separate lets the pinned SBF Cargo avoid host-only edition changes.
PATH="$tools/rust/bin:$PATH" "$tools/rust/bin/cargo" fetch --locked --manifest-path benchmarks/protocol/rust/Cargo.toml
cargo fetch --locked --manifest-path benchmarks/protocol/harness/Cargo.toml
