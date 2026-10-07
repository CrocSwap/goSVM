# Experimental runner installation

The macOS arm64 runner now has a pinned local package and an offline managed
installer. Projects share this binary without compiling Rust or adding Cargo
dependencies. Public downloads, notarization, other hosts and redistribution
review remain open. The [installation evidence](../results/svm/2026-10-05-runner-install-complete/README.md)
records successful compiled-SBF regressions and integrity failures.

## Install and select

With the current source-built CLI, from this checkout:

```sh
# A workspace cache is useful for isolated experiments; otherwise use the OS cache.
export GOSVM_CACHE="$PWD/build/dev-tool-cache"
build/gosvm runner install -archive \
  results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz
build/gosvm runner status
build/gosvm test --svm -dir examples/typed-swap
```

The archive is 3,165,750 bytes; its expected SHA-256 is
`a73c0b01a4c6f6655b305567b0e7c294f53e4e3b0082f3825c8299334be4ee0f`.
The frontend embeds this pin and every extracted member's size and digest.
Supplying a checksum next to an arbitrary archive cannot change that trust root.
This checksum is an experimental artifact pin, not a publisher signature.

Managed installations live under
`<OS user cache>/gosvm/runners/0.5.0/darwin-arm64`, or
`<GOSVM_CACHE>/runners/0.5.0/darwin-arm64`. `GOSVM_CACHE` must be absolute.
This is separate from project `build/` outputs and the managed LLVM backend.

Runner selection order is:

1. Explicit `-svm-runner` for project tests or `-runner` for standalone `svm-test`.
2. `GOSVM_TEST_RUNNER`.
3. A managed installation verified against the frontend's embedded package pin.
4. `gosvm-svm-runner` on PATH when no managed installation exists.

Explicit and PATH runners retain the exact protocol/version check. A damaged
managed installation causes an integrity error rather than a silent PATH
fallback. An explicit runner is an intentional override for source builds and
experiments. Every result continues to record the selected runner's actual hash.
`doctor` reports the optional selected runner; `runner status` verifies the
managed package specifically. Validator testing remains the default.

Installation and ordinary tests do not download tools or invoke Cargo. With no
installed package, `runner install` without `-archive` explains that no public
download is configured. A verified existing installation is idempotent.

## Verification and recovery

Installation copies the input into private staging with a fixed size bound,
checks SHA-256 before parsing, then extracts exactly the pinned regular members.
It rejects duplicate, missing, unexpected, linked, traversal and mismatched
members, with bounds on expanded bytes. It validates the gzip trailer, executable
identity and stdio VM initialization before publishing by rename. Errors and
cancellation remove staging and the installer's lock, and never select a partial
installation. Concurrent installers cannot take each other's lock.

Installed files are compared with the embedded pin, including provenance and
license inventory; the editable receipt cannot substitute new trusted hashes.
These checks detect altered package bytes. They do not protect against an actor
able to replace the CLI or change files between verification and execution.

If an installation is damaged, inspect and move aside its exact version/platform
directory, then reinstall the pinned archive. The installer does not overwrite
damaged or incompatible installations automatically. A process forcibly killed
during installation can leave `<install-root>.lock`; remove that exact lock only
after checking that no installer is running. CLI/runner upgrades should use new
versioned roots, keeping older artifacts for rollback with a compatible frontend.

## Maintainer package recipe

```sh
python3 scripts/package_runner.py --output build/runner-packages/new-package
```

The recipe uses host Cargo/Rust, the locked dependency graph and cached crate
sources in offline mode. It builds only the runner executable, checks its identity
and existing macOS ad-hoc signature, and packages the binary, Cargo lock,
source/toolchain provenance, LiteSVM patch provenance, third-party notices and
available dependency license files. The crate inventory is filtered to the
macOS arm64 dependency graph. It includes build dependencies and does not imply
that every listed crate contributes linked code.

Archive ordering, metadata, timestamps and gzip filename are fixed. Two repeated
build/package runs produced identical archive bytes. This proves repeatability
for these local inputs; different Rust/linker versions or source inputs may
produce different binaries and pins. The recipe emits `pin.json` for maintainer
review and never updates the frontend's checked-in pin or publishes an artifact.
Only an explicitly reviewed package pin belongs in a rebuilt frontend. Consumers
do not need this recipe to install the recorded package.

The local binary depends on system libiconv and libSystem, with no external Rust
library path. Actual minimum-OS compatibility and fresh-machine Gatekeeper
behavior still need testing. Ad-hoc signing is not Developer ID signing or
notarization. The Cargo inventory lists 286 packages; 118 have no packaged license
file, including goSVM's currently unlicensed runner. Cargo license declarations
and available texts are recorded, but project license selection, complete runtime
notices and redistribution review are release gates.

Before a public distribution, establish release ownership/repository, signing
and notarization policy, complete the notice review, add a pinned download source
and supported-host CI, and verify install/upgrade/recovery on clean machines.
No public release was made by this work.
