# goSVM product roadmap

Draft, October 4, 2026. This is a proposed product plan, not a release commitment
or authorization to start every milestone. The project remains experimental.
The user has explicitly requested completion of milestone 2; its full gates are
tracked in the [completion audit](docs/MILESTONE2_ACCEPTANCE.md). Later milestones
are proposals, and no scheduled implementation is implied.

## Implementation progress

October 5, 2026: roadmap work has started. The first lightweight-runner spike
passed all existing matched bounded/token fixtures in three repetitions, with
matching CU, errors, state assertions, CPI behavior, token ELF, and active feature
IDs. Median whole-suite times were 0.87 s versus 9.22 s for tokens, and 0.19 s
versus 25.44 s for the three bounded backends. See the
[fresh evidence and limits](results/svm/2026-10-05-litesvm/README.md).

The subsequent [transport comparison](results/svm/2026-10-05-transports/README.md)
replayed complete responses through HTTP, stdio, and same-core embedding in seven
shuffled repetitions. Stdio batches are selected for the first CLI workflow;
embedding saved only a few milliseconds on these fixtures. `gosvm test --svm`
now runs compiled starter fixtures through one session, with fixture-name
selection and separate reports. See the [protocol decision](docs/SVM_RUNNER_PROTOCOL.md).
The [runner 0.2.0 follow-up](results/svm/2026-10-05-runner-02/README.md)
records another seven transport repetitions, fresh validator agreement for all
14 starter fixtures, selection, and rejection of the older runner.

Runner 0.3.0 now supplies typed Clock/Rent controls and reusable VM checkpoints;
the CLI resets before each fixture and accepts `-svm-sysvars`. A compiled syscall
probe passed three repetitions, all 14 starter fixtures matched a fresh validator,
and a separate full-token regression retained CPI/rollback agreement. See
[control evidence](results/svm/2026-10-05-controls-final/README.md).

General multi-account fixtures now run through standalone `svm-test` or project
`test --svm -svm-fixtures`. Runner 0.4.0 supports atomic reset overrides and
ordered shared-state steps. Three runs of 108 full-token scenarios passed, with
105 Go vector CU/errors matching a fresh validator. See
[general-fixture evidence](results/svm/2026-10-05-general-complete/README.md) and
[format/limits](docs/SVM_FIXTURES.md). This is separate from binding schema 2.

The [lifecycle corpus](results/svm/2026-10-05-lifecycle-verified/README.md) now
covers transaction-created token accounts, a Go swap, closing/recreation and rent
failures: six scenarios/26 transactions and three patched-runner repetitions
compared with the exact captured validator oracle. The final fresh-validator
rerun was blocked by restricted loopback binding; see the evidence limits. Runner 0.5.0 patches rent-error precedence; exact `rent_epoch` metadata
remains different and is recorded explicitly. Go pool initialization, System CPI
authoring, and resizing remain open.

The [complete workflow measurements](results/svm/2026-10-05-workflow-complete/README.md)
passed 60 measured runs. Source edits with native tests forced took median 0.286 s
for the 14-case bounded starter and 0.536 s for the manually wired 108-scenario
token suite on this host. `build`/`test -timings` adds optional phase diagnostics.
Provisional local median targets are 0.5 s and 1.0 s respectively under the
documented installed-tool/warm-cache conditions. Bootstrap and footprint remain
separate measurements; this is not a dependency-heavy protocol result.

The [local package/installer validation](results/svm/2026-10-05-runner-install-complete/README.md)
now covers embedded archive/member pins, transactional offline installation,
managed runner selection, corruption rejection and bounded/token/lifecycle
regressions without Cargo on PATH. The macOS arm64 archive is 3.17 MB; it is a
local experimental artifact with no public download or notarization.

General fixture steps now expose ordered Clock/Rent controls. The
[six-scenario/15-transaction corpus](results/svm/2026-10-05-scenarios-complete/README.md)
checks real syscalls and serialized account bytes, integer limits, inherited
controls, transaction rollback, reset/selection/reversed-order isolation and
duplicate-history semantics. Existing bounded/token/lifecycle regressions pass.
The runtime binary/package stays at 0.5.0; Go SDK sysvar authoring is still open.

The user agreed to a bounded milestone 1 finish line. Its
[coverage matrix and acceptance checklist](docs/SVM_RUNTIME_COVERAGE.md) record
the accepted evidence and the user's waiver of the additional fresh-validator
and clean macOS arm64 host checks. Additional sysvars/runtime features are application-driven
follow-ups, rather than an unlimited milestone 1 requirement. Public runner
distribution/signing, project licensing/notice review and wider-host distribution
belong to the milestone 3 public alpha gate.
The [final local acceptance rehearsal](results/svm/2026-10-05-m1-acceptance-final/README.md)
now rebuilds the application ELFs and passes the isolated source-installed CLI,
managed-backend/runner and complete bounded/token/lifecycle/sysvar workflows.
Recorded differences and exact captured-oracle/package provenance have been
reviewed. The [acceptance procedure](docs/MILESTONE1_ACCEPTANCE.md) is ready for
later fresh-validator and clean macOS arm64 host checks. The user explicitly
closed milestone 1 on October 5, accepting the recorded evidence and skipping
those two additional checks. They are not represented as passing. Milestone 2
has started with fixed-array compiler support and shared-package imports.
Full-validator tests remain the default. A
[draft multi-account binding design](docs/MULTI_ACCOUNT_DESIGN.md) scopes schema 2,
validation/write ordering, arrays/imports, and the swap/escrow implementation
sequence; its first generation prototype is implemented, with full application
and lifecycle authoring still open.

The first milestone 2 increment implements [bounded scalar arrays](docs/GO_ARRAYS.md),
including `[32]byte` keys, copies, literals, equality and checked indexing.
[Validation](results/compiler/2026-10-05-arrays-supported/README.md) passed 1,000
native-Go/generated-C comparisons plus edge cases and three repetitions of
147 real-SBF scenarios, with exact native-Go state expectations, failures and
rollback. Static frames are 320 bytes for the fixture handler and 64 for its
entrypoint. The [1 KiB boundary corpus](results/compiler/2026-10-05-array-capacity-final/README.md)
adds 60 SBF scenarios across all four scalar kinds, with three repetitions and
2,112-byte handler frames. [Existing-workload regressions](results/svm/2026-10-05-m2-arrays-regression-final/README.md)
retain every prior case and ELF hash. Arrays of structs/nested arrays and schema-2
codecs remain open.

The second increment implements [module-aware shared-package imports](docs/GO_PACKAGES.md):
qualified functions/types/constants, transitive imports, preserved named-type
identity, offline resolution and dependency-aware cache receipts. The
[shared application and independent Go client](examples/shared-packages/README.md)
use canonical types, explicit wire codecs and the same quote logic. The client
runs against an isolated copy of only the pure libraries, without the compiler or
SDK. [Evidence](results/compiler/2026-10-05-shared-imports-final/README.md) includes
1,011 native-Go/C vectors, 142 SBF scenarios in three repetitions, source-located
unsupported-dependency checks and live cache invalidation. Prior workloads and
scalar-array/capacity corpora retain their exact ELFs and outcomes. This is a
bounded state-update example with handwritten codecs, not the full token swap or
completed shared-client/framework gate. A third increment implements [multiple results/assignments, switch and continue](docs/GO_CONTROL.md),
with value-copy/operand evaluation semantics and explicit unsigned statuses.
[Validation](results/compiler/2026-10-05-control-values/README.md) passed 1,000
random native-Go/C comparisons plus 10 edges and 141 SBF scenarios in three runs.
A fourth increment implements [methods/constrained pointers](docs/GO_POINTERS.md).
[Validation](results/compiler/2026-10-05-pointer-values/README.md) passed 1,000
native-Go/C random vectors plus 60 edges and 196 SBF scenarios in three runs.
The supported lifetime analysis rejects local/block/loop escapes and preserves
caller borrows through imported methods and multiple results. Static handler and
entry frames are 64 bytes for this 3,600-byte fixture. Existing starter/token/
lifecycle/control regressions retain their exact ELFs and outcomes.
A fifth increment implements [bounded byte views](docs/GO_VIEWS.md), with
capacity/aliasing semantics and lifetime rejection through calls/conversions.
[Evidence](results/compiler/2026-10-05-slice-views-supported/README.md) passes 1,000
native-Go/C random comparisons plus 149 edges and 264 SDK/SBF scenarios in three
runs, including real empty SHA-256, 1 KiB backing storage and atomic rollback.
The 6,280-byte fixture has 1,024-byte entry/1,088-byte handler frames. Prior
starter/token/lifecycle/sysvar, pointer and control ELFs/cases are retained.
A sixth increment implements [schema-2 generation](docs/SCHEMA2.md): multiple
instructions/accounts, canonical shared types/clients, packed nested/array codecs,
layout history and owner/privilege/identity/relation/alias validation. The
[prototype proof](results/compiler/2026-10-05-schema2-supported/README.md)
passes 161 independent native vectors and 165 SBF scenarios/166 transactions in
three repetitions; its 7,088-byte ELF has a 1,408-byte entry frame. This is a
preloaded ledger example, not full-token/lifecycle acceptance. The next increment
adds [checked SDK-2 APIs](docs/SDK2.md), generated token/PDA/field relationships,
the [full swap migration](examples/full-swap/README.md) and an independent Go
consumer of the actual model/client/math. Its
[proof](results/compiler/2026-10-05-framework-swap-lifecycle-final/README.md) passes
108 scenarios/110 transactions in three runs, plus the documented 13-case
project CLI suite. The earlier generated swap-only module used 45,941 CU, compared with
the initial framework's 91,265 and the separate manual baseline's 20,329. SDK
rent/System/owned lifecycle operations now pass the
[35-scenario/46-transaction proof](results/compiler/2026-10-05-lifecycle2-supported/README.md).
Generated initialization/authorized closing and the distinct [SOL escrow](examples/escrow/README.md)
now pass [32 scenarios/42 transactions × three runs](results/compiler/2026-10-05-escrow-supported/README.md).
The [creator-managed swap lifecycle](results/compiler/2026-10-06-managed-swap-supported/README.md)
now passes 29 scenarios/36 transactions × three, including creation, deposits,
swap, drain/close/reuse and rollback. The four-instruction 68,320-byte program
also passes the preserved 108-scenario corpus and shared-service regression; its
legacy wide swap uses 46,016 CU. [Repeated comparative measurements](results/compiler/2026-10-06-milestone2-bench-supported/README.md)
now pass 125 command samples, recording setup, build/test distributions, CU and
project/shared/temporary footprint. The [independent developer trial](docs/MILESTONE2_TRIAL.md)
is the sole remaining milestone-2 gate; its concrete kit passes maintainer rehearsal.
The external scoped Whirlpools core is now independently implemented using the
unsigned subset and borrowed tick-array views. Its requested runtime Clock API
passes [12 scenarios/15 transactions × three SBF runs](results/compiler/2026-10-05-clock2-supported/README.md),
plus 1,010 native/C vectors. The sibling benchmark now also verifies its
classic-token/Clock handler against scoped and full upstream Rust in 166
scenarios repeated three times. Event emission is the next reported SDK boundary;
core and handler measurements remain distinct from full-contract parity.
Full milestone 2 remains active in the
[completion audit](docs/MILESTONE2_ACCEPTANCE.md).

## Product objective

Make goSVM a credible choice for developers building Solana applications who
value fast iteration, small build artifacts, and Go authoring. A developer should
be able to install the tools, create a useful multi-account program, test its
actual SBF behavior quickly, generate clients, deploy it, and maintain its account
layouts without becoming a compiler expert.

The initial product should serve bounded, account-oriented applications such as
escrow, vaults, and a small swap program. It should not claim to replace every
Rust application or the technical requirements of Basanos. The user accepts a
constrained Go subset and has not set hard timing or CU thresholds.

Success requires three things together: a materially better edit/test workflow,
enough language and framework capability for useful applications, and confidence
that compiled programs behave correctly. Small binaries alone are insufficient.

## Evidence and starting point

Technical feasibility is established. Go-authored SBF programs execute full-width
arithmetic, validate accounts, invoke real SPL Token transfers with PDA signing,
and preserve transaction rollback. A source-installed CLI provides scaffolding,
generation, checks, builds, native tests, validator tests, and backend installation.

The default schema-1 framework handles one account and one handler; the schema-2
prototype adds multi-instruction/account generation. The manual token baseline
retains its SDK validation; its preloaded generated migration is now verified.
The compiler now has restricted module-aware user imports and constrained
pointers/methods, but no heap allocation or interfaces. Scalar arrays are now
implemented with explicit limits; richer array use remains open. These are not
all SVM restrictions; many are implementation gaps.

In the latest matched token benchmark, Go built in 2.61 seconds from an empty
target and 0.59 seconds after an edit, versus Anchor's 155.74 and 2.47 seconds.
The Go ELF was 8,064 bytes versus 188,768; Go used 20,329 CU versus Anchor's
19,431. Thus build cost favors Go while CU is mixed. Anchor was pinned to 0.32.2.
These are shared-desktop observations, not portable performance guarantees.

The default integration path starts a full validator and waits for confirmed
transactions. The October 5 opt-in LiteSVM prototype demonstrates lower overhead
on our existing suites. It now has a pinned local package and managed installation;
public distribution remains open. This is not an execution-speed comparison with
Rust using the same lightweight engine.

Read [the handoff](HANDOFF.md), [Anchor evidence](results/anchor/README.md),
[protocol methodology](benchmarks/protocol/README.md), and
[installation results](results/polish/README.md) for details and limitations.

## Proposed milestones

Milestones are ordered by dependency and demonstrated user value rather than
calendar dates. Estimate effort after the first two investigations below. Compiler
testing and documentation accompany each feature rather than waiting for beta.

| Milestone | User outcome | Evidence required to advance |
| --- | --- | --- |
| 1. Fast development loop | Test the actual SBF program without starting a validator for every run | Equivalent fixture outcomes, CU accounting, and rollback checks in a lightweight runner; measured execution and build costs |
| 2. Useful private alpha | Write a complete multi-account example through supported Go APIs | An independent developer completes the example without editing compiler internals or hand-writing wire adapters |
| 3. Accessible public alpha | Install, test, generate clients, and deploy from documented workflows | Fresh-machine CI and external trial users complete the workflow on supported hosts |
| 4. Dependable beta | Maintain a nontrivial application across upgrades | Differential/fuzz coverage, security review, migration and deployment verification, measured performance on representative programs |
| 5. Supported release | Rely on documented behavior and a maintained compatibility policy | Stable supported APIs, release/recovery process, reviewed security posture, and demonstrated user adoption |

## Milestone 1 Fast development loop

Evaluate a lightweight SVM engine behind a prebuilt runner shared by projects.
[LiteSVM](https://github.com/LiteSVM/litesvm) and
[Mollusk](https://github.com/anza-xyz/mollusk) were initial candidates. LiteSVM
0.8.2 is now selected for the experimental CLI after the recorded compatibility
and transport investigations. Broader SBF version, syscall, transaction, and
feature-set coverage still require validation against pinned artifacts. Earlier
ProgramTest experiments exposed an ELF compatibility mismatch, so retain this
explicit acceptance gate for runtime changes.

The selected runner is implemented in Rust and uses batched stdio after comparison
with an embedded binding. Developers should eventually download a verified binary
once rather than compile its dependency graph in each Go project. Continue to
account for process/serialization overhead and portability. Do not spawn a runner
per individual fixture.

Deliver three clear testing levels:

- Native Go tests for business logic, properties, and fuzzing.
- Fast SVM tests for the compiled ELF, real token CPIs, CU, account changes,
  transaction atomicity, and exact failure categories.
- Full-validator tests for compatibility and behavior requiring the wider runtime.

Provide deterministic account fixtures, clock/sysvar control where supported,
fresh state or snapshots between cases, test selection, useful failure diffs,
logs, and CU reports. Keep independent tests isolated; preserve instruction order
within stateful scenarios. Avoid hiding unsupported behavior behind mocks.

Run the existing bounded and token suites through both the new runner and the
validator. Investigate differences instead of weakening assertions. Report
installation, cold compilation, edited-test rebuild, startup, and execution
separately. Establish practical latency targets from these measurements; do not
claim subsecond SVM suites before demonstrating them. The October 5
[workflow experiment](results/svm/2026-10-05-workflow-complete/README.md) now supplies
local edit/test targets and separate bootstrap/cache/footprint evidence for the
bounded and manually wired full-token programs. Validate those targets on other
supported hosts before treating them as product guarantees.

Exit: all applicable existing cases agree on state, errors, CPI behavior, and
rollback, with documented feature alignment and materially lower integration
overhead. Users retain a straightforward full-validator verification path.
The [acceptance checklist](docs/SVM_RUNTIME_COVERAGE.md) bounds this gate to the
current SBFv3/legacy/classic-token/Clock-Rent subset on macOS arm64, with explicit
metadata limits and recorded compatibility review. The user waived the additional
fresh-validator and clean-host checks when closing this milestone. Public
distribution and wider runtime support are later gates.

## Milestone 2 Useful private alpha

The optional [SDK 2](docs/SDK2.md) increment now supplies checked classic-token
references/transfers, bounded multi-seed PDA signing, generic CPI and current
lamport reads. [Validation](results/compiler/2026-10-05-checked-token-lifecycle-final/README.md)
passes 116 native vectors, 1,011 native-Go/C boundary vectors and 117 real-SBF
scenarios in three repetitions, including exact downstream errors and rollback.
Generated token/PDA constraints and the preloaded full swap now pass their full
corpus. [Lifecycle SDK operations](results/compiler/2026-10-05-lifecycle2-supported/README.md)
also pass 35 scenarios/46 transactions in three runs for current rent, creation,
funding, resize, close/reuse and rollback. Generated rent-funded initialization
and authorized closing also pass the [escrow application proof](results/compiler/2026-10-05-escrow-supported/README.md).
The complete [creator-managed swap lifecycle](results/compiler/2026-10-06-managed-swap-supported/README.md)
now passes its actual-SBF corpus, with checked signed vault creation, funding,
draining/closing and reuse. [Repeated measurements](results/compiler/2026-10-06-milestone2-bench-supported/README.md)
now pass; the [independent developer trial](docs/MILESTONE2_TRIAL.md) below remains open.

Add the language features needed by the multi-account example, with explicit
semantics and tests:

- User packages and module-aware import resolution within the supported subset;
  clear diagnostics when a dependency requires unsupported features.
- Fixed-size arrays for public keys, hashes, and bounded collections; safe slice
  views where their backing storage and lifetime are known.
- Methods and constrained pointers for updating caller-owned values. Preserve
  Go lifetime/aliasing behavior or reject unsupported escapes at compile time.
- Practical control flow and return values, including multiple results and a
  documented error representation. Do not promise built-in `error` compatibility
  before its interface semantics are implemented.

Expand the framework to multiple instructions and accounts. Generate validation
for ownership, signer/writable flags, exact layouts/discriminators, PDA seeds and
bumps, account relationships, and aliasing policies. Provide checked classic SPL
Token helpers and enough CPI support to express the reference application.
Handle initialization, rent funding, closing, and resizing where the example
requires them, with explicit authority and payer rules.

Define stable wire layouts independently of native Go/C layout. Generate clients
and IDL from the same schema, document account versioning, and reject accidental
layout changes where they can be detected. Keep generated Go inspectable.

Enable ordinary Go services to import the same account/argument types, constants,
and reusable business logic as the on-chain program. Define these once in shared
Go packages; generated Go clients must use the canonical types rather than
independent lookalikes. Only packages reachable from the on-chain entrypoint must
meet the restricted compiler subset. An off-chain service can use normal Go
libraries in its own code. Keep shared model/math packages free of runtime/CPI
dependencies, and resolve module/SDK packaging so consumers do not need the
goSVM checkout, compiler, SBF tools, or handwritten SDK replacement directives.
Include an independent Go service that imports the swap's types, decodes account
data, constructs instruction data, and calls the shared quote calculation.
Verify common wire vectors and native-Go/SBF business-logic agreement.

Migrate the existing full token swap to this authoring model while retaining its
manual implementation as a historical baseline. Add a distinct small application,
such as escrow, to check that the APIs generalize. Include initialization and the
relevant full lifecycle, not only genesis-preloaded happy paths. Exercise invalid
authority, account substitution, duplicate accounts, failed CPI, and rollback.

Exit: external trial developers can build and test these examples with the CLI
and documentation, without editing the compiler, writing C, or manually assembling
the framework's byte offsets. Generated validation preserves the tested security
policy. Record build, CU, and footprint changes introduced by the framework.
The separate Go service must use the actual shared types and calculation without
copying structs, arithmetic or wire offsets.

## Milestone 3 Accessible public alpha

Ship versioned CLI and runner binaries with checksums and appropriate signing.
Verify installation and upgrades on macOS arm64 and Linux x86_64 first; make
other host support explicit rather than implying it. Publish platform and
toolchain compatibility, supported Go syntax, installation requirements, and
experimental limitations. A public alpha may support test/devnet use before
production use is recommended.

Improve backend packaging so users need not fetch a 448 MB archive merely to
retain Clang/LLD, subject to redistribution and licensing review. Keep compiler,
runner, and validator downloads separate and shareable. Normal builds remain
offline and never silently install tools. Support pinned versions, diagnostics
for incompatible installations, and a documented rollback path.

Deliver a getting-started tutorial, language reference, account/PDA/token cookbook,
testing guide, and troubleshooting guide. Native Go tooling should remain useful
for completion, navigation, and tests. Compiler errors should identify Go source
locations; runtime diagnostics should expose logs, CU, and actionable account or
stack failures without forcing users to read generated C.

Provide Go and TypeScript clients from a documented IDL. Anchor-compatible IDL
is a design option to evaluate, not a compatibility claim. Add a deployment and
upgrade workflow using established Solana tooling where appropriate, with explicit
cluster/program identity, authority handling, rent estimates, artifact verification,
and buffer cleanup/recovery documentation. Never make a test command deploy to a
public cluster.

Before distribution, choose the project license and support ownership, review
third-party notices and redistribution rights, establish a release repository,
and add CI for builds, native/nested-module tests, SVM tests, and installation.

Exit: someone on a fresh supported machine can follow the docs through install,
scaffold, edit, native/SVM tests, client use, and a test deployment. Trial feedback
demonstrates that the workflow is understandable without author assistance.

## Milestone 4 Dependable beta

Broaden confidence before broadening the production claim. Differentially test
supported Go semantics against native Go and execute generated SBF for relevant
cases. Fuzz arithmetic, evaluation order, layouts, bounds, aliasing, pointer
lifetimes, malformed inputs, and failure paths. Include stack/heap exhaustion and
compiler regressions as the language expands. Audit the C adapters and generated
checks as well as the frontend.

Arrange independent review of the compiler/runtime boundary and account/CPI
framework. Review application examples separately; compiler correctness is not
a custody audit. Document vulnerability reporting, release provenance, dependency
updates, and handling of known defects.

Exercise account migrations, schema/client compatibility, upgrade authority
changes, deployment recovery, and clean upgrades of the developer tools. Add
representative third-party programs so benchmarks do not only measure workloads
designed alongside the compiler. Validate the selected Solana feature sets and
toolchains in CI and publish the tested matrix.

Track regressions in build time, test latency, CU, ELF size, memory, and installed
footprint. Investigate CPI marshaling and LLVM compile cost as measured experiments;
retain baselines and correctness coverage. O1 previously increased token CU about
45%, so a faster compiler setting is not automatically a product improvement.

Exit: independent users maintain nontrivial programs, critical review findings
are resolved, important semantics have durable regression coverage, and the team
can explain compatibility and performance limits accurately.

## Milestone 5 Supported release

Publish the language subset, framework/client schema, account layout rules,
supported platforms, toolchain pins, compatibility windows, and deprecation policy.
Provide reproducible artifact/provenance checks, an upgrade and recovery guide,
and a sustainable release/security maintenance process with named maintainers.

The release decision should use developer adoption and reliability evidence,
alongside measured performance. Support a clearly bounded application class first.
Do not describe version 1 as full Go, universal Rust replacement, or production
safety for every application.

Exit: developers can reasonably depend on the documented contract and its
maintenance, with any remaining experimental components clearly separated.

## Language scope over time

| Feature | Proposed direction |
| --- | --- |
| Packages, arrays, methods, practical control flow | Product foundations; implement before asking users to build substantial applications |
| Signed integers and richer value types | Add with defined overflow/division behavior and compiler/SBF tests as applications require them |
| Pointers | Start with known lifetimes and caller-owned storage; broaden only with sound escape handling |
| Allocation | Later bounded invocation-local allocation; assess `new`, `make`, and `append` under an explicit memory budget |
| Interfaces and generics | Later, prioritizing useful compile-time resolution; evaluate code size and compilation costs |
| Standard library | Select supported packages/functions such as bit arithmetic, encoding, byte operations, and suitable cryptography |
| Full garbage collector and goroutine scheduler | Not a product objective; runtime size and predictability costs conflict with current priorities |
| Filesystems, outbound networking, processes | Not on-chain features; clients and native tests retain ordinary Go capabilities |

Allocation and interface dispatch are not categorically forbidden by SVM. They
require a useful cost model and correct semantics. Prefer explicit diagnostics
to silent deviations from Go. Any SVM-specific API should be clearly named and
documented. Persistent state remains encoded in accounts, not heap pointers.

## Performance and competitive evaluation

Compare both lean Rust and an idiomatic framework implementation using matched
behavior, safety checks, wire formats where practical, and equivalent runtime
features. Include lightweight Rust SVM testing options when evaluating test
execution; ProgramTest alone is not the entire competitive baseline. Record
framework versions and evaluate an appropriate newer baseline before release
claims; the Anchor 0.32.2 snapshot is not universal.

| Measure | Required distinction |
| --- | --- |
| Setup | Download, tool installation, and frontend/runner bootstrap |
| Builds | Empty project target, source edit, test edit, and no-op |
| Tests | Native logic, actual SBF, validator integration; separate compile/startup/execution |
| Runtime | Successful and failed CU, full lifecycle, CPI, and memory use |
| Footprint | Deployed ELF, project artifacts, shared caches, installed tools, and temporary peak use |
| Developer experience | Time to first passing program, manual wiring, confusing failures, and unsupported-feature encounters |

Keep cold and cached states explicit, record machine/load conditions, and use
repeat samples where practical. Preserve each experiment under a distinct
`results/` snapshot. A small binary may reduce rent deposits but does not establish
lower CU or smaller application state. Claims must keep these costs separate.

## Proposed external program benchmark

The user wants a major open-source Solana program rewritten in goSVM and
compared across build, test, runtime and footprint metrics. Orca Whirlpools is
a candidate, not yet a selected revision or an authorized full-port task. Its
[current tick state](https://github.com/orca-so/whirlpools/blob/main/programs/whirlpool/src/state/tick.rs)
and [swap manager](https://github.com/orca-so/whirlpools/blob/main/programs/whirlpool/src/manager/swap_manager.rs)
use signed tick indices, wide integer arithmetic, arrays and mutable state.
These expose gaps in the current Go authoring subset and framework.

Proposed sequence: select and pin an upstream revision, establish reproducible
Rust builds and reference fixtures after the current milestone 1 slice, then
use a scoped classic-token swap path with tick crossings to guide the latter
part of milestone 2. Add checked signed/wide arithmetic helpers alongside the
array/package/mutability/account-binding foundations as required. Extend toward
pool/position/liquidity/fee lifecycle and full contract parity after that first
path is correct. This need not wait for a supported release.

Match validation, account layouts, rounding, errors, clock inputs and token CPI
behavior before interpreting CU. Measure cold/no-op/edited builds, native and
actual-SBF test latency, successful/failed CU, ELF size, project artifacts,
shared caches and installed tools separately. A scoped Go port must have a
matched scoped Rust denominator for ELF/build claims; the full upstream program
can also be shown with its broader feature scope stated. Reference fixture work
can begin before the full rewrite without requiring new compiler work immediately.
The user intends to have another agent start the reference benchmark in parallel.
The exact upstream revision and scope still need to be pinned by that effort.

## Immediate next work and decision points

The initial compatibility spike, architecture comparison, and first fast-test CLI
slice, Clock/Rent controls, and VM checkpoints are complete; see the progress
section above. General fixtures and initial account-lifecycle coverage are
implemented, and complete edit/test workflows now have local measurements and
provisional targets. Pinned local packaging/installation is validated on this
host, and stateful Clock/Rent fixtures are covered. Milestone 1 is closed by user decision, with additional fresh-validator and
clean-host checks skipped. Scalar arrays, imports, multiple results/control flow and constrained
pointers/methods and byte views are implemented. Continue the
schema-2/application gates in the
[completion audit](docs/MILESTONE2_ACCEPTANCE.md). Public distribution requires
milestone 3 release ownership, signing and notice review. Neither path requires rewriting
Basanos or adopting a full Go runtime.

After the binding prototype, ask trial users
to write the reference application. Use those outcomes to set the private-alpha
scope rather than accumulating language features without an application need.

Reconsider the product direction if useful applications repeatedly require a
large runtime, if the compiler cannot preserve supported Go semantics reliably,
or if broader framework support erases the measured workflow advantage. None of
those outcomes has been demonstrated so far. The current evidence justifies
continuing toward a focused private alpha, not skipping the intervening gates.
