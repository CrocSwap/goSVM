# Development and benchmark reference

Moved from the root README on October 7, 2026. These notes preserve the detailed
implementation history, toolchain setup, reproduction commands, measured
workloads, and experimental limits. Checkpoint measurements describe their
pinned snapshots; they are not universal performance or compatibility claims.
For current development status, start with [HANDOFF.md](../HANDOFF.md) and the
[milestone acceptance audit](MILESTONE2_ACCEPTANCE.md).

Commands below run from the repository root unless otherwise specified. Some
historical links point to generated artifacts that are now local-only; see the
[artifact inventory](../results/maintenance/2026-10-06-artifact-untracking/README.md)
for retention and recovery details.

- [Project setup and SDK/framework details](#start-a-go-project)
- [Cut a release (maintainers)](#cut-a-release)
- [Anchor comparison](#anchor-comparison)
- [Maintainer validation and reproduction](#reproduce)
- [Compiler architecture and manual token workload](#implementation-and-workload)
- [Language subset](#supported-subset-and-remaining-blockers)
- [Dependency-heavy protocol experiment](#dependency-heavy-protocol-experiment)

---

Returning to the project? Read [the session handoff](../HANDOFF.md) for current
status, evidence, and environment details, and [the draft product roadmap](../ROADMAP.md)
for proposed milestones and release criteria.

Phoenix's [compiler/SDK dependencies](../docs/PHOENIX_DEPENDENCIES.md) now include
verified v0 multiplication-helper linking, rejection of unknown unresolved
symbols, and SDK-2 `SetReturnData`. The preserved 631-case trading corpus and
return-byte/CPI proofs pass with separate new pins; historical baselines remain.

Roadmap implementation has started with an opt-in LiteSVM testing spike. All
existing matched bounded/token fixtures agreed with the validator in three
repetitions, with substantially lower test overhead. See
[results and compatibility limits](../results/svm/2026-10-05-litesvm/README.md),
[runner reproduction](../benchmarks/svm-runner/README.md), and the
[multi-account binding proposal](../docs/MULTI_ACCOUNT_DESIGN.md). Validator testing
remains the default; the runner is not yet a supported distribution. The
[transport comparison](../results/svm/2026-10-05-transports/README.md) selects ordered
stdio batches, now available through `gosvm test --svm` with `-svm-run` fixture
selection. See the [experimental protocol and workflow](../docs/SVM_RUNNER_PROTOCOL.md).
Runner 0.3.0 adds explicit Clock/Rent controls and restores the initial VM before
each fast fixture. [Compiled-probe and fresh-validator evidence](../results/svm/2026-10-05-controls-final/README.md)
records the checks and remaining limits.
Runner 0.4.0 adds [general multi-account fixtures](../docs/SVM_FIXTURES.md),
available through `gosvm svm-test` for a compiled ELF or `test --svm -svm-fixtures`
for projects. [Full-token validation](../results/svm/2026-10-05-general-complete/README.md)
covers shared-state scenarios, overrides, exact CU/errors, and rollback.
Runner 0.5.0 adds a documented LiteSVM rent-error precedence patch (0.5.1, the
same engine built from the committed source with `returnData`, supersedes it for
public installs; see [runner packaging](SVM_RUNNER_PACKAGING.md)). The
[lifecycle checks](../results/svm/2026-10-05-lifecycle-verified/README.md) create and
initialize token accounts, execute a Go swap, close/recreate accounts, and verify
rollback; new-account `rent_epoch` metadata differs explicitly between engines.
The [complete workflow experiment](../results/svm/2026-10-05-workflow-complete/README.md)
passed 60 measured runs: median source-edit/forced-native-test latency was 0.286 s
for the bounded starter and 0.536 s for the manually wired full token swap.
Bootstrap, caches and workload scope are recorded separately.
The [pinned local runner package](../docs/SVM_RUNNER_PACKAGING.md) installs with
`gosvm runner install -archive <package.tar.gz>` and is verified by `runner status`.
[Installation validation](../results/svm/2026-10-05-runner-install-complete/README.md)
passes bounded/token/lifecycle regressions without Cargo on the test PATH.
Public runner downloads/signing and other supported hosts remain open.
General fixture steps now support [ordered Clock/Rent controls](../docs/SVM_FIXTURES.md),
validated with real SBF syscall reads, reset isolation and rollback in the
[stateful corpus](../results/svm/2026-10-05-scenarios-complete/README.md). The
[runtime matrix and bounded milestone 1 checklist](../docs/SVM_RUNTIME_COVERAGE.md)
record the tested subset and remaining acceptance gates.
The [acceptance procedure](../docs/MILESTONE1_ACCEPTANCE.md) and
[current-host rehearsal](../results/svm/2026-10-05-m1-acceptance-final/README.md)
provide reproducible checks. Milestone 1 is closed by the user's October 5
decision to skip the additional fresh-validator and clean-host checks; those
checks are not claimed as passing. Milestone 2 language/framework work has started.

A working experimental compiler for a restricted Go subset targeting Solana SBF.
Go code performs a full-width constant-product swap, validates eight accounts,
invokes the real SPL Token program twice, signs a vault transfer with a PDA, and
updates persistent state. A lean Rust implementation passes the same tests.

**Decision: the technical proof of concept is a GO. Keep the project experimental;
do not commit to a supported toolchain yet.** Small contracts build quickly and
produce small programs with competitive compute usage. Native-Go build speed does
not persist as generated code grows. If native-Go speed across larger programs is
essential, the present LLVM pipeline has not met that requirement.

## Start a Go project

The first ergonomics slice is implemented: one CLI, a standalone typed swap
starter, generated account validation/codecs, a Go client, an experimental IDL,
and native plus actual-SBF tests.

```sh
# Released binary (after the first public release; no Go/Clang needed to install):
curl -fsSL https://github.com/CrocSwap/goSVM/releases/download/v0.1.0-alpha.1/install-release.sh | sh
# ...or from a checkout (needs Go; macOS also host Clang for external linking):
bash scripts/install.sh "$HOME/.local/bin"

export PATH="$HOME/.local/bin:$PATH"
gosvm toolchain install   # official platform-tools + pinned SVM runners, verified; no Cargo
gosvm doctor
gosvm new my-swap
cd my-swap
gosvm check
gosvm test                # native Go tests (needs Go 1.22+)
gosvm build
gosvm test --svm          # compiled program on the local SVM runner, both targets
```

Supported hosts: **macOS arm64**, **Linux x86_64** and **Linux arm64** (glibc;
the backend needs libstdc++ and zlib). The macOS path is validated end to end; the
Linux backend pins come from Anza's official archives and are qualified in CI by
reproducing the reference ELFs, and Linux runners are released after their pins
are committed ([runner packaging](SVM_RUNNER_PACKAGING.md)). `install-release.sh`
downloads `gosvm-<version>-<platform>.tar.gz` and `SHA256SUMS` from the release
(`--version`, `--dir`; `GOSVM_RELEASE_BASE_URL` selects a mirror) and installs
after checking the SHA-256. Rust libraries (`toolchain install -rust`) remain
macOS arm64 only. `gosvm test --sbf` additionally needs the Solana validator.

The source installation has been tested on macOS arm64; Linux builds the CLI
statically (`CGO_ENABLED=0`). `gosvm toolchain install` now downloads the official
platform-tools **v1.57** archive for `fogo-v0`/`solana-v3` and requires the
extracted Clang/LLD to match their content pins ([target profiles](TARGET_PROFILES.md)).
The historical description below applies to `gosvm toolchain install -target
legacy-v1.51-v3`: it downloads the official **v1.51** archive, verifies a pinned
SHA-256 digest before extraction, smoke-tests SBF compilation/linking, and retains
only Clang and LLD. It does not invoke Cargo
or install Rust. The initial upstream archive is still **448 MB**; the installed
backend is about **168 MiB**. Downloads and extraction use a temporary directory,
and the archive is removed after installation. Signed (Developer ID/notarized)
CLI releases and a smaller upstream download remain unfinished. Other hosts can
still supply `SBF_LLVM`. Archive pins for every supported host are in
`internal/toolchain/archives.go`; `GOSVM_PLATFORM_TOOLS_BASE_URL` selects a mirror.

`gosvm toolchain status` and `doctor` verify managed backend content hashes.
The default cache is the OS user cache under `gosvm/toolchains`; `GOSVM_CACHE`
can select an absolute cache directory. Backend selection is `SBF_LLVM`, then
the managed cache, then an existing Solana v1.51 installation. No build silently
downloads tools. `toolchain install -archive /path/to/official.tar.bz2` works
offline with the same pinned checksum. The official artifact and checksum source
are recorded in [the installer](../internal/toolchain/install.go) and its receipt.
Validator **3.0.15** remains a separate dependency only for `test --sbf`.

Commands find the project from subdirectories without crossing a separate Go
module. Use `gosvm new -module example.org/my/swap my-swap` for a custom import
path. Native Go test flags pass through after `--`, for example
`gosvm test -- -run TestSwap -count=1`. Project commands reject irrelevant flags.
`gosvm build -timings` and `gosvm test --svm -timings` print optional JSON phase
durations. Diagnostics also report failed attempted phases; measure external
command wall time separately to include CLI startup and teardown.

An inspectable generated project lives in [examples/typed-swap](../examples/typed-swap).
The starter's business logic uses `Pool`, `SwapArgs`, `SwapResult`, and named
errors. Its bounded arithmetic matches the original small AMM experiment; it does
not transfer tokens. `gosvm generate` emits inspectable Go with owner/writable/
size/discriminator checks and fixed little-endian field encoding. `check`, `build`,
and `test` regenerate automatically. An embedded SDK snapshot gives the project
normal offline Go tooling without a dependency on this checkout or an unpublished
module. Commit the snapshot, generated files, manifest, and IDL.

Schema 1 supports one state account/handler and flat unsigned wire fields.
The experimental [schema-2 prototype](../docs/SCHEMA2.md), selected by `gosvm new
-schema 2`, adds multiple instructions/accounts, shared canonical types, packed
array/nested-struct codecs, layout history, generated clients and account/key/alias
validation. Its [ledger example](../examples/multi-state/README.md) passes compiled
SBF checks. Optional [SDK 2](../docs/SDK2.md), selected with `new -schema 2 -sdk 2`,
adds checked classic-token references/transfers, multi-seed PDA signing and
generic CPI. Its [compiled-SBF proof](../results/compiler/2026-10-05-checked-token-lifecycle-final/README.md)
passes 117 scenarios in three runs. Generated token/PDA constraints and the
[preloaded full swap](../examples/full-swap/README.md) now pass all 108 preserved
manual swap scenarios/110 transactions in three SBF runs. A separate
[ordinary-Go service](../examples/full-swap-service/README.md) imports its actual
types, codecs and quote. The earlier generated swap-only module used 45,941 CU/21,536 ELF bytes,
versus the manual baseline's 20,329 CU/8,064 bytes; framework overhead remains
substantial. SDK-2 [rent/System/owned lifecycle operations](../docs/SDK2.md) now pass
35 scenarios/46 transactions in [three SBF runs](../results/compiler/2026-10-05-lifecycle2-supported/README.md).
Generated rent-funded initialization and authorized closing now pass the distinct
[SOL escrow lifecycle proof](../results/compiler/2026-10-05-escrow-supported/README.md):
32 scenarios/42 transactions in three SBF runs. The [creator-managed swap lifecycle](../results/compiler/2026-10-06-managed-swap-supported/README.md)
now passes 29 scenarios/36 transactions × three, including pool/vault creation,
deposits, swaps, drain/close/reuse and rollback. The expanded four-instruction
program is 68,320 bytes; its legacy wide swap uses 46,016 CU, managed swap 47,858,
create/deposits 86,742, and drain/close 61,869. The largest static frame is
3,968 bytes. [The preserved corpus/service regression](../results/compiler/2026-10-06-framework-swap-managed/README.md)
passes all 108 scenarios/110 transactions × three, with both ordinary-Go service
layout modes. [Repeated framework measurements](../results/compiler/2026-10-06-milestone2-bench-supported/README.md)
now pass 125 command samples: median edit build 0.917 seconds managed swap and
0.520 escrow; forced native plus lifecycle SBF test 0.613 and 0.504 seconds.
These are installed-tool, warm-cache local observations. The
[independent developer trial](../docs/MILESTONE2_TRIAL.md) is the only remaining
milestone-2 gate; the concrete kit and outside-checkout maintainer rehearsal pass.
Anchor compatibility, TypeScript clients and deployment tooling are later work.
SDK 2 now also exposes the real runtime [Clock accessor](../docs/SDK2.md#runtime-clock),
with [native/C/SBF evidence](../results/compiler/2026-10-05-clock2-supported/README.md)
and passing swap/service and escrow regressions. Re-pin the frontend/SDK together
when adopting it; SDK 1 remains unchanged.
The optional [compact signer helper](../docs/COMPACT_SIGNER_ADOPTION.md) now avoids
the single-group seed copy through `cpi.SingleSigner` and `InvokeSingle`.
Existing signer APIs remain available; adopt a matching complete SDK-2 snapshot.
SDK-2 compilation now also optimizes a narrow [checked packed-store loop](../docs/PACKED_STORE_OPTIMIZATION.md),
with complete-handler and native/C/SBF bounds/rollback evidence. Other loop
shapes retain their ordinary translation.
A separate [Whirlpools application snapshot](../docs/WHIRLPOOLS_133_TARGET.md) now
meets the 1.33× CU target at **1.280853× scoped Rust**, median paired successful
handler CU at O2. All 166 cases pass three times against Go and unchanged scoped/
full Rust; 29,713 SBF arithmetic cases pass three runs in both languages. This
event-omitting classic-token fixed-fee result uses bounded arithmetic and private
validation reuse; it is separate from the starter and generated swap below.
The starter README describes its exact restrictions and wire
format. `test --sbf` uses temporary local keys/genesis and reports CU, persistence,
and atomic rollback in `build/sbf-results.json`.

The initial starter ELF is **3,216 bytes**, and its valid swap uses **486 CU**.
All 14 validator cases passed. These are measurements of this generated account
adapter, not the full token-CPI program discussed below. See
[ergonomics results](../results/ergonomics/README.md) for the original timing snapshot,
and [polish validation](../results/polish/README.md) for managed installation and current workflow checks.

## Cut a release

Maintainer-only; nothing here runs automatically. CI (`.github/workflows/ci.yml`)
builds, vets and tests on linux-amd64 and macOS arm64 for pushes and pull requests,
installs the backend (cached) and, once published, the runners, runs a starter
walkthrough and rebuilds the reference ELFs. `.github/workflows/release.yml` is
manual and only creates **draft** releases. Tags: `runner-v0.5.1`, `runner-v0.6.0`
(runner archives, immutable once published) and `v<semver>` for the CLI, e.g.
`v0.1.0-alpha.1`. The CLI embeds the runner pins, so runners are released first.
0.5.1 supersedes 0.5.0, which is not released
([runner packaging](SVM_RUNNER_PACKAGING.md)).

1. **Runner drafts.** `gh workflow run release.yml -f component=runners`. It builds
   the Linux archives of both versions for amd64 and arm64 in the pinned container
   (`scripts/build_runner_linux.sh`) and attaches them to draft releases
   `runner-v0.5.1`/`runner-v0.6.0`.
2. **macOS runner archives.** Rebuild them (offline; 0.5.1 from source with Rust
   1.98.1 and the Apple linker recorded in its provenance, 0.6.0 by repackaging the
   historical package) and attach them only if they equal the committed pins:
   ```sh
   python3 scripts/package_runner_release.py --version 0.5.1 --offline --output build/rr/0.5.1
   python3 scripts/package_runner_release.py --version 0.6.0 --offline --output build/rr/0.6.0 \
     --from-archive results/svm/2026-10-09-runner-0.6.0-package/gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz
   python3 scripts/update_runner_pins.py --check build/rr/*/gosvm-svm-runner-*.tar.gz   # must be up to date
   gh release upload runner-v0.5.1 build/rr/0.5.1/gosvm-svm-runner-0.5.1-darwin-arm64.tar.gz
   gh release upload runner-v0.6.0 build/rr/0.6.0/gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz
   ```
   If the 0.5.1 rebuild differs (another Rust or Xcode release), upload the
   qualified archive kept with the [0.5.1 evidence](../results/svm/2026-10-16-runner-0.5.1/README.md)
   after the same `--check`, or re-pin and requalify a new build.
3. **Pin the Linux archives.** Download the drafts' Linux assets, optionally rebuild
   them locally with Docker and compare, then commit the pins:
   ```sh
   mkdir -p build/rr/linux
   gh release download runner-v0.5.1 -p '*linux*' -D build/rr/linux
   gh release download runner-v0.6.0 -p '*linux*' -D build/rr/linux
   bash scripts/build_runner_linux.sh build/rr/local amd64      # optional reproduction
   cmp build/rr/linux/gosvm-svm-runner-0.6.0-linux-amd64.tar.gz build/rr/local/0.6.0-linux-amd64/gosvm-svm-runner-0.6.0-linux-amd64.tar.gz
   python3 scripts/update_runner_pins.py build/rr/linux/*.tar.gz
   make test && go vet ./...
   git add internal/runner/pins/release && git commit -m "Pin Linux runner release archives"
   git push            # CI on main then installs and runs the Linux runners
   ```
4. **Publish runners:** `gh release edit runner-v0.5.1 --draft=false` and the same
   for `runner-v0.6.0`, after CI on the pinned commit is green.
5. **CLI draft:** `gh workflow run release.yml -f component=cli -f version=v0.1.0-alpha.1`.
   It refuses to build unless the committed pins equal the runner release assets
   (`update_runner_pins.py --check`), then attaches the three CLI archives,
   `SHA256SUMS` and the version-stamped `install-release.sh` to a draft prerelease.
6. **Smoke and publish:** on a clean machine,
   `curl -fsSL https://github.com/CrocSwap/goSVM/releases/download/v0.1.0-alpha.1/install-release.sh | sh`
   (draft assets need `gh release download` instead), `gosvm toolchain install`,
   `gosvm new demo && cd demo && gosvm test --svm`; then
   `gh release edit v0.1.0-alpha.1 --draft=false`.

Local dry runs need no GitHub: `scripts/package_cli_release.py --version V --platform P
--output DIR` (and `--checksums DIR`) and the runner commands above produce the same
files; serve a directory laid out as `<tag>/<asset>` and set
`GOSVM_RELEASE_BASE_URL` (http:// or file://) to install from it. Release archives
contain LICENSE, NOTICE and THIRD_PARTY_NOTICES; runner archives add LiteSVM's
license and patch notes and the crate license inventory. Platform-tools are never
redistributed: users download them from Anza's release.

## Anchor comparison

The matched comparison gives a mixed CU result: the generated bounded starter
uses **486 CU in Go, 348 in lean Rust, and 768 in Anchor**. For the full-width swap
with two real SPL Token transfers, the wide-input case measures:

| Implementation | CU | Empty-target build | Edit build (median of 3) | ELF | Retained project artifacts |
| --- | ---: | ---: | ---: | ---: | ---: |
| Go | 20,329 | 2.61 s | 0.59 s | 8,064 B | 8.0 KiB |
| Lean Rust | 20,392 | 1.92 s | 0.94 s | 12,288 B | 169.7 KiB |
| Anchor 0.32.2 | 19,431 | 155.74 s | 2.47 s | 188,768 B | 295.95 MiB |

Go uses 4.6% more CU than Anchor on that token case, while building faster and
retaining substantially fewer artifacts. The measured token Go baseline uses
manual SDK validation. A separate [generated migration](../examples/full-swap/README.md)
now retains the same corpus with its framework costs recorded above. The starter
comparison does use the generated Go framework. All three implementations pass
the same behavioral fixtures, with explicit framework error mappings.

These builds use installed tools and cached dependency sources, matching the
pinned SBF backend across implementations. Shared caches/toolchains, IDL/client
generation, and native Rust test harnesses are excluded. Clean builds are single
observations on a shared desktop; this is not a benchmark of Anchor 1.x or a
complete AMM. See [results and raw evidence](../results/anchor/README.md) and
[reproduction instructions](../benchmarks/anchor/README.md).

## Reproduce

Tested on Apple M2, macOS 26.6.2, Go **1.22.0**, Solana CLI/test-validator and
`cargo-build-sbf` **3.0.15**, Solana platform-tools **v1.51** (Clang 19.1.7-rust-dev,
Rust 1.84.1-dev). Both new workloads target **SBF v3**, with the local validator's
default features. This is not a statement about mainnet feature activation.

Requirements: Go, host Clang, Python 3.10+, and the Solana tools. Scripts expect
platform-tools at `~/.cache/solana/v1.51/platform-tools`; override with `SBF_TOOLS`.
If missing, `cargo-build-sbf --tools-version v1.51 --install-only` installs them.
Downloads are outside all timings. Changing toolchains requires checking the ABI
and linker layout.

```sh
make test            # Native properties and generated-C differential tests
go vet ./...
make verify-tokens   # Go/Rust build, Token CPI, balances, persistence, rollback
make bench-tokens    # Clean/edit/no-op timings, sizes, isolated backend check
make scale          # Generate and benchmark four growing Go/Rust workloads
make verify-scale   # Check scaling ELFs against independent Python arithmetic
make verify         # Regress the original bounded arithmetic experiment
python3 scripts/save-results.py  # Save matching results and source/tool hashes
```

`make test` uses external linking on macOS for the pinned Go 1.22 loader
workaround, including test executables that import the HTTP harness. The new
transport comparison is another standalone Go module and needs its
[focused tests](../benchmarks/svm-transport/README.md).

Verification uses an isolated local validator, deterministic keys and genesis
fixtures. It removes its ledger and stops the validator on exit. It never uses a
public cluster or wallet. On macOS the harness uses external linking and ad-hoc
signing to work around Go 1.22/macOS 26 loader incompatibility.

After building the frontend, an ordinary contract build uses no Cargo or Rust:

```sh
build/gosvm -o build/swap.so examples/tokenswap/swap.go
build/gosvm -emit-c -o build/swap.c examples/tokenswap/swap.go
```

`-llvm /path/to/llvm` or `SBF_LLVM` selects the backend. `-no-cache` forces a rebuild.
A 153-byte receipt checks source/frontend content hashes, target, backend resolved
paths/sizes/modification timestamps, and output content hash. Missing or damaged
output is rebuilt. Deliberately replacing a backend while preserving its size and
timestamp requires `-no-cache`. This is a whole-program cache, not incremental
compilation. C/object intermediates are temporary.

For local disk cleanup, `python3 scripts/clean_build.py` previews disposable
Cargo intermediates and older acceptance-test Go/toolchain caches. `make clean`
removes those caches while retaining linked binaries, source/SDK snapshots,
`results/`, the current Go cache and the latest M1 acceptance toolchain. The
next Rust build or old acceptance rerun may rebuild or reinstall its caches.

Heavy historical experiment outputs under `results/` are local artifacts rather
than tracked source. Archives, compiled tools, dumps, logs and raw execution
reports remain on disk; their checksums and derived CU/error tables are recorded
in the [artifact tracking inventory](../results/maintenance/2026-10-06-artifact-untracking/README.md).
Small fixtures, summaries, source snapshots and reproduction scripts remain in
Git. Older reports may link to local-only files; full historical reproduction
requires restoring those artifacts. This tracking cleanup does not rewrite Git
history, delete evidence or affect required vendored LiteSVM program assets.

For a complete reset, `python3 scripts/clean_build.py --deep` previews removal
of all `build/` contents. `make clean-deep` first saves and verifies a compressed,
deduplicated archive of non-cache source/artifact snapshots under a new
`results/maintenance/` directory, then removes `build/`. Its report includes
the archive inventory and hashes. All existing results and external tools stay
intact; Go/Cargo caches, duplicate installed tools and test executables are
discarded. Restore archived working snapshots with
`tar -xzf results/maintenance/<cleanup>/build-snapshots.tar.gz` from the project
root. Ordinary builds reconstruct their output; historical scripts that require
an older frontend/module path may need the archive restored first.

## Implementation and workload

```text
Go parser/type checker → subset Check → checked C → Solana Clang -O2 → LLD → SBF ELF
```

`compiler.Check` enforces the whole on-chain subset (entry signature, recursion,
borrows and every language rule) before any code generation and returns a
backend-neutral `CheckedProgram`; the C emitter applies no rules of its own.
`project.LoadSchema` exposes validated schema 2 with resolved wire layouts. These
are the interface for alternative backends; the C/LLVM output is unchanged. See
[the backend interface](BACKEND_INTERFACE.md) and, for failure semantics,
[the error contract](ERROR_CONTRACT.md).

The frontend and harness have no external Go modules. No Go runtime, GC, scheduler
or allocator is linked. A small C adapter decodes the account envelope and marshals
PDA/Token/SHA-256 syscalls. Validation, swap logic and wide arithmetic are written in Go.
This is a Go-to-C compiler, not a port of upstream Go or TinyGo. TinyGo has not
been benchmarked here.

Account order: pool, user signer, user X, vault X, vault Y, user Y, pool PDA,
classic SPL Token program. Checks cover distinct keys, pool/vault/mint configuration,
lengths, owners and flags, token initialization, user/PDA authority, balances,
overflow, slippage and counter overflow. Input transfer uses the user signature;
output transfer uses `[pool public key, bump]` under this program's ID. The pool
counter advances after both transfers. Actual vault balances are the reserves.

Pool bytes: `vaultX[32] | vaultY[32] | mintX[32] | mintY[32] | bump[1] | count[8]`.
Instruction: amount in and minimum out, two little-endian uint64s. Fees are **30
bps rounded up to an input-token unit**, retained in the vault:

```text
fee = ceil(amountIn × 30 / 10000)
net = amountIn − fee
amountOut = floor(reserveY × net / (reserveX + net))
```

The uint64 domain is supported subject to balances and final balances fitting.
Go uses a two-limb 128-bit product and normalized division adapted from `math/bits`
(see `THIRD_PARTY_NOTICES`); the independent Rust implementation uses `u128`.

This is a **preinitialized-pool swap**, not a complete AMM. Genesis fixtures preload
pool/token accounts; mint creation and initialization are not tested. There is no
liquidity, LP token, administration, oracle, generic CPI, Token-2022, multisig or
wrapped-SOL interface. Arithmetic fixtures explore synthetic balances, not a full
mint-supply history. Frozen accounts reach SPL Token for rejection, exercising a
second-CPI failure after the first transfer has executed.

## Correctness evidence

- 100,000 wide quotes plus boundaries against `math/big` and the constant-product
  invariant; another 100,000 independent multiply/divide comparisons.
- 10,000 wide generated-C/native-Go cases, plus the original 10,000 bounded
  properties and 2,000 bounded C/native cases.
- Compiler tests cover unsigned overflow, shifts, zero values, bounds, division,
  short circuiting, evaluation order, scopes, SDK account decoding/Context copies,
  unsupported constructs, and cache invalidation.
- **103 token vectors per backend**: full-width and random successes, authority,
  owner, mint and vault substitution, aliasing, missing/extra accounts, decoder
  account limit, malformed data, readonly accounts, arithmetic limits, frozen
  accounts and slippage. Exact CPI invocation/success counts are checked.
- Two committed swaps per backend; committed `[valid, invalid]` instructions; and
  a committed failure of the second CPI. Both rollback cases check exact errors
  and unchanged pool data and all four token balances.
- **40 scaling VM checks** against independent Python arithmetic at four sizes.
- Original suite: 162 simulations, three VM runtime guards, four committed swaps,
  and two rollback checks.

ELF versions and hashes are checked; reports also identify the actual SPL Token
ELF supplied by the validator. Rust uses `no_std`, zero dependencies, optimization
level 2 and LTO, with equivalent validation. This is a lean Rust baseline.

## Measurements

Median milliseconds, seven samples for token builds; five for scaling. Final samples use interleaved backends.

| Token-swap build | Go → SBF | Lean Rust → SBF | Native Go reference |
| --- | ---: | ---: | ---: |
| Project clean | 334 ms | 1062 ms | 541 ms |
| Source edit | 308 ms | 662 ms | 651 ms |
| No change | 20 ms | 200 ms | 283 ms |

| Scaling functions | Go edit | Rust edit | Native Go edit | Go / Rust ELF bytes |
| --- | ---: | ---: | ---: | ---: |
| 1 | 109 ms | 390 ms | 347 ms | 1,568 / 1,416 |
| 16 | 193 ms | 539 ms | 408 ms | 7,328 / 7,176 |
| 64 | 368 ms | 741 ms | 384 ms | 25,760 / 25,608 |
| 256 | 1166 ms | 1808 ms | 562 ms | 99,488 / 99,336 |

The token edit build is about 2.1× faster than lean Rust on this run. At 256 mixing functions, Go is about 1.6× faster than Rust but 2.1× slower than native Go. Clean/edit/no-op distributions and CPU times for every size are in the raw reports. These are measured comparisons, not proposed acceptance thresholds.

Across **68 successful token vectors per backend**, Go uses 20,233–20,343 CU
(median 20,312); Rust uses 20,029–20,491 (median 20,408). Paired Go/Rust changes
range from −0.85% to +1.02%, median −0.46%. The wide swap is 20,321 versus 20,380 CU.
Invalid paths are in the raw report, not mixed into these medians.

The two Token invocations consume 9,290 CU in either implementation. Subtracting
those inner-program counts leaves median 11,022/11,118 CU, including validation,
PDA work, CPI overhead and arithmetic. This is not pure arithmetic cost, but shows
that Token execution is not masking a large Go penalty.

Scaling uses 1/16/64/256 distinct eight-round mixing functions, all contributing
to an observable checksum; five seeds are checked per backend. Neither source
forces inlining. The largest has 3,082 Go lines and approximately 100 KB of program.
It measures code growth, not module graphs, SDK dependencies or a production
protocol. Largest-case CU is 12,558/12,543; Go adds 15 CU at each tested size.

Timings include startup/linking, with installed tools and warm OS/stdlib caches.
The frontend is prebuilt. Clean Rust deletes target/deployment output; clean
native Go forces package recompilation while retaining the stdlib cache. Comment
edits invalidate source caches without changing code. Backends are timed serially
in seeded shuffled order within each scenario. Raw reports include every sample
and child-process CPU time. Earlier exploratory results show substantial desktop
scheduling variability and are retained; this is not isolated benchmark hardware.
Native Go builds the same code with a host driver: a build-latency reference,
not a like-for-like runtime or executable-size comparison.

## Footprint and optimization

Go leaves an **8,000-byte ELF plus 153-byte receipt**. Rust's ELF is 12,192 bytes;
its target/deployment directories total about 45 KB. Both are small. Huge Rust
project-artifact savings are not demonstrated by this dependency-free baseline.

In the earlier compiler-only benchmark snapshot, the stripped frontend was about 3.4 MiB; Clang plus LLD add 167.9 MiB, suggesting
a roughly **171 MiB builder** at that point. The current project CLI also embeds
scaffolding and the local RPC test runner; its size is recorded with ergonomics results. A build with only those backend executables in an
isolated directory produces the identical ELF; macOS dynamic dependencies are OS
libraries. A distributable signed package is not built. Full installed
platform-tools occupy 1.36 GiB; Go SDK 211 MiB. The full toolchain is currently
how the two backend binaries are obtained.

Testing adds approximately 5–6 MiB for the Go harness, 58.4 MiB for the validator,
or 196.2 MiB for the CLI tree **including** that validator. Temporary ledgers are
removed. Fresh-cache frontend bootstrap time and its roughly 49 MiB Go cache are
measured separately in the raw report. Logical file bytes are reported, not APFS
allocation, peak RAM or download size; overlapping directories are not additive.

Profiling places compile work in LLVM optimization. An `-O1` experiment passed
the then-current 196 token simulations and committed/rollback tests, but raised
the wide swap from 20,321 to 29,500 CU (45% worse), so `-O2` remains. The raw
experiment is saved. The no-op cache fixes avoidable recompilation without
changing runtime code; it does not make source edits faster.

## Supported subset and remaining blockers

To see which constructs and SDK calls one program actually uses, run
`gosvm features` ([schema `gosvm-features-v1`](FEATURES_REPORT.md)); it splits
handwritten code from the generated adapter.

One file or a directory of root-level non-test Go files in one package;
`Process(solana.Context) uint64` with the pinned SDK and restricted user packages, or
legacy `Process([]byte, []byte) uint64`. The SDK adapter supports up to 16 incoming
accounts in SDK 1 and [32 in SDK 2](../docs/ACCOUNT_DECODER32.md);
legacy ABI requires one owned writable 24-byte state and a 16-byte instruction.
Only the original experiment additionally tests v0 (`SBF_ARCH=v0 make verify`).

Supported: every integer type (`uint8`/`byte`, `uint16`, `uint32`, `uint64`, `uint`,
`int8`, `int16`, `int32`/`rune`, `int64`, `int`; `int` and `uint` are 64 bits), `bool`,
byte slices, constants, local zero values, functions with unnamed or named results, function values and closures, `defer`,
indexing, `len`, integer arithmetic/conversions, comparisons, `if`,
conditional/three-clause `for`, Go 1.22 `range` over integer counts, scalar arrays,
array pointers and byte slices, unlabeled `break`/`continue`, single or multiple
`=`/`:=`, expression `switch`, variable `++`/`--`, and compound assignment
(`+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`, `^=`, `&^=`, `<<=`, `>>=` on every
integer type and addressable target, map elements included; the
[compound fixture](../examples/compound-values/program.go) is checked against
ordinary Go on the host and on SBF). Arithmetic wraps at the operand
width (two's complement for signed types); signed division truncates toward zero
(`MinInt / -1 == MinInt`); shifts take signed or unsigned counts, a negative count
aborts, and shifts by the width or more give zero (or the sign fill for a signed `>>`).
Integer types are admitted in storage, fields, arrays, pointers, constants and
conversions; `len`/`cap`/`copy` results and range keys are ordinary `int` values. The
[uint16 fixture](../examples/uint16-values/program.go) and the generated
[integer fixture](../examples/int-values/ops.go) are checked against ordinary Go on the
host and on SBF. See [integer semantics](GO_INTEGERS.md) and
[range semantics](GO_CONTROL.md#range-loops). See [return/assignment/control semantics](../docs/GO_CONTROL.md). Context is opaque in SBF: it can
be passed/copied, but not constructed, zero-initialized or accessed through fields.
Native tests can construct it with callbacks.

Also supported: named integer scalars, named value structs, nested structs,
keyed/positional literals, field reads/writes, zero values, and by-value arguments
and returns. Struct fields are integer scalars, bools, scalar arrays, other value
structs, pointers, slices or interfaces.
Struct `==`/`!=` (non-blank fields in order, stopping at the first difference),
`switch` on structs, type aliases (`type A = T`, including aliases of types in
other packages and of generic instantiations) and `panic` (a trap that logs one
contracted line) are supported; see [value types](../docs/GO_TYPES.md). Struct
conversions, embedding, `recover` and build constraints are rejected
explicitly. Wire layout is generated independently of C or Go layout.

Fixed-size scalar arrays, including named `[32]byte` key types, now support value
copies, literals, indexed reads/writes, equality, `len`/`cap` and matching-underlying
array conversions. Each array value is limited to 1,024 bytes (except package-level
tables that are only indexed; see [large tables](../docs/GO_GLOBALS.md#large-tables)). Elements may be
structs, arrays, pointers, slices, maps, strings, interfaces or function values
([value types](../docs/GO_TYPES.md), checked against ordinary Go on the host and
on SBF) (non-byte views of arrays came with
[allocation](../docs/GO_ALLOCATION.md)); schema-1 generated codecs
remain flat unsigned fields; schema 2 adds packed scalar-array codecs. See [semantics and validation](../docs/GO_ARRAYS.md).

Named, module-aware user imports now support shared types, constants, codecs and
pure functions, including transitive packages and local replacements. Resolution
is offline; imported on-chain packages must satisfy the same subset. A separate
native service can use ordinary Go facilities outside that graph. See
[shared-package semantics and validation](../docs/GO_PACKAGES.md).

Value/pointer methods and constrained pointers now support caller-owned scalar,
struct and array storage, mutable aliases, returned caller borrows, direct method
calls and method expressions. Escaping locals move to the heap; pointer-element
arrays, pointers-to-pointers, named pointer types and method values are rejected. See
[borrowing and verified semantics](../docs/GO_POINTERS.md). Increment/decrement also
supports fields, dereferences and indexed elements.

Borrowed byte views now support two/three-index slicing, reslicing within
capacity, slice len/cap and nil comparisons, caller-borrowing results and alias
updates. SDK buffers have capacity equal to their supplied lengths. See
[view semantics and SBF evidence](../docs/GO_VIEWS.md).

Bounded, invocation-scoped heap allocation supports `new`, `make`, `append`
(Go 1.22's exact capacity growth), `copy`, slice literals, slices of any admitted
element (`[]uint64`, `[]Struct`, `[][32]byte`, `[]*T`, `[][]byte`), pointer and
slice struct fields, and locals whose references escape, which move to the heap.
The budget is 32 KiB by default and up to 256 KiB with `heap_bytes` (clients then
send `RequestHeapFrame`); running out aborts. See
[allocation semantics and SBF evidence](../docs/GO_ALLOCATION.md).

`import "math/bits"` is available on-chain as a compiler intrinsic:
the 64- and 32-bit and `uint` carry, multiply and divide functions and the 64-,
32-, 16-, 8-bit and `uint` count, reverse and rotate functions, with Go's exact
results and its divide panics as aborts. See
[math/bits semantics and SBF evidence](../docs/GO_BITS.md).

`errors`, `strings`, `bytes`, `strconv` (all but floats), `encoding/binary`
(byte orders and varints), `unicode/utf8`, `unicode` (case mapping, `IsSpace`),
`math` (integer limits) and `io` (errors, interfaces, `ReadFull`,
`ReadAtLeast`, `WriteString`) are available on-chain too: curated Go 1.22.0
source embedded in the compiler (`internal/compiler/_stdlib/`, with provenance
hashes) and compiled with the program; symbols left out (floats, reflection,
the other packages' `io` parts) are reported by name. `errors.As` takes a `*T` target, and the overlay
needed [variadic functions](../docs/GO_FUNCTIONS.md#variadic-functions) and
[pointers to pointers](../docs/GO_POINTERS.md#pointers-to-pointers), which
user code can use too. See
[standard-library packages, provenance, deviations and costs](../docs/GO_STDLIB.md).
Published third-party modules (holiman/uint256, KyberNetwork/int256, the
uniswapv3-sdk-uint256 math, lukechampine.com/uint128) run on chain; see
[real libraries](../results/compiler/2026-10-16-real-libraries/README.md),
which also added elided `&T{...}` array elements, pointer conversions and
read-only pointer parameters for package-level tables, and first compiled
them from trimmed copies. They now compile unmodified with the explicit
[trimming opt-in](GO_PACKAGES.md#trimming) (`"trim"` in `gosvm.json`, or
`-trim <pattern>`): only the declarations `Process` reaches in the named
dependency packages are checked and compiled, and every dropped declaration
and initializer is reported ([results](../results/compiler/2026-10-16-trim-opt-in/README.md)).

Generics are monomorphized: generic functions, generic structs and named arrays
with methods, explicit and inferred instantiation, type-set (`~uint64 | ~uint32`),
`comparable`, `any` and method constraints. Each instantiation is checked and
emitted as its own function or struct with a deterministic name derived from its
type arguments, at most 64 per generic declaration. Generic code may allocate
(`make([]T, n)`, `append` on `[]T`, `new(T)`), and each instance gets its own
escape analysis. Interfaces are also usable as type arguments. See
[generics semantics, naming and SBF evidence](../docs/GO_GENERICS.md) and the
[mixed fixture](../examples/mixed-values/program.go), which combines signed
integers, generics and allocation and is checked against ordinary Go on the
host and on SBF.

Interface values have Go's semantics: method sets, embedded interfaces, `any`,
implicit and explicit conversions, dynamic dispatch through value and pointer
receivers, type assertions (single and comma-ok), type switches, comparisons
(an uncomparable identical dynamic type aborts), and interfaces in fields,
slices, arrays and behind pointers, including generic dynamic types. Converting
a non-pointer value boxes a copy on the heap (its Go size rounded up to 8
bytes). See
[interface semantics, boxing cost and SBF evidence](../docs/GO_INTERFACES.md)
and the [interface fixture](../examples/iface-values/program.go), checked
against ordinary Go on the host and on SBF.

Strings have Go's semantics: literals in read-only data, `len`, indexing and
slicing (shared bytes) with bounds aborts, concatenation and `+=`, bytewise
comparisons, conversions to and from `[]byte`, `[]rune` and runes (invalid
runes become U+FFFD), `range` with Go's UTF-8 decoding, `switch`, and strings in
fields, arrays, slices, pointers, interfaces and generic code (`~string`).
Concatenations and conversions allocate a documented number of heap bytes. With
strings, the predeclared `error` interface works like any interface value. See
[string semantics, allocation and SBF evidence](../docs/GO_STRINGS.md) and the
[string fixture](../examples/string-values/program.go).

Function values, closures and `defer` have Go's semantics: declared functions,
generic instances, method values and method expressions as values; literals
capturing variables by reference (captured variables move to the heap; Go 1.22
per-iteration loop variables); calls through values (compare-and-branch
dispatch over the program's targets of the signature); `defer` (including in
loops, with deferred closures changing named results); and named results. A
nil function call aborts. Recursion through function values is rejected,
resolving each call through a value to every target of its signature. See
[function value semantics, heap cost and SBF evidence](../docs/GO_FUNCTIONS.md)
and the [function-value fixture](../examples/func-values/program.go), checked
against ordinary Go on the host and on SBF. The two combine: closures capture
strings, function types take and return strings and `error`, and deferred
closures change named `string`/`error` results
([combined fixture](../examples/func-strings/program.go), checked against
ordinary Go on the host and on SBF).

Maps have Go's results for every operation except iteration: `make` (with or
without a hint), literals, `m[k]`, `v, ok := m[k]`, assignment, `++`/`--` and
`+=` on elements, `delete`, `len`, `clear`, nil maps (writes abort), keys of
every comparable admitted type (interfaces whose dynamic type is uncomparable
abort, as Go panics) and maps in fields, slices, arrays, pointers, interfaces
and generic code. `range` over a map is rejected because Go randomizes its
order; keep the keys in a slice. The table's heap use is documented exactly.
See [map semantics, heap accounting, CU and SBF evidence](../docs/GO_MAPS.md)
and the [map fixture](../examples/map-values/program.go). Maps combine with
function values: closures capture maps, maps hold function values, function
types take and return maps, and deferred calls change maps
([combined fixture](../examples/func-maps/program.go), checked against
ordinary Go on the host and on SBF).

Package-level variables and `init` functions have Go's semantics in every
package: zero values, Go's initialization order (go/types dependency order,
imported packages first in Go 1.21's package order, `init` functions in file
and source order), on every invocation before `Process` (each SBF invocation
is a fresh process). Writable variables live in a block that is the
invocation heap's first allocation; variables that never change and have
constant initializers are read-only data and cost nothing. Initialization
whose result is fully determined (computed constants, tables, maps, errors)
is evaluated at compile time, and each invocation installs the resulting
heap image instead of running it, with the same state and heap accounting
([compile-time evaluation](GO_GLOBALS.md#compile-time-evaluation); fixture
[examples/const-init](../examples/const-init/program.go)). See
[package-level variable semantics, storage, cost and SBF evidence](../docs/GO_GLOBALS.md)
and the [fixture](../examples/global-values/program.go), checked against a
fresh native process per vector on the host and on SBF.

Unsupported: standard-library imports other than those above, dot/blank imports, workspaces, `uintptr`,
floating-point and complex types, named slices, map iteration,
method values of interface types, recursion (including through dynamic calls and function values), variadic function values and literals,
range over maps/channels/functions, labeled branches, `fallthrough`, `recover`, goroutines and channels. Runtime bounds,
division, negative-shift, `make` range, out-of-memory, failed type assertion,
uncomparable interface comparison, nil interface call, nil function call, nil-map write and unhashable map key failures abort, and so does `panic` (after logging one line); there is no recovery (deferred calls do not run on a trap) or stack growth. LLVM/VM stack limits
still apply. This compiler has neither a production audit nor a comprehensive Go
conformance claim.

Before supporting the project: shared-package distribution, richer data types, a larger real
protocol, semantic fuzzing and stack diagnostics, initialization/custody review,
packaging, and explicit cluster/toolchain compatibility. To improve larger-build
speed, investigate a smaller LLVM pass pipeline or incremental compilation while
measuring CU. A different backend is a hypothesis, not a demonstrated solution.

My recommendation is to retain **experimental status**. The earlier small
contracts establish Go authoring, wide arithmetic, real token transfers, fast
builds and competitive CU. They do not justify promising broad Go coverage or
native-Go compile speed at scale.

## Dependency-heavy protocol experiment

The [protocol benchmark](../benchmarks/protocol/README.md) separately investigates
the long cold builds and large targets that motivated this project. It models
Basanos's build shape with a stateful, hashing workload, 64 live arithmetic
kernels, the ordinary Rust Solana SDK, and 12 ProgramTest integration executables.
It does not copy Basanos functionality or alter that project.

The comparison separates SBF compilation, native VM dependency compilation,
cached edits, retained disk use, deployment size and actual validator CU. Both
languages also run through the same external harness, which makes the cost of
embedding ProgramTest visible independently of the source language. Reproduce
with `make bench-protocol`; verified evidence is saved under `results/protocol/`.

## Saved evidence

Commands write fresh reports to `build/`. Saved reports in `results/`:

- `tokenswap-benchmark.json`, `tokenswap-verification.json`.
- `scaling-benchmark.json`, `scaling-verification.json`.
- `tokenswap-exploratory-benchmark.json`, `tokenswap-o1-verification.json`,
  `clang-o2-profile.txt` preserve performance exploration and variability.
- `source-manifest.json` records source hashes and tool provenance.
- `MILESTONE-1.md`, `benchmark.json`, `verification.json` preserve the original
  bounded experiment. Its fee formula differs; it is a different workload.

ABI references: [Solana program structure](https://solana.com/docs/programs/rust/program-structure)
and [C deserializer](https://github.com/solana-labs/solana/blob/master/sdk/sbf/c/inc/sol/deserialize.h),
checked against the installed SDK. The v3 linker layout follows the installed
`rustc -Z unstable-options --print target-spec-json --target sbpfv3-solana-solana`.
Target-specific Rust output directories and ELF checks guard against stale
deployment artifacts when switching targets.
