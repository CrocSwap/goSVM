# goSVM

Returning to the project? Read [the session handoff](HANDOFF.md) for current
status, evidence, environment details, and suggested next work.

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
are recorded in [the installer](internal/toolchain/install.go) and its receipt.
Validator **3.0.15** remains a separate dependency only for `test --sbf`.

Commands find the project from subdirectories without crossing a separate Go
module. Use `gosvm new -module example.org/my/swap my-swap` for a custom import
path. Native Go test flags pass through after `--`, for example
`gosvm test -- -run TestSwap -count=1`. Project commands reject irrelevant flags.

An inspectable generated project lives in [examples/typed-swap](examples/typed-swap).
The starter's business logic uses `Pool`, `SwapArgs`, `SwapResult`, and named
errors. Its bounded arithmetic matches the original small AMM experiment; it does
not transfer tokens. `gosvm generate` emits inspectable Go with owner/writable/
size/discriminator checks and fixed little-endian field encoding. `check`, `build`,
and `test` regenerate automatically. An embedded SDK snapshot gives the project
normal offline Go tooling without a dependency on this checkout or an unpublished
module. Commit the snapshot, generated files, manifest, and IDL.

The binding format currently supports **one state account and one handler**, with
flat unsigned wire fields. The compiler also supports nested value structs and
multiple files in one package. Full Anchor compatibility, TypeScript clients,
user packages, general account constraints, initialization, and deployment tooling
remain future work. The starter README describes its exact restrictions and wire
format. `test --sbf` uses temporary local keys/genesis and reports CU, persistence,
and atomic rollback in `build/sbf-results.json`.

The initial starter ELF is **3,216 bytes**, and its valid swap uses **486 CU**.
All 14 validator cases passed. These are measurements of this generated account
adapter, not the full token-CPI program discussed below. See
[ergonomics results](results/ergonomics/README.md) for the original timing snapshot,
and [polish validation](results/polish/README.md) for managed installation and current workflow checks.

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
retaining substantially fewer artifacts. The token Go program uses manual SDK
validation; generated multi-account bindings are still future work. The starter
comparison does use the generated Go framework. All three implementations pass
the same behavioral fixtures, with explicit framework error mappings.

These builds use installed tools and cached dependency sources, matching the
pinned SBF backend across implementations. Shared caches/toolchains, IDL/client
generation, and native Rust test harnesses are excluded. Clean builds are single
observations on a shared desktop; this is not a benchmark of Anchor 1.x or a
complete AMM. See [results and raw evidence](results/anchor/README.md) and
[reproduction instructions](benchmarks/anchor/README.md).

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
`Process(solana.Context) uint64` with only `gosvm/solana` imported, or
legacy `Process([]byte, []byte) uint64`. The SDK adapter supports up to 16 accounts;
legacy ABI requires one owned writable 24-byte state and a 16-byte instruction.
Only the original experiment additionally tests v0 (`SBF_ARCH=v0 make verify`).

Supported: `uint8`/`byte`, `uint32`, `uint64`, `bool`, byte slices, constants, local
zero values, direct functions with at most one result, indexing, `len`, unsigned
arithmetic/conversions, comparisons, `if`, conditional/three-clause `for`, unlabeled
`break`, single `=`/`:=`, and variable `++`/`--`. Context is opaque in SBF: it can
be passed/copied, but not constructed, zero-initialized or accessed through fields.
Native tests can construct it with callbacks.

Also supported: named unsigned scalars, named value structs, nested structs,
keyed/positional literals, field reads/writes, zero values, and by-value arguments
and returns. Struct fields are unsigned scalars, bools, or other value structs.
Struct comparisons/conversions, embedding, aliases, and build constraints are
rejected explicitly. Wire layout is generated independently of C or Go layout.

Unsupported: other imports, arrays, pointers, signed variables
and arithmetic, globals, `init`, allocation, append/slicing, interfaces, maps,
strings, generics, closures/function values, recursion, multiple results/assignment,
`range`, `continue`, `switch`, `defer`, goroutines and channels. Runtime bounds and
division failures abort; there is no recovery or stack growth. LLVM/VM stack limits
still apply. This compiler has neither a production audit nor a comprehensive Go
conformance claim.

Before supporting the project: reusable packages, richer data types, a larger real
protocol, semantic fuzzing and stack diagnostics, initialization/custody review,
packaging, and explicit cluster/toolchain compatibility. To improve larger-build
speed, investigate a smaller LLVM pass pipeline or incremental compilation while
measuring CU. A different backend is a hypothesis, not a demonstrated solution.

My recommendation is to retain **experimental status**. The earlier small
contracts establish Go authoring, wide arithmetic, real token transfers, fast
builds and competitive CU. They do not justify promising broad Go coverage or
native-Go compile speed at scale.

## Dependency-heavy protocol experiment

The [protocol benchmark](benchmarks/protocol/README.md) separately investigates
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
