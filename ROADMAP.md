# goSVM product roadmap

Draft, October 4, 2026. This is a proposed product plan, not a release commitment
or authorization to start every milestone. The project remains experimental.
The user requested this roadmap after the initial session checkpoint; no ongoing
goal loop or scheduled implementation is implied.

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

The generated framework currently handles one account and one handler. The full
token program still uses manual SDK validation. The compiler has no general user
package imports, arrays, pointers, heap allocation, or interfaces. These are not
all SVM restrictions; many are implementation gaps.

In the latest matched token benchmark, Go built in 2.61 seconds from an empty
target and 0.59 seconds after an edit, versus Anchor's 155.74 and 2.47 seconds.
The Go ELF was 8,064 bytes versus 188,768; Go used 20,329 CU versus Anchor's
19,431. Thus build cost favors Go while CU is mixed. Anchor was pinned to 0.32.2.
These are shared-desktop observations, not portable performance guarantees.

Fast integration execution is still missing. Our current runner starts a full
validator and waits for confirmed transactions. Native tests are fast and test
compilation is much lighter, but the existing integration measurements do not
establish faster SVM execution than Rust testing tools.

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
[Mollusk](https://github.com/anza-xyz/mollusk) are candidates, not selected
dependencies. They execute programs without a full validator; exact SBF version,
syscall, transaction, and feature-set compatibility must be proven against our
pinned artifacts before choosing one. Earlier ProgramTest experiments exposed
an ELF compatibility mismatch, so this is an explicit acceptance gate.

The runner may be implemented in Rust. Developers should download a verified
binary once rather than compile its dependency graph in each Go project. Compare
a batched sidecar protocol with an embedded binding before committing to an ABI;
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
claim subsecond SVM suites before demonstrating them.

Exit: all applicable existing cases agree on state, errors, CPI behavior, and
rollback, with documented feature alignment and materially lower integration
overhead. Users retain a straightforward full-validator verification path.

## Milestone 2 Useful private alpha

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

Migrate the existing full token swap to this authoring model while retaining its
manual implementation as a historical baseline. Add a distinct small application,
such as escrow, to check that the APIs generalize. Include initialization and the
relevant full lifecycle, not only genesis-preloaded happy paths. Exercise invalid
authority, account substitution, duplicate accounts, failed CPI, and rollback.

Exit: external trial developers can build and test these examples with the CLI
and documentation, without editing the compiler, writing C, or manually assembling
the framework's byte offsets. Generated validation preserves the tested security
policy. Record build, CU, and footprint changes introduced by the framework.

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

## Immediate next work and decision points

The recommended next implementation slice is a compatibility/performance spike
for the prebuilt SVM runner, paired with a written design for multi-account
bindings and the minimum language features they require. These investigations
can proceed independently; neither requires rewriting Basanos or adopting a
full Go runtime. Scope and estimate them before committing to release dates.

After the spike, select the runner architecture using actual ELF/CPI/rollback
results and installation footprint. After the binding prototype, ask trial users
to write the reference application. Use those outcomes to set the private-alpha
scope rather than accumulating language features without an application need.

Reconsider the product direction if useful applications repeatedly require a
large runtime, if the compiler cannot preserve supported Go semantics reliably,
or if broader framework support erases the measured workflow advantage. None of
those outcomes has been demonstrated so far. The current evidence justifies
continuing toward a focused private alpha, not skipping the intervening gates.
