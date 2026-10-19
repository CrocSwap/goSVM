# goSVM session handoff

Updated October 16, 2026 (America/New_York).
**Current phase:** the user's two pre-core-language hurdles (Fogo/Solana with
minimal user awareness; Go+Bend+Rust in one program) are met and merged to `main`;
core-language work has started. See the October 9 section below.

**On-chain bump search (October 19, branch `gap4-find-pda`; unreleased, for
the Hyperlane Sealevel port):** `pda.Seeds.FindAddress(c, program)` is
`find_program_address`: bumps 255 down to 1 (Solana never tries 0) through
`solana.CreateAddress`, skipping on-curve results (status 1), about 1,570 CU
per attempt. It matches 401 vectors from Rust `solana-pubkey` 3.0.0, and the
host `fixture.FindProgramAddress` was aligned (it used to try bump 0; no
fixture changed). SDK Go only: full-swap ELFs unchanged; the SDK-2 example
snapshots were refreshed. [examples/find-address](examples/find-address/README.md)
passes `gosvm test --svm` on both targets.
[Evidence](results/compiler/2026-10-19-find-address/README.md).

**First-program tutorial and release walkthrough (October 18):**
[docs/TUTORIAL.md](docs/TUTORIAL.md) builds a PDA counter
([examples/tutorial](examples/tutorial/README.md), in the CI module loop): scaffold,
native tests, SVM fixtures with a rollback case, devnet deploy, `gosvm call` and a
stdlib Go client. The published v0.1.0-alpha.1 installer, toolchain and tutorial
sections 1–4 pass from an isolated HOME; **the devnet deploy/calls are pending**
(devnet airdrops returned 429 all session) and the tutorial's deploy/call output
is marked as not yet recorded. `gosvm test --svm` now defaults to
`testdata/svm.json`. [Evidence and rough edges](results/svm/2026-10-18-release-devnet/README.md).

**Typed SVM fixture builder (October 18, user-approved, unreleased):** host-only
`gosvm/sdk/fixture` ([sdk/fixture](sdk/fixture/fixture.go), shipped in SDK-2
snapshots; older snapshots load without it and generation adds it) writes
`testdata/svm.json` from Go with name-derived keys, PDAs, rent-exempt states,
per-scenario expected-state tracking and automatic rollback expectations. Schema-2
clients now generate `XKeys`/`XInstruction` builders (aliased
`Instruction`/`AccountMeta` types shared with the builder) and `Err*` code
constants. `gosvm new -schema 2 -sdk 2` adds `cmd/fixtures`, its staleness test
and output, passing `gosvm test --svm` on both targets. The tutorial's generator
was ported with a byte-identical `svm.json`, identical ELFs and CU.
[Reference](docs/SVM_FIXTURES.md#writing-fixtures-in-go).

**Runner 0.5.1 replaces 0.5.0 for fogo-v0/legacy (October 16, user-approved;
not published):** resolves the 0.5.0 source decision below. 0.5.1 is the committed
`benchmarks/svm-runner` source (LiteSVM 0.8.2 + rent-error patch, plus `returnData`
in simulation results and `getTransaction` metadata), built from source on every
platform; the macOS binary `ca1f9f71…` is reproducible (staged sources at a fixed
path + `--remap-path-prefix`; identical from two checkouts, Cargo homes and cold
target dirs with Rust 1.98.1 and the recorded Apple linker). `gosvm runner install`
and `toolchain install` now download `runner-v0.5.1` and `runner-v0.6.0`; there is
no `runner-v0.5.0` release, but every 0.5.0 pin stays accepted (`-archive`, existing
installs, which `fogo-v0`/legacy still select when 0.5.1 is absent). MIT crates
shipping no license text get the upstream text from `third_party/crate-licenses/`
(none "review required" now; the packager refuses otherwise), so the 0.6.0 release
archive was re-pinned (`cf6e8444…`, binary unchanged; the earlier unpublished
candidates stay accepted). `make test` passes with the downloaded runners.
Linux runner pins remain the release-workflow TODO.
[Evidence](results/svm/2026-10-16-runner-0.5.1/README.md),
[packaging](docs/SVM_RUNNER_PACKAGING.md).

**Public-alpha installation prepared, not published (October 16, branch
`worktree-agent-a92fd8a88da7b542d`):** nothing was pushed, tagged or released;
publishing is the maintainer procedure in
[DEVELOPMENT § Cut a release](docs/DEVELOPMENT.md#cut-a-release), pending user
approval. `gosvm runner install` (no `-archive`) now downloads the pinned runner
archives from GitHub releases `runner-v0.5.0`/`runner-v0.6.0`
(`GOSVM_RELEASE_BASE_URL` overrides the base; https/http/file) and verifies them
exactly like local archives; `gosvm toolchain install` also installs the runners
(`-runners=false` skips). Release pins live in `internal/runner/pins/release/`;
the macOS release archives **repackage the qualified binaries unchanged** (binary
SHA-256s `886ea293…`/`550bac10…`) adding LICENSE/NOTICE/crate-license inventory,
so their archive pins are new while the historical archive pins stay accepted.
**Linux runner pins are a TODO**: the Docker daemon was unresponsive, so
`release.yml` (component=runners) builds them in a digest-pinned
`rust:1.98.1-bookworm` container and `scripts/update_runner_pins.py` commits them.
**Found:** the pinned 0.5.0 binary was built from an older
`benchmarks/svm-runner/src/lib.rs` than the committed one (which adds `returnData`
to simulation results), and both macOS binaries embed local paths, so neither is
reproducible from the repo; a Linux 0.5.0 built from source would differ in
behaviour under the same version — a user decision before publishing it
([details](docs/SVM_RUNNER_PACKAGING.md#building-runner-archives-maintainers)).
Toolchain: platform-tools v1.51/v1.57 archives and v1.57 Clang/LLD content pins
for linux-amd64 and linux-arm64 (all four Linux archives downloaded and checked
against GitHub's digests); the darwin gates are gone; Rust libraries stay macOS
only with an explicit message. `gosvm doctor` outside a project now checks fogo
and solana instead of failing on the legacy backend (walkthrough friction).
CLI release: `scripts/package_cli_release.py`, `scripts/install-release.sh`
(checksum-verified binary install), `gosvm version` stamps the release.
CI: `.github/workflows/ci.yml` (linux-amd64 + macOS arm64: vet, `make test`,
module tests, starter walkthrough, reference-ELF rebuild that qualifies the Linux
backend pins) and manual draft-only `release.yml`. Linux was not run locally.
[Evidence](results/svm/2026-10-16-public-install/README.md).

**Core language (October 14):** the user chose to start with features existing
programs work around. Landed: `range` loops (Go 1.22 semantics; `int` keys are
the one admitted `int` variable, restricted like `len` results —
[GO_CONTROL](docs/GO_CONTROL.md)), `uint16` throughout the compiler and schema 2
(fields, codecs, PDA seeds), and `import "math/bits"` (64/32/16/8-bit functions,
Go-exact incl. Div traps — [GO_BITS](docs/GO_BITS.md)). The frozen check corpus
lists the entries these changed in `corpusChanges`. Then, at the user's
request, the remaining roadmap language items, all with Go-exact semantics,
host differentials against native Go and SBF runs on both targets:
[signed integers and `uint`](docs/GO_INTEGERS.md),
[generics](docs/GO_GENERICS.md) (monomorphized in Check),
[bounded allocation](docs/GO_ALLOCATION.md) (bump allocator, Go 1.22 append
growth, Go-style escape analysis that moves escaping locals to the heap rather
than rejecting them — a choice the user may revisit) and
[interfaces](docs/GO_INTERFACES.md) (boxing on the heap, compare-and-branch
dispatch; `error` stays out because strings are not in the subset). Each added
traps recorded in [ERROR_CONTRACT](docs/ERROR_CONTRACT.md), which also contracts
append growth and heap accounting. `gosvm features` reports per-program `uses`
for Qushta's subset gating. Later the same day:
[strings](docs/GO_STRINGS.md) (with `error`),
[function values, closures, `defer` and named results](docs/GO_FUNCTIONS.md)
(compare-and-branch dispatch; recursion through function values rejected
conservatively by signature), [maps](docs/GO_MAPS.md) and compound assignment
on every integer type and target. User decisions (October 14): maps give Go's
results but every `range` over a map is rejected (Go randomizes order); floats
stay out (fixed-point integers); escaping locals keep moving to the heap;
`Add64`/`Sub64` treat any nonzero carry as 1 (compiled Go). Evaluation order
within expressions is gc's (see the October 15 entry below). Then,
from the [library survey](results/compiler/2026-10-14-library-shakeout/README.md):
[struct `==`, type aliases and `panic`](docs/GO_TYPES.md) and
[arrays of composite elements](docs/GO_ARRAYS.md)
([evidence](results/compiler/2026-10-14-value-types/README.md)). `panic` is a
trap that logs one contracted line (no custom error code is possible; deferred
calls do not run; `recover` is rejected). Still out: floats,
goroutines/channels/select, `recover`, map iteration, `clear` on slices.

**Package-level variables and `init` (October 14, branch
`worktree-agent-a4d54a33b60ae5969`):** the library shakeout's largest
blocker. [GO_GLOBALS](docs/GO_GLOBALS.md): Go's initialization order (go/types
dependency order, imported packages first in Go 1.21's package order, `init`
functions in file/source order), re-run on every invocation before `Process`
(a generated `go_Process` wrapper); writable variables live in one block that
is the invocation heap's first allocation (contracted in
[ERROR_CONTRACT](docs/ERROR_CONTRACT.md#contracted-runtime-behaviour)); a sound
syntactic analysis puts never-written variables with constant initializers in
read-only data at zero CU. Go-exact choice: every initializer of every package
runs (they can trap), so computed constants such as `MustFromHex(...)` cost CU
on every invocation. Check lowers each package's initializers into a synthetic
`init.vars` function (`CheckedProgram.Globals`, `GlobalBytes`, `Inits`), so
borrow checking, escape analysis, closures and interfaces need no special
cases. Operands follow gc's [evaluation order](docs/ERROR_CONTRACT.md#evaluation-order)
(October 15). Evidence: a five-package example
against one fresh native process per vector (2,042 host vectors; 242 SBF cases
on both targets), [results](results/compiler/2026-10-14-globals/README.md);
full-swap ELFs unchanged; the nine recorded corpus rejections now compile.

**Both merged (October 14):** package-level variables and the value types
(struct `==`, aliases, composite arrays, `panic`) were developed in parallel
and merged together; they compose without special cases: arrays of structs
and arrays are ordinary (read-only or writable) globals mutated in place
(`G[i].F = v`, `pkg.G[i][j]++`), alias-typed globals are their aliased
types, and a `panic` during initialization logs its contracted line and
aborts before `Process`. The merge added one rule change: an array type above
the 1,024-byte per-value limit (which protects 4 KiB SBF frames) is admitted
when its only values are package-level variables used in place — indexed,
`len`/`cap`, index-only `range`, read-only literal
([large tables](docs/GO_GLOBALS.md#large-tables)) — which admits uint256's
`var pows = [60]Int{...}`; every other use keeps the rejection. ERROR_CONTRACT
also gained a [panic backend-reproduction](docs/ERROR_CONTRACT.md#backend-reproduction)
subsection for Qushta. Evidence: `examples/global-value-types` (1,402 host
vectors against fresh native processes; 152 SBF cases on both targets), eight
initialization-panic probes against native Go and on SBF,
[results](results/compiler/2026-10-14-globals-value-types/README.md); full-swap
ELFs unchanged; the corpus has 62 rejections left (guard 60).

**Standard library (October 15, branch `worktree-agent-a4919db3f7fd1c8b4`,
merged with the above):** [GO_STDLIB](docs/GO_STDLIB.md): `errors`, `math`
(integer limits), `encoding/binary` (byte orders, varints), `unicode/utf8`,
`unicode` (case mapping, `IsSpace`), `strings`, `bytes` and `strconv` (no
floats) are curated Go 1.22.0 source embedded in the compiler
(`internal/compiler/_stdlib/`, Go's LICENSE, `PROVENANCE.json` with file
hashes) and compiled with the program; left-out symbols are reported by name
with the reason. Changes from Go are marked `goSVM:` in the source: `errors.As`
is generic over `*T` (no reflection); `Is`/`As` walk `Unwrap() []error` trees
with explicit stacks; the recursive `Error` methods of `joinError`/`NumError`
go through `internal/errtext` and a restricted dispatch (`MethodSlot.Except*`);
three intrinsics (`Function.Intrinsic`: comparable, unsafeString, noescape).
Added language features: variadic declared functions and methods (Check
rewrites calls to pass a slice) and pointers to pointers (sound borrow rule
for `&p`). **Found: a latent SBF miscompilation** — clang passes C struct
parameters `byval` and the SBF backend does not copy them, so a function that
writes a string/slice/struct parameter and passes it on can corrupt its
caller; fixed for overlay code only (copy at entry, `byval.go`); applying it to
program code changes 64 corpus C hashes and the full-swap ELFs, so it needed a
user decision (since resolved: see the aggregate-parameter entry below). Evidence:
[results](results/compiler/2026-10-14-stdlib/README.md) (30,906 host vectors,
306 SBF cases on both targets, CU probes, real-stdlib shakeout rerun);
full-swap ELFs unchanged.

**Evaluation order (October 15, branch `worktree-agent-a5aa296099e430346`,
merged with the standard library):**
at the user's decision goSVM now matches gc (Go 1.22.0) exactly, as
Qushta asked, and the order is contracted in
[ERROR_CONTRACT](docs/ERROR_CONTRACT.md#evaluation-order): within each
statement or root expression, calls and the other *sequenced* operations of
gc's order pass run first, left to right; plain operands (variables, fields,
elements, indirections) are read afterwards, and their traps fire after the
calls' panics. Probes of native gc (default, `-l`, `-N -l`) fixed the rules,
including gc's early copies (`len`/`cap`/`append`, slices, `&&`/`||`,
non-pointer type assertions, interface-conversion operands converted by
address, generic-path map keys, the `m[k] = append(m[k], ...)` form) and map
literals inserting constant entries first. Check records the order
(`CheckedProgram.Sequenced`, `Hoisted`, `MapLiteralOrder`,
`SharedOperands`; [backend interface](docs/BACKEND_INTERFACE.md#evaluation-order));
a sound analysis hoists only where left to right could observably differ, so
the full-swap ELFs are byte-identical. One gc-optimizer effect is documented,
not matched: inlined nil-dereference faults may move after a later inlined
panic. Evidence: `examples/eval-order` (978 cases, 5,868 host vectors; 2,934
SBF cases on both targets), [results](results/compiler/2026-10-15-eval-order/README.md).
Merged with the standard library: the analysis also covers the overlay's
code, variadic calls (Check's slice literal is an ordinary plain operand) and
pointers to pointers; 20 cases for these were added (998 cases: 5,988 host
vectors, 2,994 SBF cases on both targets, all matching native Go), the
standard-library differential and SBF suites pass unchanged, the corpus
needed no new entry, and the full-swap ELFs are still `4697a9f5…`/`2b5f21d9…`.

**Aggregate parameters on SBF fixed for all code (October 15, user decision):**
the SBF backend lowers C `byval` parameters as the caller's own memory without
copying, so on SBF (never on the host) a callee could change its caller's
string/slice/struct/array/interface variable by writing its parameter (value
receivers and interface boxes included), could read the caller's variable's
*new* value after changing it through a package variable or pointer (found in
the audit), and `t := append(s, x)` with spare capacity could lengthen a
package-level or heap `s` (the C runtime's growth helper wrote its parameter).
`examples/byval-values` reproduces it in plain Go: 15 of its 21 cases differ on
SBF at the previous commit and all match native Go now (host always matched).
Every function and literal now copies, at entry, each aggregate parameter it
writes (plainly) and each one it may read after a write that can change the
caller's variable (opaquely; a type- and confinement-aware analysis keeps it
off ordinary code); copies over 128 bytes stay whole in memory; the growth
helper returns a new header. Programs without such parameters keep their C;
the corpus lists 111 changed C hashes (64 copies, the rest append's helper).
**The full-swap reference ELFs changed** (fogo-v0 `4697a9f5…` → `a66c5a97…`,
solana-v3 `2b5f21d9…` → `b419245b…`, target-less v1.51 `4e96d2bb…` →
`05f52dd2…`; `clock2` unchanged): only `Swap`, `ManagedSwap` and `CreatePool`
(their accounts struct) and `pda.AddKey`/`pda.Address` write aggregate
parameters. Behaviour is unchanged on both targets (same errors, accounts and
rollback in every fixture); wide swap +603/+599 CU, pool creation +717–807,
early-failure paths 0; ELFs −224/−152 bytes. The new reference is
[results/compiler/2026-10-15-byval-rebuild](results/compiler/2026-10-15-byval-rebuild/README.md)
(`scripts/verify_rebuild.py`'s default `--reference`); earlier results keep
the old hashes as their historical record. Docs: [GO_STDLIB](docs/GO_STDLIB.md#aggregate-parameters-on-sbf),
[backend interface](docs/BACKEND_INTERFACE.md#aggregate-parameters) (backends
must give callees their own copies), ERROR_CONTRACT changelog (bug fix).

**Stack-frame diagnostics are build errors (October 15):** the v1.57 SBF
backend reports frames over 4,096 bytes as `Error: Function X overflows the
maximum allowed frame space ...` (v1.51: `... Stack offset of N exceeded
...`) yet Clang exits 0, so goSVM used to ship such ELFs. `gosvm build` (and
every in-process build) now fails with `SBF stack frame exceeds 4096 bytes`,
naming the Go function or the entrypoint (`internal/compiler/stackframe.go`,
`TestFrameOverflowFailsBuild`). fogo-v0 and solana-v3 both diagnose; the
legacy v1.51 `-mcpu=v3` profile prints nothing for the same program. No test
program, example or the full swap hits it. An earlier, discarded variant of
the byval fix did (`go_Process` 4,480 bytes in the full swap), which is how
it was noticed.

**Real third-party libraries (October 16, branch
`worktree-agent-a2fb31a20d9d41f9d`):** `examples/real-libraries` (its own
module) requires holiman/uint256 v1.3.1, KyberNetwork/int256 v0.1.4,
KyberNetwork/uniswapv3-sdk-uint256 v0.5.5 and lukechampine.com/uint128 v1.3.0
in `go.mod`; the offline resolver finds them in the module cache unchanged
(the SDK's go-ethereum requirements are not needed by what is reached). **No
package compiles unmodified as a whole** (each has files importing
`math/big`/`fmt`/`database/sql`/go-ethereum or using floats; every file of a
reachable package must satisfy the subset), so goSVM compiles the example
against trimmed copies generated from the module cache by
`examples/real-libraries/trim` (`go.mod` `replace`; every kept declaration
byte-identical; `trim.json` lists each removal with its check error and every
file's hashes). Fixed in-scope gaps: package `io` in the overlay (for
`io.EOF`), elided `&T{...}` elements of array type (Check rewrites them to
`&T{...}`), read-only pointer parameters (an address passed only to
parameters that read through it keeps a table read-only: uint256's
`z.Lt(&pows[t-20])`), and pointer conversions `(*T)(p)`. 22,126 host vectors
and 1,188 SBF cases on each target match the unmodified libraries natively;
the only difference found is a gc optimization (Go 1.22's zero-copy
`[]byte(s)` gives capacity `len(s)` depending on inlining), documented in
[GO_STRINGS](docs/GO_STRINGS.md#allocation). CU: `ComputeSwapStep`
10.5k–17.8k, `GetTickAtSqrtRatioV2` 8k–14k, `GetSqrtRatioAtTickV2` 2.4k–5.3k,
plus **25.4k CU of package initialization per invocation** for `utils`
(25 `MustFromHex`, `MustFromDecimal`, 3 `MustFromDec`, `Exp`). No corpus C or
full-swap ELF changed (corpus entry 249's message now lists `io`;
announce the Check relaxations and `io` to Qushta). **Open user decisions:**
per-function reachability (would remove the trimming; tradeoffs in the
results) and compile-time evaluation of initializers (the largest CU win; landed the same day, below).
[Results](results/compiler/2026-10-16-real-libraries/README.md). Tests
(`TestRealLibraries*`, `TestRealLibraryForms*`) need the modules in the Go
module cache (`go mod download` in the example) and skip, saying so, without
them.

**Compile-time evaluation of package initialization (October 16, branch
`worktree-agent-afda29cce628856d5`; user-approved):** Check interprets
initialization (`internal/compiler/initeval*.go`) and evaluates the longest
prefix of its steps (each statement of a package's synthetic `init.vars`,
then its `init` functions, in Go's order) that completes without a trap or
panic, an SDK or native-library call, a map keyed by pointers or
interfaces, or exceeding 1,048,576 interpreter steps; that step and every
later one still run on each invocation, so traps, panic lines and later
`init` functions changing evaluated variables behave as before. The
backend installs the resulting heap image at entry (`sol_memcpy_`, then
stores of pointers to read-only data, about 3 CU each). **Design decision:**
the image keeps every object on the heap at the offset the run-time
allocation would have had, so heap accounting is exactly unchanged (moving
never-written objects to read-only data would change heap budgets to save
a few CU); the change is observable only in CU and ELF bytes, and the image
is optional for a backend. Exposed as `CheckedProgram.InitImage` (typed,
backend-neutral objects and values), `PackageInit.EvaluatedVars`/
`EvaluatedFuncs`, `InitSteps()`; `Program.RuntimeInit` turns it off (tests
only); `gosvm features` reports `uses.init` (cost class `image`,
`image+runtime` or `runtime`, the variables each way, the stop reason).
CU (fogo-v0/solana-v3): `utils` initialization 25,442/25,445 → 114/114;
`ComputeSwapStep` transactions 45,063/45,036 → 18,439/18,391 (within the
range), 38,329 → 11,705, 45,708 → 19,084; `global-values` 2,179 → 244.
Evidence: `examples/const-init` (aliasing, maps grown at init, closures,
method values, sentinel errors through `errors.Join`, a pointer-keyed map
ending the prefix at 39 of 42 steps) against fresh native processes, both
ways, on the host (1,022 vectors) and SBF (122 per target); the live heap
after initialization is word-for-word the run-time one (map tables aside)
for five programs; stop-reason, partial-package and SBF edge tests (panic
line after a prefix, 64 KiB heap frame fault). Corpus: only the two empty
`init` programs' C changed. Full-swap ELFs unchanged. Announced in the
ERROR_CONTRACT changelog for Qushta.
[Results](results/compiler/2026-10-16-const-init/README.md).
**Opt-in trimming (October 16, Feature B):** user decision: whole-package
checking stays the default; dependency packages can opt in, by import-path
pattern (`"trim": ["github.com/x/lib/..."]` in `gosvm.json`, repeatable
`-trim` for low-level builds and `gosvm features`), to checking and
compiling only the declarations `Process` reaches
([GO_PACKAGES](docs/GO_PACKAGES.md#trimming), `internal/compiler/trim.go`).
Trimmed packages are type-checked whole (unsupported standard-library
imports from GOROOT source, other modules from the module cache, required
offline even when only dropped code uses them; never compiled); methods are
kept when a reached interface could call them; `init` functions are never
dropped; initializers of unreferenced variables are dropped (Go would run
them) and, like every dropped declaration and type-only import, reported:
build warnings plus `<output>.trim.json`, `gosvm features` `uses.trimmed`,
`CheckedProgram.Trimmed`; the receipt/dependency keys include the
configuration. The entry package is trimmed only when named exactly.
Default builds are unchanged (corpus unchanged, full-swap ELFs identical);
whole-package rejections in module dependency packages now end with a hint
naming the opt-in (ERROR_CONTRACT changelog; announce to Qushta).
`examples/real-libraries` now compiles the four modules **unmodified**
under the opt-in: host differential and 1,188 SBF cases per target match
native Go, call CU identical to the trimmed copies, initialization 27 CU
cheaper (`int256.MaxI256` and other unreferenced initializers dropped);
the trim tool/copies stay for the historical results
([results](results/compiler/2026-10-16-trim-opt-in/README.md)).

**Qushta verified backend (October 14):** Qushta is building an IR backend
(typed Go → IR-SVM → proved SBF, `solana-v3` first) in its own fork; see the
[reply](docs/coordination/2026-10-14-qushta-reply.md). goSVM landed the agreed
interface: `compiler.Check`/`CheckedProgram` and `project.LoadSchema`
([backend interface](docs/BACKEND_INTERFACE.md)), `gosvm features`,
`gosvm test --svm -elf`, and a v1.57 fresh-rebuild check. Error codes, check order
and traps are a [stable contract](docs/ERROR_CONTRACT.md): announce changes to
Qushta before landing them. Their baseline is tag `qushta-phase1-baseline`.

Earlier status: The original session was parked
October 4 at the user's request because of weekly Codex usage resets. A new
director has started the roadmap with the user's authorization. The user's prior
performance objective was the 1.33× Whirlpools CU target, now achieved by the
[1.280853× owner-side snapshot](docs/WHIRLPOOLS_133_TARGET.md). Independent
developer acceptance remains the sole milestone-2 gate; see the
[completion audit](docs/MILESTONE2_ACCEPTANCE.md).
No scheduled task is implied.

## New director resume checklist — October 9

Read this handoff, [ROADMAP.md](ROADMAP.md), then
[target profiles](docs/TARGET_PROFILES.md),
[Bend consumer integration](docs/BEND_LIBRARIES.md) and
[the October 7 acceptance report](results/compiler/2026-10-07-target-profiles-final/README.md).
The report's `metrics.json` and `qualification.json` contain the exact pins and
validation scope. Milestone 1 is closed by user decision; the independent developer
trial remains the sole milestone-2 acceptance gate. The Whirlpools 1.33× objective
was achieved and should not be restarted as unfinished work.

**Working tree:** the October 7 and 9 work is merged to `main` and pushed to
`origin` (October 14). Run `git status --short` before editing. Resume
under the user's current instruction; roadmap entries are not scheduled tasks.

## October 9: Fogo/Solana and Go+Bend+Rust ergonomics

The user's stated exit criteria before moving to core-language work: (1) Fogo and
Solana support where users are minimally aware of ISA/toolchain/runner details;
(2) ergonomic Go+Bend+Rust in one program. They chose: include real-network
validation (throwaway funded keys, approval per deploy); Rust joins as pure ABI v1
libraries with Go as the shell; Bend-side changes go as a request to the Bend
session ([request](docs/coordination/2026-10-09-bend-request.md)).

Implemented on `targets-and-libraries` (root tests/vet pass):

- **Network targets.** `"targets": ["fogo", "solana"]` (new-project default) maps to
  `fogo-v0` / `solana-v3`. Live cluster reads on October 9: Fogo testnet/mainnet
  (`solana-core` 20.0.0) have no SBPF v1–v3 gates; SBPFv3 gate `5cC3foj…` is active
  on Solana mainnet (slot 428,976,000), devnet and testnet. Solana CLI 3.0.15 only
  knows an obsolete v3 feature ID and wrongly shows v3 inactive. `build` and
  `test --svm` cover every target and publish `build/<network>/program.so`.
- **Zero-path setup.** `gosvm toolchain install` downloads the pinned official v1.57
  archive (matches the content pins); runners 0.5.0 (fogo-v0) and 0.6.0 (solana-v3)
  are managed and auto-selected per target. The 0.6.0 package is retained in
  [results/svm/2026-10-09-runner-0.6.0-package](results/svm/2026-10-09-runner-0.6.0-package/README.md);
  there is still no public runner download.
- **Declarative libraries.** `gosvm library add -name X <bend manifests…>` or
  `-rust <crate>` records pins in `gosvm.json` and generates `libs/X` with a
  producer and objects per target. Accepted pairings: Bend fogo-v0/v1.57
  (default; qualified October 14), Bend fogo-v0/v1.53, Bend solana-v3/v1.57,
  GoSVM-built Rust v1.57 ([Rust libraries](docs/RUST_LIBRARIES.md)).
- **Deploy/call.** `gosvm deploy -network fogo-testnet|solana-devnet|…` checks live
  feature gates, then deploys natively through the upgradeable loader (no Solana
  CLI) with a per-network payer in
  `deploy/keys/` (never the CLI keypair — on this host that is a production
  upgrade authority for another project, pointed at Fogo mainnet).
  `gosvm call` simulates/sends with return data, CU and logs.
- **Evidence (local).** [Go+Bend and Go+Bend+Rust qualification](results/compiler/2026-10-09-go-bend-rust-targets/README.md):
  9 cases × 3 runs per fixture per target, with Bend's published solana-v3 bundle
  (`9db669ce…c37ee`) and the accepted fogo-v0 bundle; `scripts/verify_go_bend_rust.py`.
  `gosvm toolchain install -rust` installs the pinned Rust producer toolchain (1.4 GB).

- **Public networks.** [Go+Bend+Rust probe](results/network/2026-10-09-go-bend-rust-probe/README.md)
  deployed natively (no Solana CLI) to Solana devnet (SBPFv3) and Fogo testnet
  (SBPFv0); four committed calls each returned the expected data or error.
  [Redeploy](results/network/2026-10-09-redeploy/README.md) upgrades in place
  (no-op if identical, extends when larger, reclaims leftover buffers); verified
  on both networks. [Upgrade authority](results/network/2026-10-09-upgrade-authority/README.md):
  `gosvm authority` shows/transfers (checked or `-unchecked` address)/removes it,
  and `deploy -authority` upgrades as a non-payer authority; verified on both.

- **Bend fogo-v0 / v1.57 qualified (October 14).** Bend's published bundle
  (`d8493350…66ecd`) with the solana-v3 bundle passes
  [Go+Bend and Go+Bend+Rust qualification](results/compiler/2026-10-14-bend-fogo-v157/README.md)
  on both targets; linked ELFs and per-case CU are identical to the v1.53 run.
  It is no longer `untested` and is the documented fogo-v0 default; v1.53 stays
  accepted and qualified.
- **Bend libraries from source (October 14).** `gosvm library add -name X
  -bend-source PROOF.bend -root ns.fn [-bend-checkout dir | $GOSVM_BEND]` records
  `bend_source` in `gosvm.json`; GoSVM runs Bend's `build:library --json` per
  target into `build/bend-libraries/<name>/<key16>` (Bend's reply:
  `../bendSVM/docs/coordination/2026-10-14-gosvm-reply.md`), then binds
  the manifests through the pinned path. See
  [Build from Bend source](docs/BEND_LIBRARIES.md#build-from-bend-source).
  The mixed fixture rebuilt this way matched the published objects and passed
  `gosvm test --svm` on both targets; the Bend checkout stayed unchanged.

Open:
- Probe programs stay deployed for future network checks: devnet
  `Db91CBr77NWLrQJ83ud2h3nL1AtEYrF1EftwKzznsVpp`, Fogo testnet
  `G1aZmYxscrVjKg7Mw7u4PForusZoWCtn2Ry9N5vFtThW` (authority: the throwaway
  payers in `build/network-probe/deploy/keys`).

**Collaboration contract and ownership:** GoSVM owns its frontend/SDK, profile
selection, generated Go/cgo consumer, external-object linking and Go final-image
qualification. The Bend owner supplies checked pure-library artifacts and the
generated C/Rust interface. Its authoritative documents are
`../bendSVM/docs/library-artifact-contract.md` and
`../bendSVM/tests/mixed-library/fixture.md`; its broader session handoff is
`../bendSVM/docs/HANDOFF.md`. The accepted producer generation is
`../bendSVM/build/interop/fogo-v0/20261007T134003Z-0bdd2144/library.manifest.json`,
SHA-256 `ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de`
(rechecked October 9). ABI v1 uses canonical bytes, fixed-width integers,
status codes and a fingerprinted C symbol. The producer is explicitly v1.53 /
fogo-v0; Go's caller is v1.57 / fogo-v0. Do not relabel the producer pin or treat
`finalElfAuthorized:false` as final-image authorization.

**What the collaboration has established:** Bend reported native ABI checks,
15 Rust/Bend SBF cases, target-mismatch refusal and recursive-call refusal for
the published generation. GoSVM separately validated native cgo calls and the
actual linked Go/Bend image's nine scenarios × three, including malformed input
and transaction rollback. These are separate Rust/Bend and Go/Bend consumers of
the same ABI. A single final program combining Go, Bend and Rust, arbitrary
Rust-library imports, and formal verification of the Go shell/compiler have not
been established by this acceptance. Current GoSVM consumer support is the
ordinary pure-library contract, not Bend's distinct qualified-SHA package contract.

**Remaining coordination:** at the accepted checkpoint Bend solana-v3 objects
were unavailable and GoSVM correctly refused them. Before consuming new outputs,
obtain the Bend owner's explicit target/toolchain/artifact manifest and qualify
the changed mixed final image in a fresh results directory. The same application
already passes local runtime tests on both GoSVM profiles; actual Fogo/Solana
network validation remains unperformed. A newer sibling handoff or producer
artifact is new evidence, not an implicit update to the accepted pins here.

**Local replay:** retained executables are `build/target-gosvm`,
`build/modern-runtime-runner` and
`results/compiler/2026-10-06-phoenix-dependencies-final/return-data-runner`.
They are untracked/rebuildable; large temporary Go/Cargo caches were removed.
Use `scripts/build-cli.sh`, `scripts/build_modern_runner.py` and the acceptance
report's `scripts/verify_target_profiles.py` command to rebuild/rerun. Supply a
new results directory and both explicit runner paths; keep historical evidence
unchanged. Native tests of separate example/service modules remain separate from
root `make test`.


October 7: [explicit target profiles](docs/TARGET_PROFILES.md) now support
`fogo-v0` and modern `solana-v3` through project build/test and low-level CLI,
with content-pinned platform-tools v1.57 on macOS arm64. Existing target-less
configurations retain exact v1.51 experimental-v3 semantics; `migrate-target`
changes configuration explicitly while source, SDK, layouts and generated clients
remain byte-identical. Outputs/receipts separate profile, toolchain/backend and
frontend/SDK/imported-package/foreign-artifact dependencies. The historical runner
is unchanged; a separate LiteSVM 0.13.0 / Agave 4.0 / SBPF 0.14.4 runner is rebuilt
from pinned source with the equivalent rent-error precedence patch. The same
54 scenarios / 66 transaction steps pass on both profiles × three, including
Clock, current Rent, token/PDA CPI, lifecycle failures and rollback.
[Pure Bend-library ABI v1](docs/BEND_LIBRARIES.md) consumer support validates the
producer manifest/interface/target/artifact pins, generates cgo bindings using the
selected native header/object and links captured SBF objects. The published
v1.53 fogo-v0 producer with manifest SHA `ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de`
passes native ABI boundaries and the actual v1.57 Go/Bend image's nine scenarios
× three, including malformed input and transaction rollback. V3 library use is
refused because Bend has not published v3 objects. Root tests/vet, native service,
modern runner unit tests and mutation/migration/backend checks pass. Full evidence
and reproduction are in [the new acceptance report](results/compiler/2026-10-07-target-profiles-final/README.md).
These are local runtime tests, not Fogo/Solana network validation, broad platform
qualification or a reinterpretation of historical benchmark results.

October 6: the user requested [artifact untracking](results/maintenance/2026-10-06-artifact-untracking/README.md).
Generated archives/binaries, dumps, logs, large fixtures and raw execution
reports remain unchanged locally, with checksums in the new inventory. Compact
CU/error observations are retained alongside source, small fixtures and existing
summaries. Old report links may require restoring local artifacts; historical
Git blobs are preserved. The user subsequently applied the prepared cleanup bundle; tracking changes are
now reflected in this checkout. Historical Git blobs remain preserved.

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

1. [Root README](README.md): public introduction and quickstart.
   [Development reference](docs/DEVELOPMENT.md): detailed setup, subset,
   architecture, reproduction, and historical measurements.
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
