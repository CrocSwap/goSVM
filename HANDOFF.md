# goSVM session handoff

Session parked October 4, 2026 (America/New_York), at the user's request because
of weekly Codex usage resets. This document lets a new director continue from
the repository without the old chat. No experiment or goal loop is running.
The matched Anchor comparison is complete. Subsequent discussion covered faster
testing and feasible language support, and the user requested a product roadmap.
That draft is saved in [ROADMAP.md](ROADMAP.md); implementation of its milestones
has not started.

## User intent and decisions

The project tests whether restricted Go can be a practical Solana SBF authoring
language. The user's main motivation is Rust's slow builds and large build
directories. They accept a constrained Go subset, want stateful programs and CU
comparisons, and have set no hard numerical success thresholds. They have asked
for installation and authoring ergonomics resembling Anchor.

`../basanos/` motivated the build-cost investigation, but the user explicitly
does not want its precise functionality reproduced. It has tight technical
requirements. Our synthetic protocol models its dependency/test build shape;
it is not a replacement or a port. Do not change Basanos as part of this work.

Current assessment: continue the experiment. Executable Go-authored programs,
real token transfers, small artifacts, and competitive CU are demonstrated.
Broad Go support, production readiness, and native-Go build speed at increasing
source size are not demonstrated. No public deployment or supported release has
been requested or made.

## Read these first

1. [Root README](README.md): setup, subset, architecture, original measurements.
2. [Latest Anchor results](results/anchor/README.md) and
   [methodology](benchmarks/anchor/README.md): the most recent completed work.
3. [Installation polish](results/polish/README.md): current installer and workflow.
4. [Initial ergonomics](results/ergonomics/README.md): generated starter evidence.
   Its unfinished-installer notes are historical and superseded by polish.
5. [Protocol experiment](benchmarks/protocol/README.md): dependency-heavy build
   comparison, detailed limitations, and links into `results/protocol/`.

Historical reports bind their own source/tool/binary hashes. They are snapshots,
not assertions that all current source files still have those hashes. The root
README's older 8,000-byte/20,321-CU token result and latest 8,064-byte/20,329-CU
result are different wire adapters, not a measurement contradiction.

## What is implemented

The pipeline is Go parser/type checker → checked C → Solana Clang/LLD → SBF.
There is no Go runtime, GC, allocator, or scheduler; this is neither upstream Go
SBF support nor TinyGo. Runtime account decoding and syscall marshaling are C;
validation, arithmetic, and application logic are Go.

| Area | Location and status |
| --- | --- |
| Compiler | `internal/compiler/`: subset checks, C generation, ABI adapters, linker scripts, cache, tests |
| Native SDK | `solana/`: account context and native testing surface |
| CLI | `cmd/gosvm/`: new, generate/check, build, test, doctor, toolchain commands |
| Project generation | `internal/project/`: embedded templates and SDK snapshot, typed codec/check/client generation, experimental IDL |
| Tool installation | `internal/toolchain/`: pinned download, SHA verification, minimal backend, receipts |
| SBF fixture runner | `internal/sbftest/`: simulation, committed state and rollback against a local validator |
| Comparative harness | `cmd/verify/`: bounded, token, scaling, protocol, and Anchor modes |
| Programs | `examples/`, `baselines/`, `benchmarks/`: Go programs and matched Rust implementations |
| Evidence and reproduction | `results/`, `scripts/`, `Makefile` |

The compiler supports named unsigned types, nested value structs, multiple files
in one on-chain package, unsigned arithmetic/control flow, and a narrow SDK.
Read the README for exact limits. No general user imports, arrays, pointers,
signed arithmetic, allocation, interfaces, or ordinary Go standard library.

The project generator supports **one handler and one state account**, with flat
unsigned wire fields. `examples/typed-swap/` is a standalone generated module
with local SDK snapshot, native tests, client, and validator fixtures. It performs
bounded swap arithmetic and state mutation, not token transfers. The full token
program uses manual SDK account validation, eight accounts, full uint64-domain
arithmetic, two classic SPL Token CPIs, and PDA signing.

Managed backend installation is verified on macOS arm64 only. It downloads the
official 448 MB platform-tools archive, then keeps roughly 168 MiB of Clang/LLD.
No Cargo or Rust installation is needed for ordinary Go contract builds. The
CLI is still installed from source using Go and host Clang. Signed releases,
smaller downloads, other host verification, and validator distribution are open.

## Latest measured results

Pinned Anchor 0.32.2 and platform-tools v1.51 keep the backend comparable. These
are not measurements of Anchor 1.x. Anchor uses ordinary typed Account parsing,
constraints, serialization, and default instruction-name logging, with a narrow
entry guard to match exact input/account counts. It is not a tuned zero-copy
implementation. Token-2022 and unrelated Anchor SPL features are disabled.

| Workload | Go CU | Lean Rust CU | Anchor CU |
| --- | ---: | ---: | ---: |
| Generated bounded starter | 486 | 348 | 768 |
| Full token swap, wide input | 20,329 | 20,392 | 19,431 |

Go uses 36.7% less CU than Anchor on the starter but 4.6% more on the full token
swap. All three token versions spend 9,290 CU inside SPL Token. One possible
follow-up is CPI marshaling: Go and lean Rust pass all eight account infos to
each transfer; Anchor SPL passes the three required infos. This is an observed
source difference, **not a proven attribution** of the CU gap.

| Full token build | Go | Lean Rust | Anchor |
| --- | ---: | ---: | ---: |
| Empty target, one observation | 2.61 s | 1.92 s | 155.74 s |
| Comment edit, median of three | 0.59 s | 0.94 s | 2.47 s |
| ELF bytes | 8,064 | 12,288 | 188,768 |
| Retained project artifacts | 8.0 KiB | 169.7 KiB | 295.95 MiB |

Timings exclude downloads, installed tools/shared caches, CLI bootstrap, Go
project regeneration, IDL/TS generation, and native VM harness compilation.
The shared M2 desktop had variable load; do not promise these ratios universally.
The first small-program timing phase overlapped the tail of initial verification.

The earlier dependency-heavy protocol experiment reproduced the motivating
shape: Rust SDK SBF cold build 6m52s, native ProgramTest cold compilation 35m35s,
and 4.70 GiB retained host target after edits. Go SBF cold build was 2.19s.
These are different test workflows; Rust can use the same external Go validator
driver and avoid compiling ProgramTest. Vendored OpenSSL and repeated linking
contribute materially to the host cost. The optimized Go lifecycle was 68,702 CU
versus Rust's 67,295. See that experiment's methodology before interpreting it.

## Rent follow up

The user also asked whether smaller ELFs reduce SOL rent. Yes, for a new program
with correspondingly smaller allocation. The read-only mainnet RPC query from
this session returned these minimum balances (lamports):

| Account data bytes | Purpose | Minimum balance |
| --- | --- | ---: |
| 36 | Loader-v3 Program account, common to all | 833,120 |
| 8,109 | Go ELF plus 45-byte ProgramData header | 41,843,960 |
| 12,333 | Lean Rust ELF plus header | 63,301,880 |
| 188,813 | Anchor ELF plus header | 959,820,280 |

Totals: Go 0.04267708 SOL, lean Rust 0.064135 SOL, Anchor 0.96065340 SOL.
These are **dated estimates**, not deployments: exact ELF allocation, two program
accounts, no extra upgrade capacity, fees or temporary buffers. Requery
`getMinimumBalanceForRentExemption` before presenting current prices. Rent is a
held deposit, recoverable when an authorized close is possible. Pool/token
account sizes and deposits do not shrink merely because the program is smaller.
Sources: [deployment](https://solana.com/docs/programs/deploying),
[loader sizes](https://docs.rs/crate/solana-loader-v3-interface/9.0.0/source/src/state.rs).

## Validation at the checkpoint

- Root `go test ./...` and `go vet ./...` passed after the final harness changes.
- Latest Anchor comparison: 315 token simulations, six committed swaps, six
  rollback checks; 42 bounded fixtures simulated and submitted with persistent
  state checks. Exact Anchor error mappings and CPI counts are checked. Successful
  token results compare full pool/token account bytes against independent math.
- Original token regression rerun: 206 simulations, four committed swaps, four
  rollback checks; original CU results unchanged.
- `scripts/anchor_report.py` asserted that all six runtime ELF hashes match the
  measured build artifacts. Benchmark source hashes matched after restoration
  of temporary comment edits.
- No remaining failure or partially running benchmark. Two temporary Anchor
  bring-up targets were removed; measured targets remain under ignored `build/`.

## Resume and reproduction

Canonical checkout on this host: `/Users/colkitt/sith/toys/crypto/goSVM`.
The chat's default shell directory can be elsewhere; set the workdir explicitly.
Use Go 1.22.0, Solana CLI/validator/cargo-build-sbf 3.0.15, platform-tools v1.51
(SBF Rust 1.84.1-dev and LLVM 19), SBF v3. Tools normally reside at
`~/.cache/solana/v1.51/platform-tools`; benchmark scripts accept `SBF_TOOLS`.
Ordinary Go CLI backend selection also supports `SBF_LLVM` and the managed cache.

Start with cheap checks, not the multi-minute benchmark:

```sh
go test ./...
go vet ./...
(cd examples/typed-swap && GOWORK=off go test ./...)
bash scripts/build-cli.sh build/gosvm
build/gosvm doctor
```

On this macOS version, Go 1.22 executables using HTTP need external linking and
ad-hoc signing. `scripts/build-cli.sh` and verification scripts handle this.
For a manually built verifier: `go build -ldflags=-linkmode=external -o
build/verify ./cmd/verify`, then `codesign --force --sign - build/verify`.

To reproduce Anchor, follow `benchmarks/anchor/README.md`: prepare wire adapters,
fetch locked crates using the **pinned** Cargo, run `scripts/anchor_bench.py`,
`scripts/verify-anchor.sh`, and `scripts/anchor_report.py`. Host Cargo and pinned
Cargo use different registry caches here. Locks contain compatibility pins for
the older SBF Rust; removing them can break offline builds/MSRV compatibility.
The scripts overwrite their own results directory. Preserve this checkpoint or
save a new experiment directory before refreshing historical measurements.

Run `make verify-tokens` for a fresh original token regression. Protocol and
scaling reproduction is documented in the README and Makefile. Validator tests
use isolated local fixtures, never a real wallet or public cluster. Root tests
do not discover the nested starter module; run its tests separately.

## Product planning after the checkpoint

The user asked for a roadmap to a competitive end-user product. [ROADMAP.md](ROADMAP.md)
now proposes five milestones: fast SVM testing, useful multi-account private
alpha, accessible public alpha, dependable beta, and supported release. It records
the language scope discussion, release evidence, competitive benchmarks, and
remaining product decisions. It is a draft, not a committed schedule or an active
implementation goal.

The recommended next slice is a lightweight prebuilt SVM runner compatibility
spike and a design for generated multi-account bindings. Current native tests are
fast, but full-validator integration still pays startup/RPC/confirmation costs;
faster SVM execution has not yet been demonstrated. Candidate engines require
checking against our SBFv3 artifacts, real token CPIs, and rollback behavior.

Packages, arrays, methods, and constrained pointers are proposed product
foundations. Bounded allocation and selected interfaces/standard-library
functionality are later options, not fundamentally prohibited by SVM. Full Go
runtime compatibility is not the goal. See the roadmap for detailed gates.

Do not reinterpret the mixed CU results as Go beating Rust or Anchor in general.
The demonstrated advantage is primarily the narrow build/dependency workflow.

## Repository persistence

Before the parking step, all project files were untracked in an empty Git
repository. Checkpoint `6762db8` saved the handoff, source, tests, and evidence.
The roadmap is a subsequent documentation change; use `git log` and `git status`
to inspect current history. No remote is configured and nothing has been pushed.
Local ignored `build/` outputs and
external compiler/registry caches are disposable and are not part of the commit.
The committed source, lockfiles, tests, and reports suffice to rebuild them after
installing the documented prerequisites. Preserve or back up this checkout if
moving to another machine; a fresh clone from a remote does not exist yet.
