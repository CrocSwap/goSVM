# Runner 0.5.1 and supplementary crate licenses — October 16, 2026

User-approved replacement of runner 0.5.0 as the managed `fogo-v0`/legacy runner,
and license texts for MIT crates that ship none. **Nothing was published**: no
push, tag, release or workflow run; no keypair or cluster was used. Raw archives
are local-only (`*.tar.gz` here is untracked); `pin.json` and `provenance.json`
are tracked. Publishing: [DEVELOPMENT § Cut a release](../../../docs/DEVELOPMENT.md#cut-a-release).

## Why 0.5.1

The qualified 0.5.0 macOS binary (`886ea293…`) was built from a
`benchmarks/svm-runner/src/lib.rs` (`eea4eda8…`) older than the committed one,
which adds `returnData` to simulation results and stores `logMessages`,
`computeUnitsConsumed` and `returnData` for `getTransaction`. A Linux 0.5.0 built
from source would have behaved differently under the same version string. 0.5.1
is the committed source with only the version bumped (`Cargo.toml`, `Cargo.lock`,
`VERSION`), built from source on every platform. Same LiteSVM 0.8.2, rent-error
patch, protocol and feature profile.

## Archives and pins (macOS arm64)

| Runner | Archive bytes | Archive SHA-256 | Binary SHA-256 | Crates (texts supplied) |
| --- | ---: | --- | --- | ---: |
| 0.5.1 | 3,179,739 | `5f207d714f40a727c88b4badbb11731a96448cf027e1d6a3f6461ec306d34704` | `ca1f9f71ac5e737178be2887d144f293082a65fc48ca9aa512bfeb8fc2be617c` (6,886,240 bytes) | 286 (8) |
| 0.6.0 | 3,436,424 | `cf6e844438e69aa9041bb4d294fbea5932f9d9a34eb8d426ea3afa4bd3a70c23` | `550bac10c7f127d6ca6fb73df6e54942ec7347f3b9a68208e592ae085e9dff30` (unchanged) | 321 (5) |

Both are now `internal/runner/pins/release/`; `scripts/update_runner_pins.py`
derived pins equal to the packager's `pin.json`. The unpublished release
candidates of the public-install preparation (0.5.0 `f39fc2ef…`, 0.6.0
`25a8d37f…`) moved to `internal/runner/pins/*-repackage-2026-10-16.json`: they stay
accepted (checked: both install with `-archive`, as does the historical 0.5.0
package `a73c0b01…`) but are not downloaded. `gosvm runner install -version 0.5.0`
now says 0.5.0 is superseded.

## Reproducibility

`package_runner_release.py --version 0.5.1 --offline` now copies the crate and the
vendored LiteSVM to a fixed `/tmp/gosvm-runner-build/src` (Cargo hashes the
absolute path of a path dependency outside the workspace into symbol names: the
first attempt, building in place with `--remap-path-prefix` only, gave different
binaries from two checkouts, `77cb452b…` vs `d12466bd…`, with identical registry
crate hashes but a different `litesvm` metadata hash) and remaps the staged
sources, target dir and Cargo home to `/gosvm`, `/gosvm-target`, `/cargo`; the
provenance records no local path. Then two builds — this worktree with
`~/.cargo` and a reused target dir, and a copy of the needed files at another path
with a different `CARGO_HOME` (registry symlinked) and a cold target dir — gave
the same binary and the same archive bytes, as did a third packaging run. The
binary contains no local path (only `/cargo/registry/…`, `/gosvm/third_party/…`
and third-party paths inside LiteSVM's embedded SPL ELFs). Requires the recorded
rustc/cargo 1.98.1 and Apple ld 27037.1 / SDK 27.0; another toolchain gives other
bytes. The 0.6.0 macOS binary is still repackaged (a source build through the
same staging gives `4080d269…`, not the qualified `550bac10…`); two 0.6.0
packaging runs were byte-identical. Linux builds were not executed (no Docker).

## Supplementary licenses

[`third_party/crate-licenses/`](../../../third_party/crate-licenses/README.md),
keyed by crate version, with upstream sources and commits:

| Crate | Copyright line |
| --- | --- |
| `five8` 0.2.1, 1.0.0; `five8_const` 1.0.0; `five8_core` 0.1.2, 1.0.0 | `Copyright (c) 2024 Kevin Heavey` |
| `qstring` 0.7.2 | `Copyright 2017 Martin Algesten` |
| `void` 1.0.2 | `Copyright (c) 2015 The rust-void Developers` |
| `enum-iterator` 1.5.0 (0.5.1 only; MIT in 1.5.0, 0BSD in 0.8/2.x) | `Copyright (c) 2023 Stephane Raux` |

No crate is "review required" in the darwin-arm64, linux-amd64 or linux-arm64
graphs of either runner (checked with `cargo metadata --filter-platform` for all
six); the packager now refuses an archive that would leave one.

## Return data ([return-data-probe.txt](return-data-probe.txt))

A scratch schema-2/SDK-2 program returning 8 bytes, driven over the stdio
protocol `gosvm test --svm` uses: 0.5.0 reports no `returnData` and rejects
`getTransaction`; 0.5.1 (fogo-v0 and legacy) and 0.6.0 (solana-v3) report
`{"data":["KgAAAAAAAAA=","base64"],"programId":…}` from `simulateTransaction` and
in `getTransaction` metadata. CU is 253 on every runner.

## Validation (macOS arm64)

With a fresh `GOSVM_CACHE` (v1.57 backend imported with `toolchain install -llvm`)
and both runners installed by `gosvm toolchain install` through the download path
from a `file://` mirror laid out as `<tag>/<asset>`:

- `make test` passes: 19 packages ok, 46 without tests, 0 failures
  (`internal/compiler` 571 s); a `-run SBF -v` rerun shows all 50 fogo-v0/solana-v3
  SBF subtests passing on the managed 0.5.1/0.6.0 runners, none skipped.
- `go vet ./...` clean; the eight CI example/benchmark modules pass.
- `gosvm test --svm` on `typed-swap`, `escrow` (`svm.json`) and `full-swap`
  (`svm.json`, `lifecycle.json`) for legacy (v1.51), fogo-v0 and solana-v3: all
  12 runs pass (264 scenarios). For legacy and fogo-v0, all 214 PASS lines,
  including CU, are identical between the historical 0.5.0 binary and 0.5.1.
- Starter (`gosvm new`; `test --svm`): 14/14 on fogo-v0 and solana-v3, CU identical
  to the public-install walkthrough. With only 0.5.0 (historical package) and the
  earlier 0.6.0 candidate installed, `runner status` verifies both, reports 0.5.1
  missing, and `test --svm` selects 0.5.0 for fogo-v0 with the same CU.
- `scripts/verify_rebuild.py` reproduces the four reference ELFs (`a66c5a97…`,
  `0f768370…`, `b419245b…`, `54f34b65…`); `check_rebuild_reference.py` accepts them.
- `shellcheck scripts/build_runner_linux.sh` clean (actionlint not available here).
