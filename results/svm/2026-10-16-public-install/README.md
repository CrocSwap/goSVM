# Public-alpha installation preparation — October 16, 2026

Prepares a fresh machine to install goSVM and run `gosvm test --svm` from GitHub
release downloads. **Nothing was published**: no push, tag, release or workflow
run; no keypair or cluster was used. Publishing is the maintainer procedure in
[DEVELOPMENT § Cut a release](../../../docs/DEVELOPMENT.md#cut-a-release).

## Platform-tools pins (Linux added)

`platform-tools/*.json` record a streaming download of each official Linux archive
from `anza-xyz/platform-tools` (archive size and SHA-256, and the SHA-256, link
targets and ELF `DT_NEEDED` of the tools goSVM extracts). Every archive SHA-256
equals GitHub's release asset digest.

| Version | Host | Bytes | Archive SHA-256 | clang / lld SHA-256 |
| --- | --- | ---: | --- | --- |
| v1.57 | linux-amd64 | 531,380,716 | `b0f7af10…320c2b00` | `6672ea42…` / `e54c88f3…` |
| v1.57 | linux-arm64 | 528,839,404 | `f2f31c93…2320375d` | `eba6e975…` / `bc01c509…` |
| v1.51 | linux-amd64 | 542,447,828 | `0933e05d…b5407a6b` | (receipt records them) |
| v1.51 | linux-arm64 | 535,788,934 | `e281c6b3…585b760bb` | (receipt records them) |

The Linux `clang-22`/`lld` need `libz.so.1`, `libstdc++.so.6`, `libm`, `libgcc_s`
and glibc. They were not executed here; CI's reference-ELF rebuild
(`scripts/check_rebuild_reference.py`) is the linux-amd64 qualification gate.

## Runner release archives (macOS arm64)

`runner-<version>-darwin-arm64/` hold the pin and provenance of the release
archives (the archives themselves are local-only, like other results archives).
They repackage the earlier pinned binaries unchanged with
`scripts/package_runner_release.py --from-archive … --offline`; two runs gave
identical bytes, and `scripts/update_runner_pins.py --check` derives the same pins
from the archive bytes.

| Runner | Archive bytes | Archive SHA-256 | Binary SHA-256 | Crates (no license file) |
| --- | ---: | --- | --- | ---: |
| 0.5.0 | 3,177,170 | `f39fc2ef81bc299d49388d83b0b9c097d45c7453038cd2090c07a843e158d7c4` | `886ea293…` | 286 (117) |
| 0.6.0 | 3,435,809 | `25a8d37f94332e01a0fc9cadc464571ce6b7cff97ab8626c427cdfe868913060` | `550bac10…` | 321 (113) |

Crates without license files are covered by `licenses/Apache-2.0.txt` where they
declare Apache-2.0, except five MIT-only crates of 0.6.0 marked "review required"
in `CRATE_LICENSES.txt`. Linux runner pins are **pending** (no working Docker
daemon here): the release workflow builds them in the pinned container.

Source rebuild attempts (scratch, deleted): building 0.5.0 from this checkout with
the worktree path remapped to the original checkout gave a different binary
(`29a9647a…`, 6,886,272 bytes) because the committed `src/lib.rs` (`fbb57e9b…`)
is newer than the packaged source (`eea4eda8…`, adds `returnData`); 0.6.0 gave
`71599057…` because the original build used `CARGO_HOME=/private/tmp/gosvm-modern-cargo`
and a different crate metadata hash. Hence the macOS release reuses the binaries.

## Walkthrough (macOS arm64, fresh HOME)

[walkthrough.sh](walkthrough.sh) with an empty `HOME` (no OS cache, Go caches,
`~/.cache/solana`), PATH limited to system tools plus Go 1.22.0, release assets
served by `python3 -m http.server` from `build/release-staging` (laid out as
GitHub's `<tag>/<asset>`, selected with `GOSVM_RELEASE_BASE_URL`) and the real
platform-tools download from Anza. Transcript: [walkthrough-macos.txt](walkthrough-macos.txt).

`curl … install-release.sh | sh` (checksum-verified CLI) → `gosvm toolchain install`
(431 MB download, backend + both runners, 57–144 s) → `gosvm doctor` → `gosvm new
my-swap` → `check` → `test` → `build` → `test --svm` (14/14 fixtures on fogo-v0 and
on solana-v3, same CU as the recorded starter) → `runner status`. Installed
footprint: toolchain 193 MB, runners 19 MB.

Friction found and fixed: `gosvm doctor` outside a project selected the legacy
v1.51 profile and failed after the documented `toolchain install`; it now checks
fogo-v0 and solana-v3. `runner status` said "open …/receipt.json" for a missing
runner; it now says "not installed" with the install command. Linux is untested
locally.

## Integrity negatives (CLI)

Truncated download → "is 3000000 bytes; the pinned package is 3177170 (truncated or
wrong file)"; one flipped byte → "runner archive size or SHA-256 mismatch"; HTTP
404 → names the URL; a renamed foreign file via `-archive` → "matches no pinned
package" listing the accepted hashes; a pinned archive of another platform is
named as such (unit test). `install-release.sh` with a modified archive →
"SHA-256 mismatch …; not installing". Nothing was installed in any case. A
historical 0.6.0 install from the earlier local archive verifies alongside a
release-archive 0.5.0 install.

## Validation (macOS arm64)

With `GOSVM_CACHE` holding a backend imported by `toolchain install -llvm` and both
runners installed by the new download path from a `file://` release mirror:
`make test` passes (19 packages ok, 46 without tests, 0 failures; SBF suites ran
on fogo-v0 and solana-v3 with the release-installed runners); `go vet ./...`
clean; the eight example/benchmark modules pass; `scripts/verify_rebuild.py`
reproduces all four reference ELFs (`a66c5a97…`, `0f768370…`, `b419245b…`,
`54f34b65…`) and `scripts/check_rebuild_reference.py` accepts them; actionlint
1.7.1 and shellcheck report no errors for the workflows/scripts.
