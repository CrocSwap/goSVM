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
Runner 0.5.0 adds a documented LiteSVM rent-error precedence patch. The
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
bash scripts/install.sh "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$PATH"
gosvm toolchain install   # macOS arm64; verified download, no Cargo
gosvm doctor
gosvm new my-swap
cd my-swap
gosvm check
go test ./...
gosvm build
gosvm test --sbf
```

This is a **source installation**, tested on macOS arm64. Go and host Clang
bootstrap the CLI. `gosvm toolchain install` downloads the official platform-tools
**v1.51** archive, verifies a pinned SHA-256 digest before extraction, smoke-tests
SBF compilation/linking, and retains only Clang and LLD. It does not invoke Cargo
or install Rust. The initial upstream archive is still **448 MB**; the installed
backend is about **168 MiB**. Downloads and extraction use a temporary directory,
and the archive is removed after installation. Signed CLI releases and a smaller
upstream download remain unfinished. Other hosts can still supply `SBF_LLVM`.

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
Go parser/type checker → checked C → Solana Clang -O2 → LLD → SBF ELF
```

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

One file or a directory of root-level non-test Go files in one package;
`Process(solana.Context) uint64` with the pinned SDK and restricted user packages, or
legacy `Process([]byte, []byte) uint64`. The SDK adapter supports up to 16 incoming
accounts in SDK 1 and [32 in SDK 2](../docs/ACCOUNT_DECODER32.md);
legacy ABI requires one owned writable 24-byte state and a 16-byte instruction.
Only the original experiment additionally tests v0 (`SBF_ARCH=v0 make verify`).

Supported: `uint8`/`byte`, `uint32`, `uint64`, `bool`, byte slices, constants, local
zero values, direct functions with unnamed results, indexing, `len`, unsigned
arithmetic/conversions, comparisons, `if`, conditional/three-clause `for`, unlabeled
`break`/`continue`, single or multiple `=`/`:=`, expression `switch`, and variable
`++`/`--`. See [return/assignment/control semantics](../docs/GO_CONTROL.md). Context is opaque in SBF: it can
be passed/copied, but not constructed, zero-initialized or accessed through fields.
Native tests can construct it with callbacks.

Also supported: named unsigned scalars, named value structs, nested structs,
keyed/positional literals, field reads/writes, zero values, and by-value arguments
and returns. Struct fields are unsigned scalars, bools, scalar arrays, or other value structs.
Struct comparisons/conversions, embedding, aliases, and build constraints are
rejected explicitly. Wire layout is generated independently of C or Go layout.

Fixed-size scalar arrays, including named `[32]byte` key types, now support value
copies, literals, indexed reads/writes, equality, `len`/`cap` and matching-underlying
array conversions. Each array value is limited to 1,024 bytes. Nested arrays,
struct elements and non-byte views remain unsupported; schema-1 generated codecs
remain flat unsigned fields; schema 2 adds packed scalar-array codecs. See [semantics and validation](../docs/GO_ARRAYS.md).

Named, module-aware user imports now support shared types, constants, codecs and
pure functions, including transitive packages and local replacements. Resolution
is offline; imported on-chain packages must satisfy the same subset. A separate
native service can use ordinary Go facilities outside that graph. See
[shared-package semantics and validation](../docs/GO_PACKAGES.md).

Value/pointer methods and constrained pointers now support caller-owned scalar,
struct and array storage, mutable aliases, returned caller borrows, direct method
calls and method expressions. Local/block escapes, pointer fields/elements,
pointers-to-pointers, named pointer types and method values are rejected. See
[borrowing and verified semantics](../docs/GO_POINTERS.md). Increment/decrement also
supports fields, dereferences and indexed elements.

Borrowed byte views now support two/three-index slicing, reslicing within
capacity, slice len/cap and nil comparisons, caller-borrowing results and alias
updates. SDK buffers have capacity equal to their supplied lengths. Local/block
escapes are rejected; no allocation or append is provided. See
[view semantics and SBF evidence](../docs/GO_VIEWS.md).

Unsupported: on-chain standard-library/dot/blank imports, workspaces, signed variables
and arithmetic, globals, `init`, allocation, append, non-byte/named slices, slice fields, interfaces, maps,
strings, generics, closures/function values, recursion, named results, variadics,
`range`, type switches, labeled branches, `fallthrough`, `defer`, goroutines and channels. Runtime bounds and
division failures abort; there is no recovery or stack growth. LLVM/VM stack limits
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
