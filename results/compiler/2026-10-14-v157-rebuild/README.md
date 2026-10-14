# Fresh-rebuild reproducibility: fogo-v0 and solana-v3 (platform-tools v1.57)

October 14, 2026. Local only (`network_validation: false`); no runner involved.

## Question

Are goSVM ELFs byte-identical on fresh, uncached rebuilds for the two current
targets? Earlier v1.57 "identical across repetitions" results
([October 7](../2026-10-07-target-profiles-final/README.md)) reran
`gosvm test --svm` without `-no-cache`, so repeats could be cache hits. Fresh
rebuild identity was previously shown only for the legacy v1.51 profile
(`scripts/whirlpool_optimized_verify.py`, `scripts/whirlpool_snapshot.py`).

## Method

`scripts/verify_rebuild.py`, for each target × workload:

- copies the unchanged source (excluding `build/`) into two fresh
  `mkdtemp` directories whose absolute paths differ in both prefix and length
  (`.../gosvm-rebuild-a-XXXX/work/a/<name>` and
  `.../gosvm-rebuild-bb-longer-path-XXXX/work/bb-longer-path/<name>`);
- gives each side its own empty `GOCACHE` and `TMPDIR` (so Clang/LLD's
  temporary build directory differs too);
- builds with `-no-cache`:
  - full token swap (`examples/full-swap`, project build):
    `gosvm build -no-cache -target <profile> -dir <copy>` → `build/<network>/program.so`;
  - `examples/clock2` (low-level): `gosvm -no-cache -target <profile> -o <out> <copy>`;
- compares SHA-256 and scans each ELF for the workspace path, temp root,
  `TMPDIR`, repository path and host username.

The bounded starter, dependency-heavy protocol workload and Go+Bend+Rust fixture
were not run. Go+Bend+Rust needs pinned Bend producer manifests and the full Rust
producer tree; its linked Bend/Rust objects are captured bytes, so only the Go
frontend and link step would be new relative to this check.

## Command

```sh
GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off scripts/build-cli.sh build/gosvm
GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  python3 scripts/verify_rebuild.py --output results/compiler/2026-10-14-v157-rebuild
```

## Host and pins

- macOS 26.6.2 arm64, Go 1.22.0 (`go1.22.0 darwin/arm64`)
- CLI `build/gosvm` sha256 `080ad53e66fa5fd537775dfa8641b9c66af37b4531c01d39956883fe98495661`
  (commit `dfcb8fc` sources; the CLI is unchanged by this work)
- Backend: unmanaged `~/.cache/solana/v1.57/platform-tools/llvm`, LLVM 22.1.2, content
  pins verified by the CLI: clang `67ebfa73…9ee697`, ld.lld `91db7cf0…441630`
  (equal to `ModernBackendHashes` in `internal/targets/profile.go`)

## Outcome: identical, no embedded host strings

| Target | Workload | ELF bytes | SHA-256 (both sides) | Matches Oct 7 |
| --- | --- | ---: | --- | --- |
| fogo-v0 | full token swap | 69,032 | `4697a9f572a0609da6a2063d6e323e1dfa1f1f77f2fe373abffcf64078cfcfc0` | yes |
| fogo-v0 | clock2 | 5,304 | `0f768370b8f8dd080eacee738fbf87c95fcda6ddf8532246b3f36ea8ad9c3804` | yes |
| solana-v3 | full token swap | 69,352 | `2b5f21d964375b2645d6071c53f0b63af6a7dbe2cd8ee32a63f9c5976c6cba9e` | yes |
| solana-v3 | clock2 | 4,400 | `54f34b65e934efa720f9755179e03e8b0edc6abeb8039f2788a03dbeedce66e5` | yes |

The hashes also equal the `elf_sha256` values in the
[October 7 acceptance metrics](../2026-10-07-target-profiles-final/metrics.json),
(cached builds, October 7). No non-determinism was found and
no compiler change was needed: `--strip-all` removes debug/path data, and the
temporary `program.c`/`program.o` paths from `os.MkdirTemp` do not reach the image.

`metrics.json` holds per-side paths, hashes, sizes and scan results; `NN.log` are
the CLI outputs (home directory shown as `~`).

A fast regression check, `TestModernRebuildIsByteIdentical`
(`internal/compiler/rebuild_test.go`), builds `clock2` for both targets from two
different temporary paths and compares bytes; it skips without the pinned v1.57
backend.

Scope: one host package (macOS arm64). Identity across hosts or other
platform-tools packages is not established.
