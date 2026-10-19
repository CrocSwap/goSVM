# v0.1.0-alpha.4 release — October 19, 2026

Published experimental prerelease: [v0.1.0-alpha.4](https://github.com/CrocSwap/goSVM/releases/tag/v0.1.0-alpha.4),
from commit `939b215500e0219d95b140b1987e0b8363af6244` (merge of #25).
The public tag resolves to that commit. The documentation update recording this
release follows the tag and does not change its source.

[Release workflow](https://github.com/CrocSwap/goSVM/actions/runs/39303274102)
passed: macOS arm64, Linux amd64 and Linux arm64 CLI archives, stamped installer,
and SHA256SUMS. [Source CI](https://github.com/CrocSwap/goSVM/actions/runs/39282416757)
passed on Linux amd64 and macOS arm64. Runner pins remain 0.5.1 (Fogo/legacy)
and 0.6.0 (Solana); their existing public assets were checked by the release job.

## Downloaded-asset checks

- All installer/archive SHA-256 values match SHA256SUMS. The manifest's own hash
  and each archive's binary hash are recorded in [compact metrics](metrics.json).
- Each archive contains exactly five regular files: `gosvm`, `README`, `LICENSE`,
  `NOTICE`, `THIRD_PARTY_NOTICES`; the binary is executable (0755).
- macOS binary passes `codesign --verify --strict`; Linux archives contain ELF64
  binaries for the declared amd64/arm64 architectures.
- Actual macOS release installer and binary ran in a fresh HOME and empty managed
  cache. Draft assets were served through a local file mirror; the backend and
  runners downloaded from their normal public sources. `doctor` and
  `runner status` verify platform-tools v1.57 and both runners.
- Generated schema-1 starter: native tests and all 14 SBF fixtures pass on each
  of fogo-v0 and solana-v3. Generated schema-2/SDK-2 ledger: native tests and four
  SBF scenarios (five steps) pass on each target, including failure cases.
- Released CLI rebuilds full token swap and clock2 twice, at different absolute
  paths and with separate empty Go caches, using `-no-cache`. All four ELFs are
  identical, contain none of the checked host paths/username strings, and match
  [the combined integration reference](../../compiler/2026-10-19-hyperlane-integration/rebuild/metrics.json).
- After publication, public installer and checksum URLs were fetched with curl.
  With no version or base-URL override, installation succeeds and produces the
  same binary that passed the smoke tests.

Host: macOS 26.6.2 arm64, Go 1.22.0, Python 3.14.6. Native tests used the documented
`GOFLAGS=-ldflags=-linkmode=external` workaround. Doctor still recommends Go 1.23+
on macOS 26; the optional Rust library producer was not installed. Linux archives
were not executed locally; source CI executes on Linux amd64, and arm64 was
cross-built and inspected.

These are local-runtime release checks. No public-network transaction or new
SDK public-network qualification was performed. The bounded starter, schema-2
ledger and full token-swap rebuild are separate workloads; this smoke does not
qualify a dependency-heavy protocol or a complete Hyperlane port. Broader source
validation is recorded in [the integration evidence](../../compiler/2026-10-19-hyperlane-integration/README.md).

## Upgrade

The release changes SDK-2 frozen snapshots. Keep compiler and complete SDK
snapshot generations matched. Create a fresh scaffold and migrate the existing
program/schema, preserving application source and configuration; no automatic
in-place SDK refresh command is provided. Rebuild programs: ELF hashes and CU
profiles can change. The low-level 64-account decoder remains opt-in; schema-2
account declaration limits are unchanged.

## Reproduce

Run on a supported host with Go installed (Go 1.23+ on macOS 26), in a fresh
scratch directory. Use a new HOME/cache if checking first-install behavior.

```sh
curl -fsSL https://github.com/CrocSwap/goSVM/releases/download/v0.1.0-alpha.4/install-release.sh | sh
export PATH="$HOME/.local/bin:$PATH"
gosvm version
gosvm toolchain install
gosvm doctor
gosvm runner status
gosvm new starter
(cd starter && gosvm check && gosvm test --svm)
gosvm new -schema 2 -sdk 2 ledger
(cd ledger && gosvm check && gosvm test --svm)
```

From a checkout at the release tag, using a new output directory:

```sh
python3 scripts/verify_rebuild.py --cli "$HOME/.local/bin/gosvm" --output build/alpha4-rebuild
python3 scripts/check_rebuild_reference.py build/alpha4-rebuild/metrics.json
```

Archives, generated projects, cache contents, raw logs and rebuild reports remain
local-only. This directory retains summaries, checksums and compact metrics.
