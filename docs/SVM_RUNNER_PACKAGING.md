# SVM runner installation and packaging

`gosvm test --svm` runs compiled programs in a pinned local SVM runner built from
the vendored, patched LiteSVM. Two runner versions exist: **0.5.0** (LiteSVM 0.8.2,
selected for `fogo-v0` and the legacy profile) and **0.6.0** (LiteSVM 0.13.0,
selected for `solana-v3`). Users never compile Rust: the CLI downloads a pinned
archive and verifies it against SHA-256 pins compiled into the CLI.

## Install and select

```sh
gosvm toolchain install          # backend, then both runners (-runners=false skips)
gosvm runner install             # runners only; -version 0.5.0|0.6.0 selects one
gosvm runner install -archive gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz   # offline
gosvm runner status
```

Downloads happen only in these explicit install commands; builds and tests never
download or silently install anything. The release layout is:

| Tag | Assets |
| --- | --- |
| `runner-v0.5.0` | `gosvm-svm-runner-0.5.0-<platform>.tar.gz` |
| `runner-v0.6.0` | `gosvm-svm-runner-0.6.0-<platform>.tar.gz` |
| `v<cli version>` (e.g. `v0.1.0-alpha.1`) | `gosvm-v…-<platform>.tar.gz`, `SHA256SUMS`, `install-release.sh` |

`<platform>` is `darwin-arm64`, `linux-amd64` or `linux-arm64` (Go's GOOS-GOARCH
names, as in the cache paths). Runner URLs are
`https://github.com/CrocSwap/goSVM/releases/download/runner-v<version>/<archive>`;
`GOSVM_RELEASE_BASE_URL` replaces the `…/releases/download` base (https://,
http:// or file://) for mirrors, air-gapped caches and tests. A download from any
source passes exactly the checks of a local archive, so the URL is not a trust root.
Runner tags are immutable and shared by every CLI release that pins them; a new
runner build gets a new runner version.

| Runner | Platform | Archive bytes | SHA-256 | Binary SHA-256 |
| --- | --- | ---: | --- | --- |
| 0.5.0 | darwin-arm64 | 3,177,170 | `f39fc2ef…e158d7c4` | `886ea293…be0a2bc3` (unchanged) |
| 0.6.0 | darwin-arm64 | 3,435,809 | `25a8d37f…68913060` | `550bac10…ec9dff30` (unchanged) |
| both | linux-amd64, linux-arm64 | **pending**: built by the release workflow; pins committed afterwards | | |

Pins: `internal/runner/pins/release/<version>-<platform>.json`. The macOS release
archives repackage the qualified binaries of the earlier local packages
(`internal/runner/pins/darwin-arm64.json`, `internal/runner/pins/0.6.0-darwin-arm64.json`),
adding the license files below; those earlier archives and installations made from
them remain accepted. Until Linux pins are committed, a Linux CLI reports that it
pins no runner for the host and `gosvm test --svm` needs `-svm-runner` or
`GOSVM_TEST_RUNNER` (for example a runner built with `scripts/build_runner_linux.sh`).

Managed installations live under `<OS user cache>/gosvm/runners/<version>/<platform>`,
or `<GOSVM_CACHE>/runners/…` (`GOSVM_CACHE` must be absolute). Selection order:

1. Explicit `-svm-runner` for project tests or `-runner` for standalone `svm-test`.
2. `GOSVM_TEST_RUNNER`.
3. A managed installation verified against an embedded pin.
4. `gosvm-svm-runner` on PATH when no managed installation exists.

A damaged managed installation is an integrity error, never a silent PATH fallback.

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
`THIRD_PARTY_NOTICES`; `README`; `Cargo.lock`; `litesvm/` (0.5.0: the vendored
LiteSVM's `GOSVM_PATCH.md`, `GOSVM_PROVENANCE.json` and `LICENSE-APACHE`; 0.6.0:
`build_modern_runner.py`, which reconstructs and patches the checksummed LiteSVM
0.13.0 crate, and the Apache-2.0 text, since that crate ships none);
`licenses/<crate>-<version>/…` (every license/notice file shipped by a crate in the
target's dependency graph); `licenses/Apache-2.0.txt`; `CRATE_LICENSES.txt` (each
crate, its declared license, source and packaged texts; generated from `cargo
metadata` and the crate sources, build dependencies included); `dependencies.json`;
`provenance.json` (build kind, source hashes, binary hash, dynamic libraries);
`manifest.json`; repackaged archives also keep `original/provenance.json`.
The macOS archives list 286 (0.5.0) and 321 (0.6.0) crates; five MIT crates of
0.6.0 (`five8`, `five8_const`, `five8_core`, `qstring`, `void`) ship no license text
and are marked "review required". The inventory is not a legal review.

## Building runner archives (maintainers)

`scripts/package_runner_release.py --version 0.5.0|0.6.0 --output DIR` writes the
archive, `pin.json`, `dependencies.json` and `provenance.json`. Tar ordering,
metadata and gzip header are fixed; repeated runs give identical bytes.

- **macOS arm64:** `--from-archive <earlier pinned package>` repackages its runner
  binary (checked against the historical pin). The released macOS archives were
  made this way, offline, twice with identical results.
- **Linux:** `bash scripts/build_runner_linux.sh build/runner-release amd64|arm64`
  builds from this checkout inside `rust:1.98.1-bookworm` pinned by digest (the
  Rust release recorded for the macOS binaries), with the checkout at `/gosvm` and
  Cargo's home at `/cargo`, so embedded paths do not depend on the host. Crates come
  from `Cargo.lock` (checksummed by Cargo; `--locked`), LiteSVM 0.8.2 from
  `third_party/` and LiteSVM 0.13.0 from the SHA-256-pinned crate patched by
  `scripts/build_modern_runner.py`. The release workflow runs the same command.
- `scripts/update_runner_pins.py ARCHIVE…` derives each pin from the archive bytes
  alone and writes `internal/runner/pins/release/<version>-<platform>.json`;
  `--check` verifies committed pins against published archives.

**Reproducibility limits found on October 16, 2026.** Neither existing macOS binary
can be rebuilt from this repository byte for byte: the 0.5.0 binary was built from
an earlier `benchmarks/svm-runner/src/lib.rs` (SHA-256 `eea4eda8…`) than the one
committed (`fbb57e9b…`, which adds `returnData` to simulation results — reported
over stdio too — and `logMessages`/`computeUnitsConsumed` to stored transaction
metadata; the pinned 0.5.0 binary contains no `returnData` string), and both
binaries embed the maintainer's Cargo home or checkout paths. The macOS release
therefore reuses the qualified binaries, while a Linux 0.5.0 runner built from the
committed source would report return data that macOS 0.5.0 omits, under the same
version string. **Decision needed before publishing Linux 0.5.0:** accept that
asymmetry, or cut a new runner version (e.g. 0.5.1) built from source on every
platform and requalify it. 0.6.0's committed source matches its packaged binary's
recorded source hashes, so only paths differ there.
The Linux runners have not been executed by this work (no working Docker daemon);
CI's SBF suites run them once their pins are committed.

Ad-hoc signing is not Developer ID signing or notarization. Archives are fetched
with curl/Go's HTTP client, which set no quarantine attribute; a browser download
of an archive does, and `-archive` installs then need `xattr -d com.apple.quarantine`.
Historical evidence: [the first local package](../results/svm/2026-10-05-runner-install-complete/README.md),
[the 0.6.0 package](../results/svm/2026-10-09-runner-0.6.0-package/README.md),
[the public-install preparation](../results/svm/2026-10-16-public-install/README.md).
