# Installation and workflow polish

Verified October 4, 2026, on macOS arm64 with Go 1.22.0 and Solana validator 3.0.15.

## Managed backend

`gosvm toolchain install` now installs the SBF backend directly, without Cargo
or a Rust compiler. It downloads the official [platform-tools v1.51 release](https://github.com/anza-xyz/platform-tools/releases/tag/v1.51),
verifies the pinned archive SHA-256 **before extraction**, retains only Clang and
LLD, runs an SBF compile/link smoke test, and publishes the installation atomically.
The pin was taken from the release's GitHub asset digest. The receipt records the
archive URL/digest plus SHA-256 hashes of both retained binaries.

The clean-cache end-to-end check used a real download, not the previously
installed Solana tools. Download, verification, extraction, and smoke test took
**76.45 seconds** in this run. The archive is **448,012,397 bytes** and is deleted
after extraction; the retained backend plus receipt is **176,074,162 bytes**
(**167.92 MiB**). Rust, headers, libraries, and other tool binaries are not retained.
This improves installed footprint and removes Cargo setup; it does not reduce
the upstream download size. A second install verified the cache in **0.11 seconds**.

Managed installation is currently enabled only for **macOS arm64**, where the
minimal package and dynamic dependencies were validated. Other hosts retain the
manual `SBF_LLVM` path. Validator installation and signed CLI releases remain
separate work. The CLI still bootstraps from Go source with host Clang on macOS.

`GOSVM_CACHE` overrides the managed cache root. Normal builds never download.
Backend precedence is explicit `SBF_LLVM`, managed cache, existing Solana cache.
`toolchain status` and `doctor` hash the installed binaries to detect damage.
Routine builds use the existing backend path/size/mtime cache behavior rather
than rehashing 168 MiB on every edit. Checksums detect corruption; the receipt is
not a security boundary against someone who can rewrite the whole local cache.
A stale installer lock after a killed process is reported explicitly.

## Everyday workflow

- `gosvm new -module example.org/protocol/swap/v2 custom-swap` produces a working
  standalone module with consistent source imports and SDK replacement.
- Project commands discover their root from subdirectories, stopping at a separate
  Go module. A check from `client/` passed with an empty `PATH`, demonstrating that
  checking needs neither Go nor LLVM subprocesses.
- `gosvm test -- -run TestConstraintsAndNoWritesOnFailure -count=1 -v` forwards
  native test flags. The integration check verified that only the requested test
  ran. The command disables ambient Go workspaces to use its own SDK snapshot.
- `go.mod` accepts quoted module/replacement paths and replacement blocks;
  conflicting or version-specific SDK overrides are rejected.
- Help works with no command and with subcommand `--help`. Unknown commands and
  irrelevant flags return clear errors instead of accidentally entering a build.
- A platform word inside a filename, such as `my_linux_helpers.go`, no longer gets
  mistaken for an actual Go platform suffix.

## Runtime and verification

The new managed backend produces an ELF **byte-for-byte identical** to the
previous starter: **3,216 bytes**, **486 CU** for a successful swap. All **14 SBF
fixtures** passed simulation and confirmed submission, including persistent state
and atomic rollback. The full native-plus-validator run took **9.20 seconds**;
a single uncached contract build in this run took **95 ms**. These are individual
workflow observations on a shared desktop, not a new build-performance benchmark.

Root `go test ./...` and `go vet ./...` pass. New installer tests exercise the
extraction allowlist, path traversal exclusion, symlink/duplicate/missing-file
rejection, cancellation, and checksum failure without partial publication.
Project tests cover custom modules, SDK replacement syntax, and nested-module
boundaries. The downloader is separate from installation logic, allowing ordinary
Go unit tests on this machine despite the older Go 1.22 HTTP-binary linker issue;
the CLI continues to use the existing external-link/ad-hoc-sign workaround.

See [verification.json](verification.json) for commands, timings, receipt, source
hashes, and artifact hash; [sbf-results.json](sbf-results.json) for per-case CU and
confirmed outcomes. Command output and unit-test/vet logs are alongside them.

Reproduce with `python3 scripts/polish_verify.py --download` for a new download
into a temporary empty cache. Without `--download`, the verifier reuses (or
installs into) the isolated `build/polish/cache`. Neither mode changes the user's
normal Solana cache or contacts a public Solana cluster. Previous benchmark
snapshots remain historical and are not overwritten.
