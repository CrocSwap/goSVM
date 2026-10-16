# SVM runner installation and packaging

`gosvm test --svm` runs compiled programs in a pinned local SVM runner built from
the vendored, patched LiteSVM. The CLI manages two runner versions: **0.5.1**
(LiteSVM 0.8.2, selected for `fogo-v0` and the legacy profile) and **0.6.0**
(LiteSVM 0.13.0, selected for `solana-v3`). Users never compile Rust: the CLI
downloads a pinned archive and verifies it against SHA-256 pins compiled into the CLI.

**0.5.1 supersedes 0.5.0** (October 16, 2026). Both are LiteSVM 0.8.2 with the same
rent-error patch and stdio protocol; 0.5.1 is built from the committed
`benchmarks/svm-runner` source on every platform, which adds `returnData` to
simulation results and stored transaction metadata (`logMessages`,
`computeUnitsConsumed`, `returnData`, read with `getTransaction`). The qualified
0.5.0 macOS binary was built from an earlier, uncommitted source without them, so
it could not be rebuilt for Linux under the same version. 0.5.0 is not released
or downloaded any more, but its pinned packages remain accepted: an existing
0.5.0 installation is still verified and selected for `fogo-v0`/legacy when 0.5.1
is not installed, and `gosvm runner install -archive` still installs a pinned 0.5.0
package. Evidence: [runner 0.5.1](../results/svm/2026-10-16-runner-0.5.1/README.md).

## Install and select

```sh
gosvm toolchain install          # backend, then both runners (-runners=false skips)
gosvm runner install             # runners only; -version 0.5.1|0.6.0 selects one
gosvm runner install -archive gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz   # offline
gosvm runner status
```

Downloads happen only in these explicit install commands; builds and tests never
download or silently install anything. The release layout is:

| Tag | Assets |
| --- | --- |
| `runner-v0.5.1` | `gosvm-svm-runner-0.5.1-<platform>.tar.gz` |
| `runner-v0.6.0` | `gosvm-svm-runner-0.6.0-<platform>.tar.gz` |
| `v<cli version>` (e.g. `v0.1.0-alpha.1`) | `gosvm-v…-<platform>.tar.gz`, `SHA256SUMS`, `install-release.sh` |

`<platform>` is `darwin-arm64`, `linux-amd64` or `linux-arm64` (Go's GOOS-GOARCH
names, as in the cache paths). Runner URLs are
`https://github.com/CrocSwap/goSVM/releases/download/runner-v<version>/<archive>`;
`GOSVM_RELEASE_BASE_URL` replaces the `…/releases/download` base (https://,
http:// or file://) for mirrors, air-gapped caches and tests. A download from any
source passes exactly the checks of a local archive, so the URL is not a trust root.
Runner tags are immutable and shared by every CLI release that pins them; a new
runner build gets a new runner version. There is no `runner-v0.5.0` release.

| Runner | Platform | Archive bytes | SHA-256 | Binary SHA-256 |
| --- | --- | ---: | --- | --- |
| 0.5.1 | darwin-arm64 | 3,179,739 | `5f207d71…d34704` | `ca1f9f71…be617c` (built from source, reproducible) |
| 0.6.0 | darwin-arm64 | 3,436,424 | `cf6e8444…a70c23` | `550bac10…ec9dff30` (unchanged qualified binary) |
| both | linux-amd64, linux-arm64 | **pending**: built by the release workflow; pins committed afterwards | | |

Pins: `internal/runner/pins/release/<version>-<platform>.json` (downloaded).
Pins directly under `internal/runner/pins/` stay accepted for `-archive` and existing
installations but are never downloaded: the historical local packages
(`darwin-arm64.json` for 0.5.0, `0.6.0-darwin-arm64.json`) and the unpublished
release candidates of the public-install preparation
(`*-darwin-arm64-repackage-2026-10-16.json`: 0.5.0 `f39fc2ef…`, 0.6.0 `25a8d37f…`),
which hold the same binaries as the historical packages. The 0.6.0 release archive
was re-pinned when the supplementary license texts below were added; its runner
binary is unchanged. Until Linux pins are committed, a Linux CLI reports that it
pins no runner for the host and `gosvm test --svm` needs `-svm-runner` or
`GOSVM_TEST_RUNNER` (for example a runner built with `scripts/build_runner_linux.sh`).

Managed installations live under `<OS user cache>/gosvm/runners/<version>/<platform>`,
or `<GOSVM_CACHE>/runners/…` (`GOSVM_CACHE` must be absolute). Selection order:

1. Explicit `-svm-runner` for project tests or `-runner` for standalone `svm-test`.
2. `GOSVM_TEST_RUNNER`.
3. A managed installation verified against an embedded pin: `solana-v3` 0.6.0;
   `fogo-v0` and legacy 0.5.1, else 0.5.0 (and `fogo-v0` 0.6.0 only with
   `GOSVM_TEST_FEATURES`).
4. `gosvm-svm-runner` on PATH when no managed installation exists.

A damaged managed installation is an integrity error, never a silent PATH fallback.
`gosvm runner status` reports 0.5.1 and 0.6.0, and 0.5.0 only when it is installed.

## Verification and recovery

A download is written to a private temporary file, bounded by the pinned size;
a short or long download fails before hashing ("truncated or wrong file"). Then,
as for `-archive`: the bytes are copied into private staging with a size bound,
SHA-256 is checked before parsing, exactly the pinned regular members are
extracted (duplicate, missing, unexpected, linked, traversal and mismatched
members are rejected, expanded bytes are bounded, the gzip trailer is validated),
the runner's identity and stdio initialization are smoke-tested, and the
installation is published by rename. A local archive that is the pinned package
of another platform is named as such. Errors and cancellation remove staging and
the lock and never select a partial installation. Installed files are compared
with the embedded pin; the editable receipt cannot substitute new hashes. These
checks detect altered package bytes; they do not protect against an actor able to
replace the CLI or change files between verification and execution.

If an installation is damaged, move aside its exact version/platform directory
and reinstall. A process killed during installation can leave `<install-root>.lock`;
remove that exact lock only after checking that no installer is running.

## Archive contents and licensing

`bin/gosvm-svm-runner`; `LICENSE` and `NOTICE` (goSVM, Apache-2.0);
`THIRD_PARTY_NOTICES`; `README`; `Cargo.lock`; `litesvm/` (0.5.x: the vendored
LiteSVM's `GOSVM_PATCH.md`, `GOSVM_PROVENANCE.json` and `LICENSE-APACHE`; 0.6.0:
`build_modern_runner.py`, which reconstructs and patches the checksummed LiteSVM
0.13.0 crate, and the Apache-2.0 text, since that crate ships none);
`licenses/<crate>-<version>/…` (every license/notice file shipped by a crate in the
target's dependency graph); `licenses/Apache-2.0.txt`; `CRATE_LICENSES.txt` (each
crate, its declared license, source and packaged texts; generated from `cargo
metadata` and the crate sources, build dependencies included); `dependencies.json`;
`provenance.json` (build kind, source hashes, binary hash, dynamic libraries);
`manifest.json`; repackaged archives also keep `original/provenance.json`.

The macOS archives list 286 (0.5.1) and 321 (0.6.0) crates. Crates that ship no
license file and declare Apache-2.0 are covered by `licenses/Apache-2.0.txt`.
MIT crates that ship no license file get the upstream text, with its copyright
line, from [`third_party/crate-licenses/<crate>-<version>/`](../third_party/crate-licenses/README.md)
(0.5.1: `enum-iterator` 1.5.0, `five8` 0.2.1/1.0.0, `five8_const` 1.0.0,
`five8_core` 0.1.2/1.0.0, `qstring` 0.7.2, `void` 1.0.2; 0.6.0: the 1.0.0 `five8`
crates, `qstring`, `void`), marked "upstream text supplied by goSVM". No crate is
left without a text, on macOS or in the Linux graphs, and the packaging script
refuses to build an archive that would leave one. The inventory is not a legal review.

## Building runner archives (maintainers)

`scripts/package_runner_release.py --version 0.5.1|0.6.0 --output DIR` writes the
archive, `pin.json`, `dependencies.json` and `provenance.json`. Tar ordering,
metadata and gzip header are fixed; repeated runs give identical bytes.

- **Source builds** (0.5.1 everywhere, Linux 0.6.0): the script copies the runner
  crate and its path dependencies (vendored LiteSVM 0.8.2; for 0.6.0, LiteSVM 0.13.0
  reconstructed from the SHA-256-pinned crate by `scripts/build_modern_runner.py`)
  to the fixed `GOSVM_RUNNER_BUILD_ROOT/src` (default `/tmp/gosvm-runner-build`),
  because Cargo hashes the absolute path of a path dependency outside the crate's
  workspace into symbol names, and builds with `cargo build --release --locked`
  (`--offline` with cached crates) and `--remap-path-prefix` mapping the staged
  sources, the target directory and Cargo's home to `/gosvm`, `/gosvm-target` and
  `/cargo`. `RUSTFLAGS` must be unset. The provenance records no local paths.
- **macOS arm64 0.5.1:** `python3 scripts/package_runner_release.py --version 0.5.1
  --offline --output DIR` with rustc/cargo 1.98.1 (`48a229cea`, `797e8a9bc`) and the
  Apple linker recorded in the provenance (ld 27037.1, SDK 27.0). Two builds from
  different checkout paths, Cargo homes and cold target directories gave the same
  binary and archive bytes.
- **macOS arm64 0.6.0:** `--from-archive <earlier pinned package>` repackages its
  runner binary (checked against the historical pin), offline; two runs gave
  identical bytes.
- **Linux:** `bash scripts/build_runner_linux.sh build/runner-release amd64|arm64`
  runs the source build inside `rust:1.98.1-bookworm` pinned by digest, with the
  checkout at `/gosvm` and Cargo's home at `/cargo`. Crates come from `Cargo.lock`
  (checksummed by Cargo; `--locked`). The release workflow runs the same command.
- `scripts/update_runner_pins.py ARCHIVE…` derives each pin from the archive bytes
  alone and writes `internal/runner/pins/release/<version>-<platform>.json`;
  `--check` verifies committed pins against published archives.

**Reproducibility.** The macOS 0.5.1 binary is reproducible from this repository
given the same Rust toolchain and Apple linker/SDK; a different Xcode or Rust
release gives different bytes (the release archive is the pinned artifact, so a
rebuild is a check, not a requirement). Ad-hoc code signatures are part of the
reproducible bytes. The 0.6.0 macOS binary is still the qualified build of
October 9: its recorded sources match the committed ones, but it embeds that
build's Cargo home and was built with a different crate metadata hash, so it is
repackaged rather than rebuilt (a source build here gives `4080d269…`). The
historical 0.5.0 binary (`886ea293…`) was built from `src/lib.rs` `eea4eda8…`,
which is not in the repository, and embeds local paths. Linux builds of both
versions have not been executed (no working Docker daemon); CI's SBF suites run
them once their pins are committed.

Ad-hoc signing is not Developer ID signing or notarization. Archives are fetched
with curl/Go's HTTP client, which set no quarantine attribute; a browser download
of an archive does, and `-archive` installs then need `xattr -d com.apple.quarantine`.
Historical evidence: [the first local package](../results/svm/2026-10-05-runner-install-complete/README.md),
[the 0.6.0 package](../results/svm/2026-10-09-runner-0.6.0-package/README.md),
[the public-install preparation](../results/svm/2026-10-16-public-install/README.md),
[runner 0.5.1](../results/svm/2026-10-16-runner-0.5.1/README.md).
