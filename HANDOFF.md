# goSVM session handoff

Updated October 6, 2026 (America/New_York). The original session was parked
October 4 at the user's request because of weekly Codex usage resets. A new
director has started the roadmap with the user's authorization. The active user
performance objective is the 1.33× Whirlpools CU target, now achieved by the
[1.280853× owner-side snapshot](docs/WHIRLPOOLS_133_TARGET.md). Independent
developer acceptance remains the sole milestone-2 gate; see the
[completion audit](docs/MILESTONE2_ACCEPTANCE.md).
No scheduled task is implied.

October 6: the user requested [artifact untracking](results/maintenance/2026-10-06-artifact-untracking/README.md).
Generated archives/binaries, dumps, logs, large fixtures and raw execution
reports remain unchanged locally, with checksums in the new inventory. Compact
CU/error observations are retained alongside source, small fixtures and existing
summaries. Old report links may require restoring local artifacts; historical
Git blobs are preserved. The session's read-only `.git` requires applying the
prepared cleanup bundle before this checkout's tracking changes take effect.

October 6: the [Phoenix dependency proof](docs/PHOENIX_DEPENDENCIES.md) now passes
freestanding v0 `__multi3` linking, rejection of unresolved non-syscall imports,
and SDK-2 `SetReturnData` with a copying native callback. The 631-case matched
IOC/FOK/free-funds trading corpus passes Go v0/v3 and scoped/full Rust × three.
V3 CU stays unchanged (1.231522× scoped Rust); the separate v0 control measures
1.896918×. The setter's 20-byte order-ID and 16-case boundary/CPI/rollback probe
pass exact simulation/submission return bytes and identity on both targets ×
three. A separately pinned runner exposes return-data metadata; old packages
remain frozen. Existing SDK-2 examples were explicitly refreshed for the API.
Token, escrow, managed/generated swap and optimized Whirlpools regressions pass.
Unsigned CPI-length lowering avoids another v0 helper; it changes Whirlpools CU
by −3..+10 and its median to 1.281201×, with an 83,480-byte ELF and 2,368-byte
maximum frame. All previous optimization snapshots are retained. The linked
report includes complete frontend/SDK and runner pins for the benchmark owner;
application placement implementation remains independent work.

October 6: Phoenix's extra-account reproduction identified the SDK-2 decoder's
16-account cap. The [decoder extension](docs/ACCOUNT_DECODER32.md) raises incoming
SDK-2 capacity to 32 while preserving SDK 1 at 16 and the separate outgoing
CPI/schema limits. Native boundary/alias tests and token/escrow/managed/generated
swap regressions pass; the optimized Whirlpools 166-case × three comparison has
identical CU and retains the 1.280853× ratio. The generated 17-account failure now
reaches its handler's count guard (6000), rather than decoder 1001. Benchmark
owners need a new complete frontend/SDK pin; previous evidence stays frozen.
Phoenix's real 16/17-account reproduction and all 430 standard cases now pass
Go/scoped/upstream Rust × three. Standard-case CU is unchanged, retaining the
1.370116× ratio. The linked report contains the complete frozen frontend archive,
file/CLI pins and extraction/rebuild checks.

October 6 [build-cache cleanup](results/maintenance/2026-10-06-build-cleanup/README.md)
reduced `build/` from about 14 GiB to 5.27 GiB. Regenerable Cargo intermediates
and older acceptance Go/toolchain caches were removed; source/SDK snapshots,
program binaries, current Go cache, latest M1 toolchain and all result evidence
remain. The optimized handler rebuilds to its exact measured ELF and runner
0.5.0 still runs. `python3 scripts/clean_build.py` previews future cleanup;
`make clean` applies it. Old acceptance reruns may reconstruct their caches.

The subsequent user-directed [deep cleanup](results/maintenance/2026-10-06-deep-build-cleanup/README.md)
removed the entire regenerated 6.18-GiB `build/` directory. Non-cache working
snapshots now occupy about 320 MiB in a verified, deduplicated archive; all
historical results retain their hashes. External tools and sibling projects
remain untouched. `make clean-deep` repeats this policy with a new report/archive;
`python3 scripts/clean_build.py --deep` previews it. Current-source builds recreate
their outputs. Restore the archive before historical reruns that rely on an
older pinned frontend or module path under `build/`. The earlier cleanup above
describes the conservative first pass, not the current workspace footprint.

The matched Anchor comparison is complete. The first LiteSVM compatibility spike
also passed three matched bounded/token repetitions. Read the
[new results](results/svm/2026-10-05-litesvm/README.md),
[runner methodology](benchmarks/svm-runner/README.md), and
[draft multi-account design](docs/MULTI_ACCOUNT_DESIGN.md). Milestone 1 was closed
October 5 by the user's explicit decision to skip the final fresh-validator and
clean-host checks; neither is claimed as passing. Milestone 2 language foundations now include
scalar arrays, shared-package imports, multiple results/control flow, constrained
pointers/methods and bounded byte views. A [transport comparison](results/svm/2026-10-05-transports/README.md) now
selects stdio batches, implemented by `gosvm test --svm` with fixture selection.
Read the [protocol/workflow](docs/SVM_RUNNER_PROTOCOL.md). Multi-account schema
prototype now passes native/SBF tests; [SDK 2](docs/SDK2.md) now adds checked
token references, multi-seed PDA signing and generic CPI, with 117 SBF scenarios
passing in three runs. Generated token/PDA constraints and the preloaded
[full swap migration](examples/full-swap/README.md) now pass the preserved
108-scenario/110-transaction SBF corpus in three runs; an independent normal-Go
consumer uses its actual types/codecs/math. The earlier swap-only generated module used 45,941 CU and a 21,536-byte ELF;
the expanded module now uses 46,016 CU for the same wide swap and a 68,320-byte ELF,
including its new lifecycle instructions. The manual
20,329-CU/8,064-byte baseline remains separate. The
[lifecycle SDK proof](results/compiler/2026-10-05-lifecycle2-supported/README.md)
now passes 35 scenarios/46 transactions in three runs for current rent, signed/PDA
System creation, funding, owned resizing/closing/reuse and rollback. Generated
init/authorized-close policy now passes the [escrow proof](results/compiler/2026-10-05-escrow-supported/README.md):
32 scenarios/42 transactions in three actual-SBF runs, including claim/cancel,
PDA reuse, current rent, failed CPI and atomic rollback. The [creator-managed swap lifecycle](results/compiler/2026-10-06-managed-swap-supported/README.md)
now also passes 29 scenarios/36 transactions in three SBF runs: pool/vault
creation, deposits, swaps, drain/close/reuse and failures/rollback. The
[preserved swap/service regression](results/compiler/2026-10-06-framework-swap-managed/README.md)
passes 108 scenarios/110 transactions × three against the same ELF, with native
handler tests and independent service support for both layouts. Comparative
measurements now pass the [125-sample benchmark](results/compiler/2026-10-06-milestone2-bench-supported/README.md).
The [independent developer trial](docs/MILESTONE2_TRIAL.md) is the sole remaining
milestone-2 gate; the packaged kit and outside-checkout maintainer rehearsal pass.
No completed independent feedback exists yet. [Token regression](results/compiler/2026-10-05-checked-token-lifecycle-final/README.md)
and the [full swap/service regression](results/compiler/2026-10-05-framework-swap-lifecycle-final/README.md)
pass with the updated SDK; SDK-1 and the rebuilt manual token baseline are preserved.
Read [schema 2](docs/SCHEMA2.md) and the
[Whirlpools agent handoff](docs/WHIRLPOOLS_HANDOFF.md). The sections below
retain the original checkpoint's measurements and environment history.

## Latest Clock/Whirlpools coordination

The benchmark agent's verified fixed-fee core has progressed independently;
see [the updated coordination handoff](docs/WHIRLPOOLS_HANDOFF.md). Runtime Clock
was its immediate SDK dependency and is now implemented as `solana.Clock(c,
output)` with an exact 40-byte canonical encoding. Signed timestamps cross as
raw two's-complement words. [Evidence](results/compiler/2026-10-05-clock2-supported/README.md)
passes 1,010 native/C vectors and 12 SBF scenarios/15 transactions × three runs.
SDK 1 stays frozen. Existing SDK-2 full-swap/escrow snapshots have been explicitly
updated with the matching current API. The [full swap/service regression](results/compiler/2026-10-05-framework-swap-clock2/README.md)
and [escrow regression](results/compiler/2026-10-05-escrow-clock2/README.md) pass their
full corpora in three runs and reproduce their prior ELF hashes. The port must create and pin its own
matching frontend/SDK without overwriting old benchmark evidence. Checked token vault creation/closing is now implemented and the full
creator-managed lifecycle passes the proof above. The expanded program uses
86,742 CU for create/deposits, 47,858 for managed swap and 61,869 for drain/close,
with a 3,968-byte maximum static frame. SDK-helper changes require a new complete
frontend/SDK pin; the Clock API hash alone does not identify helper revisions.

The sibling benchmark has now reduced its fixed-fee handler's default-O2 median
paired CU ratio from 2.07 to 1.81 using application-only bounded/wide division,
inverse-tick and fee-growth changes. Its current scoped Go ELF is 84,448 bytes;
all 166 scenarios still pass three times against unchanged scoped/full Rust.
These remain event-omitting handler measurements, separate from the preserved
2.44 core and the generated constant-product swap. Its seed-copy reproduction
led to the [compact signer investigation](docs/SINGLE_SIGNER_INVESTIGATION.md):
an isolated 530-byte single-group builder preserves encoding and reduces a small
real System-transfer probe from 6,357 to 4,019 CU, with native checks and three
SBF repetitions of guards/rollback. A subsequent separate benchmark-derived SDK
passes all 166 handler cases three times and reduces the paired ratio to 1.74,
with an 84,192-byte ELF and 2,418 CU saved on every successful case. The exact
benchmark-tested helper is now optional in canonical SDK 2; see
[adoption and current regressions](docs/COMPACT_SIGNER_ADOPTION.md). Existing
benchmark pins/source and its default handler remain untouched. The two local
SDK-2 application snapshots were explicitly refreshed with matching helpers.
Independent developer acceptance remains the only milestone-2 closure gate.

The user subsequently set **1.33× Rust** as the optimistic Whirlpools handler CU
target, with **1.5×** as an intermediate checkpoint from the current 1.737883×.
Measure median paired successful-case ratios on the matched default-O2,
event-omitting classic-token handler corpus. This is an optimization objective,
not an achieved result or an additional milestone-2 gate. Next candidates are
validated tick-view reuse, packed tick access/update and remaining swap-loop
arithmetic, with compiler work driven by isolated reproductions. See
[the scoped target and measurement rules](ROADMAP.md#performance-and-competitive-evaluation).

The owner-side [checked packed-store optimization](docs/PACKED_STORE_OPTIMIZATION.md)
now reduces the unchanged compact handler from 1.737883× to **1.702520×** at O2.
All 166 cases pass three runs; all 103 successes improve (median 746 CU),
23 failures improve and 40 are unchanged. ELF is 84,208 bytes (+16), largest
static frame 1,472. The canonical compiler reproduces the isolated write-only
variant exactly. Packed-read and comparison-parameter alternatives were tested
separately and not adopted. Direct 422-case SBF encoding/abort/rollback tests,
12,000 native/C vectors with fallback checks, root tests/vet and all four
application regressions pass. SDK/API contents and benchmark pins/source remain
unchanged; adopting this requires a newly recorded frontend identity.

The subsequent [application optimization](docs/WHIRLPOOLS_133_TARGET.md) now
achieves **1.280853×**, beating the specified 1.33× median target at default O2.
Bounded `MulDiv`, a small-operand multiplication path and a private core after
the handler's existing tick validation account for the change. Public `Apply`
still validates independent callers. Go, unchanged scoped Rust and full upstream
Rust pass all 166 scenarios × three; 29,713 expanded SBF arithmetic cases pass
three times in Go and unchanged Rust. All successes improve from 1.702520×
(median 14,416 CU / 21.91%); 29 failures improve and 34 are unchanged, with no
regressions. ELF is 83,424 bytes, largest static frame 1,472. Individual successful
ratios range from 1.226617× to 1.617088×. This is a separately packaged owner-side
application snapshot, with an extracted-source rebuild reproducing the measured
ELF; sibling source/default pins remain unchanged. Events, adaptive fees and
dynamic arrays are outside this scoped result. No milestone-2 gate was waived.

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
| Runner installation | `internal/runner/`: embedded local-package pins, offline install/status, managed-cache integrity |
| SBF fixture runner | `internal/sbftest/`: simulation, committed state and rollback against a local validator |
| Comparative harness | `cmd/verify/`: bounded, token, scaling, protocol, and Anchor modes |
| Programs | `examples/`, `baselines/`, `benchmarks/`: Go programs and matched Rust implementations |
| Evidence and reproduction | `results/`, `scripts/`, `Makefile` |

The compiler supports named unsigned types, bounded scalar arrays, nested value structs, multiple files
in one on-chain package, unsigned arithmetic/control flow, and a narrow SDK.
Read the README for exact limits. Named module imports and constrained pointers/methods
now work within the subset; no signed arithmetic, allocation, interfaces, or ordinary Go standard library.

Schema 1 supports one handler/state account with flat unsigned wire fields.
Schema 2 now supports a [multi-instruction/account prototype](docs/SCHEMA2.md)
with packed arrays/nested structs, canonical shared types, clients, version history
and privilege/owner/identity/relation/alias validation. `examples/typed-swap/` is a standalone generated module
with local SDK snapshot, native tests, client, and validator fixtures. It performs
bounded swap arithmetic and state mutation, not token transfers. The manual full-token
baseline uses explicit SDK account validation, eight accounts, full
uint64-domain arithmetic, two classic SPL Token CPIs, and PDA signing. The
generated full swap now uses shared model/math, typed token references and
generated account/PDA relationships with the same matched wire format. Its
preloaded migration remains a preserved baseline; separate managed instructions
now supply pool/vault initialization, deposits, drain and closing with a new
177-byte layout and recorded creator authority.

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
make test  # macOS Go 1.22 tests importing HTTP need external linking
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

The October 5 runner spike and transport comparison agree on current
SBFv3/token-CPI/rollback fixtures and reduce integration overhead. The experimental
`gosvm test --svm -svm-runner /path/to/gosvm-svm-runner` path uses stdio batches;
`-svm-run <regex>` selects fixtures. It seeds the observed local-validator 3.0.15
feature profile, not mainnet. Reports are in `build/svm-results.json`.
The CLI now requires runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch); earlier binaries are rejected.
See [earlier runner validation](results/svm/2026-10-05-runner-02/README.md) for fresh
validator agreement, fixture selection, and repeated transport measurements.
Runner 0.3.0 supplies typed Clock/Rent controls, a reusable checkpoint, reset, and
blockhash expiry. `-svm-sysvars <JSON file>` controls fast-suite initialization;
the initial VM is restored before every fixture, including fees/history/statuses.
The [new control validation](results/svm/2026-10-05-controls-final/README.md)
includes a real SBF syscall probe, three repetitions, full uint64 report precision,
and 14 fresh-validator matches. A separate token regression preserves CPI/rollback
agreement. See `benchmarks/svm-controls/README.md` for reproduction.
Runner 0.4.0 adds atomic ordinary-account reset overrides and general named-account,
signer, multi-program ELF fixtures with ordered shared-state steps. Read
[the format](docs/SVM_FIXTURES.md) and
[latest validation](results/svm/2026-10-05-general-complete/README.md). Standalone
`gosvm svm-test -elf ... -fixtures ... -runner ...` runs an existing artifact;
project `test --svm -svm-fixtures ...` retains native tests/generation/build.
The small example is `examples/typed-swap/testdata/svm.json`. Default `sbf.json`
and full-validator starter testing remain unchanged. General fixture execution
currently uses LiteSVM; the fresh token validator comparison uses the independent
benchmark harness. Three runs of 108 scenarios/110 transactions passed, including
all 105 Go token CU/error matches, real CPIs, failed-CPI/transaction rollback,
overrides and reset isolation. New results bind their own hashes.
The [lifecycle follow-up](results/svm/2026-10-05-lifecycle-verified/README.md)
adds native System aliases, explicit fresh-blockhash steps, and successful
simulation-state overrides. Six scenarios/26 transactions create and initialize
mints/token accounts, run a Go swap, drain/close/recreate, and check ten failure
paths and rollback against a validator oracle captured earlier in this session
and three patched-runner repetitions. Final fresh-validator rerun was blocked
when the environment switched to restricted mode (loopback bind denied); the
captured validator used this same 26-transaction corpus and exact ELF/token hashes.
The Go pool itself remains genesis seeded. Runner 0.5.0 uses one provenance-bound
LiteSVM source patch: preserve an existing instruction error before rent checks.
`rent_epoch` is explicitly different (new accounts: LiteSVM 0, validator uint64
maximum); both engines retain exact metadata checks in separate fixtures.
The [complete workflow experiment](results/svm/2026-10-05-workflow-complete/README.md)
now records 60 measured runs, cache/rebuild assertions, setup and separate
project/shared footprint. Source edits with forced native execution take median
0.286 s bounded/0.536 s manual full token on this host; provisional local median
targets are 0.5 s/1.0 s under its installed-tool/warm-cache conditions. Optional
`gosvm build -timings` and `gosvm test --svm -timings` emit phase JSON, including
partial `passed:false` records on workflow failures. Root tests/vet and nested
starter tests pass. The earlier workflow pilot is preserved. Reproduce with
`python3 scripts/svm_workflow.py --output results/svm/new-workflow --samples 5`.
The script builds an isolated frontend and restores staged program/test sources.
The [pinned local package/installer](docs/SVM_RUNNER_PACKAGING.md) is now validated
on this macOS arm64 host. `gosvm runner install -archive <pinned.tar.gz>` and
`runner status` use an embedded archive/member pin, staged extraction, version
and stdio smoke checks, and atomic publication. Selection is explicit flag,
environment, verified managed cache, then PATH; damaged managed installs fail.
The [final installation evidence](results/svm/2026-10-05-runner-install-complete/README.md)
contains the actual 3,165,750-byte archive, pin, provenance, dependency inventory,
bounded/full-token/lifecycle regression reports and integrity negatives. Only Go
was on validation PATH; the starter supplied installed LLVM by absolute path.
Root tests/vet and separate starter tests pass. The final package recipe repeated
with identical bytes. Reproduce with `scripts/runner_install_verify.py` using a
new result directory and the recorded archive. Maintainers use
`scripts/package_runner.py`; it emits a reviewable pin without publishing or
editing the frontend pin. The pilot's different package is preserved and rejected
by the final pin. No public download endpoint, notarization or other host
verification exists. Project license selection and complete dependency/runtime
notice review remain open; the inventory does not authorize redistribution.
General fixtures now accept step-level `sysvars` controls for complete Clock
and/or Rent before transaction construction. Controls persist across later steps
and transaction failures, and reset before the next scenario; blockhash/history
changes remain explicit. Full-suite validation rejects incomplete/null/empty,
duplicate/unknown and out-of-range controls before executing even a selected
scenario. Suites with step controls report effective typed sysvars per step;
uncontrolled reports retain their historical case shape. The
[six-scenario/15-transaction validation](results/svm/2026-10-05-scenarios-complete/README.md)
checks the unchanged 1,664-byte C syscall probe, independent byte packing,
uint64/int64 limits, serialized accounts, single/multi-instruction rollback,
partial updates, selection/reversed order and reset. The 108-token, 26-lifecycle
and 14-starter regressions remain exact. Reproduce with `scripts/svm_scenarios.py`
into a new result directory; the runner binary/package stays 0.5.0. The local
RPC bind probe is denied, so no new validator oracle is claimed. Go SDK sysvar
authoring and other sysvars remain open. Only the native System program has an
alias; address lookup tables are not implemented.

The user agreed to cap milestone 1 at the tested subset rather than an indefinite
runtime/release tail. Read [the coverage matrix and checklist](docs/SVM_RUNTIME_COVERAGE.md).
The user explicitly closed milestone 1 and authorized milestone 2, skipping the
additional fresh-validator and clean macOS arm64 host checks. Public distribution/signing, licensing/notice
review and wider-host distribution remain milestone 3 gates. Scope additional
runtime features against application needs instead of treating them as mandatory
milestone 1 work.
The accepted milestone 1 implementation and local evidence include the
[final local rehearsal](results/svm/2026-10-05-m1-acceptance-final/README.md)
source-installs an isolated CLI, starts with empty Go/module/goSVM caches, verifies
a copied managed backend and installs the pinned runner. Rebuilt starter,
matched-token adapter, manual lifecycle token and syscall-probe ELFs retain their
exact hashes; 14 starter cases, three project-general cases, 108 token scenarios,
26 lifecycle transactions and 15 sysvar transactions pass. Native project tests
are forced; only Go is on the testing PATH. The metadata-negative check rejects
validator rent epochs and clears a stale valid report. Captured-oracle review
checks all 105 Go token CU/errors, lifecycle cases, feature IDs, dependency hashes,
vendor provenance and exact runtime-source hashes against the pinned package.
Root tests/vet, separate starter/transport tests and five locked/offline Rust
runner tests pass. Local RPC binding is still denied. This existing-host run
explicitly does not establish either skipped external check. Use
[the acceptance procedure](docs/MILESTONE1_ACCEPTANCE.md) and
`scripts/milestone1_acceptance.py` for later clean-host work and fresh-validator
checks without captured-oracle reuse. Failed harness
bring-up attempts remain separate `passed:false` snapshots. Historical reports
retain their original open-gate status; the user's later waiver closes milestone 1.
Milestone 2's first compiler increment implements [bounded scalar arrays](docs/GO_ARRAYS.md):
named `[32]byte` keys, by-value copies/parameters/results, arrays in structs,
keyed/positional/inferred literals, comparisons, len/cap and checked indexing.
Elements are byte/uint32/uint64/bool (or named scalars), at most 1,024 bytes per
array value. Nested/struct elements and array slicing remain open; package imports follow below.
The [final corpus](results/compiler/2026-10-05-arrays-supported/README.md) passed 1,000
native-Go/C random vectors plus 15 edge/failure checks and three runs of 147 SBF
transactions, including full-width index bounds and atomic rollback. The 4,352-byte
ELF has static Clang frames of 320 bytes for `Process` and 64 for the entrypoint;
instrumented linking reproduces its exact hash. The
[1 KiB boundary corpus](results/compiler/2026-10-05-array-capacity-final/README.md)
adds 60 SBF scenarios across byte/uint32/uint64/bool in three repetitions, each
with a 2,112-byte handler and 64-byte adapter frame; native-Go/C adds 56 boundary
vectors. Freestanding hidden memory helpers handle LLVM-generated copies and
zeroing; array element reads and initialized declarations avoid redundant whole
array copies. These measurements apply to the tested fixtures, not every function.
Root tests/vet and nested
starter/transport tests pass. The
[existing-workload regression](results/svm/2026-10-05-m2-arrays-regression-final/README.md)
rebuilds all prior ELFs and retains exact cases/hashes. Runner/pin remain 0.5.0.
Saved native-driver sources are text and ignored build staging holds executable
inputs; older array evidence has module boundaries to avoid root test discovery.
Reproduce with `scripts/array_verify.py --output results/compiler/new-arrays`.
The second increment implements [module-aware shared-package imports](docs/GO_PACKAGES.md).
`ReadProgram` loads only the entrypoint import graph via offline `go list -find`,
with workspaces/ambient flags disabled and a private alternate mod/sum pair.
`CompileProgram` checks source packages in one type universe, preserving named
identity and deterministic symbol/dependency order. Qualified direct calls and
scalar/array conversions work; only the root Process is an ABI entrypoint. Import
cycles, main packages, illegal internal imports, unsupported features and recursion
are rejected. Globals/init are not executed or supported. Every source/helper in
a reachable package is checked; host-only packages remain outside that graph.
The cache hashes graph files/module metadata and compiles that loaded snapshot.
[Final evidence](results/compiler/2026-10-05-shared-imports-final/README.md) binds
1,011 native-Go/C vectors, three runs of 142 SBF scenarios, isolated native client
reuse and live transitive cache mutation. Application/service modules need their
own tests. All prior-workload and array/capacity regressions pass with exact ELFs;
the runner stays 0.5.0.
The third increment implements [multiple results/assignments, switch and continue](docs/GO_CONTROL.md).
Internal result aggregates preserve typed value copies, tuple forwarding and
sole-argument calls; stores follow operand/RHS evaluation and left-to-right Go
assignment order. Loop continues jump to the post, including from nested switches.
[Evidence](results/compiler/2026-10-05-control-values/README.md) passes 1,000 native
random comparisons, 10 edge/error checks and three SBF runs of 141 scenarios.
The 2,936-byte ELF has 64-byte handler/entry frames. The explicit status contract
uses unsigned codes; built-in error/interfaces and named results remain rejected.
The fourth increment implements [methods and constrained pointers](docs/GO_POINTERS.md).
Per-result borrow summaries substitute caller origins through functions, imported
methods and multiple results; local/block/loop escapes are rejected. Value copies,
mutable aliases, nil receiver/assignment timing, nested/element addresses and
method expressions agree with ordinary Go in the supported fixtures.
[Evidence](results/compiler/2026-10-05-pointer-values/README.md) passes 1,000
native-Go/C random vectors and 60 edge cases, plus 196 SBF scenarios in three
repetitions with exact bytes/failures and multi-instruction rollback. The
3,600-byte ELF has 64-byte handler/entry frames and zero-byte memory helper frames;
instrumented linking reproduces its exact hash. Root tests/vet and nested starter/
transport tests pass. The [installed-host regression](results/svm/2026-10-05-m2-pointer-regression/README.md)
retains starter/token/lifecycle/sysvar cases and ELFs.

The fifth increment implements [borrowed byte views](docs/GO_VIEWS.md), including
length/capacity, two/three-index slicing, reslicing, nil contexts and alias updates.
The same borrow analysis tracks conversions, returned/forwarded views and element
pointers and rejects local/block/loop/copy escapes. SDK buffers expose capacity
bounded to their supplied lengths; native fixtures must match those capacities.
[Evidence](results/compiler/2026-10-05-slice-views-supported/README.md) passes 1,000
random native-Go/C comparisons and 149 edge cases, plus 264 SDK/SBF scenarios in
three repetitions with exact bytes/errors, real empty SHA-256, 1 KiB backing
storage and multi-instruction rollback. The 6,280-byte ELF has 1,024-byte entry
and 1,088-byte library-handler frames; hidden helpers have zero-byte frames and
instrumented linking reproduces the exact hash. Descriptor aggregates have
freestanding copy support even without array declarations. The earlier native
fixture capacity mistake is preserved as a failed attempt, followed by separate
passing snapshots. Existing starter/token/lifecycle/sysvar, pointer and control
regressions retain their exact ELFs and outcomes.

The sixth increment implements the first [schema-2 prototype](docs/SCHEMA2.md):
two instructions/two layouts, typed bundles, packed nested/array codecs, canonical
clients, layout history and validation before handler execution. The
[compiled-SBF proof](results/compiler/2026-10-05-schema2-supported/README.md)
passes 161 independent native vectors and 165 scenarios/166 transactions in three
repetitions, including readonly/merged privileges and atomic rollback. Its
7,088-byte ELF has a 1,408-byte static entry frame. Generator guards require
a recorded migration policy, distinct historical discriminators and valid history;
focused tests additionally cover readonly aliases/program identities and named
scalar-array codecs. `compiler.TypeCheckProgram` inspects provisional declarations
in memory; final compilation still certifies the supported bodies/borrowing.
Checked token/PDA/CPI and the preloaded full swap are now implemented; the
lifecycle SDK proof above follows this first schema prototype. Generated lifecycle
policy and the distinct SOL escrow now pass the proof linked above. The full
swap lifecycle now passes the latest managed proof and repeated measurements;
the independent developer trial remains open.
The transport comparison is a separate Go module,
so run its focused tests in addition to root and generated-starter tests.

Shared-package distribution, richer arrays and the account framework
are remaining product foundations. Bounded allocation and selected interfaces/standard-library
functionality are later options, not fundamentally prohibited by SVM. Full Go
runtime compatibility is not the goal. See the roadmap for detailed gates.

The user wants normal Go backends to reuse on-chain structs and business logic
without copying definitions. This is now an explicit milestone 2 requirement in
the roadmap and multi-account design: canonical shared packages/types, Go clients
using those same types, and an independent service importing the shared quote
calculation. A native smoke example in `build/shared-go-check/` passed using the
current starter's actual `Pool`/`SwapArgs`, client codec round-trips and `Swap`
function (1,000 input -> 1,992 output for reserves 1,000,000/2,000,000). It needed
local project and embedded-SDK replacements, so normal dependency packaging is
still open. Shared on-chain dependencies must satisfy the Go subset; unrelated
backend code can use ordinary Go. That smoke check preceded the array increment;
shared-package import resolution is now implemented. The new pure model/quote/wire
example and isolated service need no SDK or compiler, with ordinary local module
replacements for the unpublished application module. Schema-2 generated clients,
public module/SDK packaging and full swap/escrow service reuse remain open.

The user has now proposed a major open-source program benchmark, naming Orca
Whirlpools as an example. The roadmap records a proposed sequence: pin and
measure the Rust reference first, then a scoped tick-crossing swap during
milestone 2, followed by wider parity. No full rewrite has started; the user
intends to have another agent begin the reference benchmark in parallel. Keep
that effort's upstream pin, fixtures and results separate from runner/compiler
implementation; no upstream revision or full-port scope is fixed here.

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
