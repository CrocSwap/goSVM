# Dependency-heavy protocol benchmark

This experiment models Basanos's **build shape**, not its protocol or system
requirements. It combines a stateful job lifecycle, runtime hashing, typed wire
decoding, a live dispatch catalog, the normal Solana Rust SDK, and a large native
VM-test graph. Basanos source, targets, deployed programs and worktrees are untouched.

## Reproduce

Use the root README's Go/Solana tools and platform-tools **v1.51**. Native Rust
uses the installed host toolchain (recorded in the report), independent of SBF's
Rust 1.84.1-dev. Run from the repository root:

```sh
make bench-protocol
```

`protocol-prepare` generates source/fixtures and fetches both locked dependency
graphs **before** timing. Then the benchmark builds programs, builds/runs the Go
tests and external validator, and builds/runs ProgramTest. This can take many
minutes and several GiB. Its output directories are exclusively under
`build/protocol/`; each cold phase replaces its own prior benchmark target.

Stages can also be run independently:

```sh
python3 scripts/protocol_bench.py --stage contracts
python3 scripts/protocol_bench.py --stage go-harness
python3 scripts/protocol_bench.py --stage host
make verify-protocol
```

No public cluster, wallet or deployment is involved. Download/install time is
excluded. Rust wrappers and ambient flags are removed, Cargo uses four build jobs,
and native Go uses four build workers. Compiler toolchains and downloaded sources
are installed; cold means empty project targets, not a freshly installed machine.
The Go harness additionally gets a fresh, dedicated standard-library build cache.
The small frontend is prebuilt before contract timing; its preparation is recorded
separately.

## Workload

Four accounts carry operator authorization, configuration, a 256-byte job state,
and a 2,048-byte payload. A 72-byte command drives commit, execute-batch, checkpoint,
finalize and reset. The program checks ownership, signer permissions, distinct
accounts, dimensions, nonces, cursor bounds, budgets, phase transitions and
overflow. SHA-256 commits the payload and checkpoints state. Each record selects
one of 64 specialized unsigned integer kernels; every kernel affects output and
is exercised by the tests. These kernels are synthetic arithmetic, not a model
executor or a claimed Basanos substitute.

Go implements the lifecycle in its supported subset; its SDK SHA-256 operation
marshals the real runtime syscall. Rust uses `solana-program = 2.3.0`, Borsh-derived
command decoding and the SDK hash API, which calls the same syscall on SBF. The
programs share a wire format and required checks, not an implementation.

Python independently generates **116 cases**, including 64 single-kernel cases,
the complete 20-instruction lifecycle, malformed commands/accounts, authority
substitution, invalid commitments, credit exhaustion and overflow. Python's
arithmetic and `hashlib` determine expected state. Native Go checks the cases;
native Rust executes its actual processor as part of the integration suites.
Both SBF images are run in the external validator. ProgramTest separately runs
the native Rust processor through bank transactions, including the complete
lifecycle and atomic rollback. The external suite commits both SBF lifecycles
and verifies atomic rollback after a successful commit followed by a failing
instruction.

## Comparison boundaries

There are three distinct costs:

| Path | Contract build | VM tests |
| --- | --- | --- |
| Go | Restricted frontend, Clang and LLD | Go driver, prebuilt validator |
| Rust with external harness | Solana SDK/Borsh dependency build | The same Go driver and validator |
| Rust with ProgramTest | The same Rust SBF build | Host Rust application plus ProgramTest and 12 integration executables |

The separate host crate has **669 resolved packages**, including the application;
the SBF graph has **184**. Resolved package count includes target-conditional
packages and is not a count of compiled crates. Cargo timing reports identify
actual compilation units. The original Basanos observation was 673 locked
packages, including its tests. This is an ecosystem comparison, not 669 arbitrary
dependencies added to make Rust look slow.

The host graph also compiles vendored **OpenSSL C code**: `agave-precompiles`
2.3.9 enables `solana-secp256r1-program/openssl-vendored`. This is a transitive
runtime choice, not a feature requested by the synthetic application. The build
was observed waiting on that build script and its `make`/Clang children after
most Rust crates completed. Cargo's timing report includes that build-script
span. Host cold-build time must not be described as time spent solely compiling
Rust source.

Twelve independent integration binaries partition the reference cases and use
shared helpers. Only one group adds the full lifecycle/rollback sequence. This
models repeated linking in a real multi-file Rust integration suite without
copying Basanos's dozens of suites. The native default **debug** profile is used;
release LTO is not imposed on tests to inflate costs. The SBF crate retains the
common `cdylib`/`lib` setup and release profile. Its builder warns that the dual
crate type precludes LTO; the warning is retained in the logs.

SBF and host crates deliberately have separate locks. The initial combined graph
hit Cargo 1.84's inability to parse a host-only edition-2024 dependency. Separating
them is a sensible Rust layout and removes that incidental obstacle while keeping
the host VM's real dependency cost. No Basanos build caches are reused.

ProgramTest is **2.3.9**, running the **native Rust processor**; the external
validator is **3.0.15**, running both actual SBFv3 ELF images. All use the same
reference cases. CU comparisons come exclusively from the shared external
validator. Test execution wall time is not a like-for-like VM speed comparison: native
processor execution, real-bank confirmation, startup and shutdown costs differ.

An initial attempt to load SBFv3 in the older ProgramTest exposed a toolchain
compatibility limit. Its `solana-sbpf` 0.11.1 strict parser expects five program
headers, including `PT_GNU_STACK` and `PT_NULL`; platform-tools v1.51 emits four
`PT_LOAD` headers. Both Go and Rust images use the newer layout. The final harness
explicitly chooses native processor mode and makes no claim to validate SBF in
ProgramTest. The external validator verifies that separately. The cold build
completed before this small harness correction; the repair/relink is timed
separately, with the identical dependency graph and 12 executables retained. The
initial failure is archived. A fresh reproduction directly uses the corrected
native harness.

## Results

Measured on an 8-core M2 Mac with 24 GiB RAM, under substantial unrelated load.
These are project-cold builds with installed compilers and downloaded dependencies.
Final Go results use the optimized byte helpers described below.

| SBF program | Go | Rust SDK/Borsh |
| --- | ---: | ---: |
| Cold build, wall time | 2.19 s | 6 min 52 s |
| Cold build, child CPU time | 0.63 s | 222.01 s |
| Cached source edit, median of 3 | 0.99 s | 6.56 s |
| No-op build, median of 3 | 0.039 s | 2.22 s |
| Deployable ELF | 39,568 bytes (38.6 KiB) | 53,912 bytes (52.6 KiB) |
| Retained SBF build output | ELF plus a small cache receipt | 216.8 MiB target, plus deployment output |

The Go frontend's own build is separate: 2.93 s with the installed Go cache.
Rust's SBF cold build compiled 171 Cargo units. Both languages use the installed
Solana LLVM toolchain; Go does not obtain these results from the native Go backend.

| Host test workflow | Go native tests + shared external driver | Rust native ProgramTest, 12 binaries |
| --- | ---: | ---: |
| Cold compilation | 34.43 s driver + 6.85 s native test binary | 35 min 35 s |
| Cold compilation, child CPU | 36.42 s + 8.83 s | 1,450.83 s |
| Native test source edit, median of 3 | 0.91 s | 54.59 s |
| No-op test build, median of 3 | 0.52 s | 5.97 s |
| Retained cache/target after the run | 83.5 MiB cache + 9.1 MiB binaries | 4.70 GiB target |

These host columns provide different test infrastructure. Go's native test binary
does not embed a VM; its external driver uses an installed validator. Rust can
use that **same external driver**, avoiding the entire ProgramTest target. This
table measures the cost of the chosen workflows, not equivalent language runtimes.

The Rust host target was **3.83 GiB immediately after its cold build**. The harness
correction and three source edits brought it to **4.70 GiB**, including 610.5 MiB
of incremental artifacts. The final target's `deps` subtree holds 3.96 GiB. The
shape has reproduced both the long cold build and gigabyte-scale artifacts without
copying a large production protocol.

The host cold build compiled **749 Cargo units**. The longest unit was vendored
OpenSSL's build script at **966 s**; the RPC client took 325 s and the accounts
database took 162 s. These spans overlap and must not be added together. The
OpenSSL discovery reinforces that this is dependency/workflow cost, not simply
slow compilation of our own Rust code.

Native Go tests passed, all 12 native ProgramTest partitions passed, and the
external validator passed **232 simulations, 40 committed lifecycle instructions
and two atomic rollback checks** across the two SBF programs. Native ProgramTest
execution was 77.69 s; the shared external verification was 82.54 s. Those times
include different startup, transaction and shutdown behavior and are not VM
throughput comparisons.

### Compute cost and the Go optimization

The first Go version used byte-at-a-time equality/copy/clear loops. It compiled
quickly but cost **105,400 CU** for the full lifecycle, versus Rust's **67,295 CU**
(56.6% more). Replacing those private helpers with four word operations in Go
reduced the lifecycle to **68,702 CU**, just **2.1% more than Rust**. Account
checks, protocol behavior, the compiler and the runtime syscall boundary stayed
the same. Native helper tests check every mismatch byte and unaligned regions;
the complete independent state/error reference and validator suite pass again.

| Instruction | Optimized Go CU | Rust CU |
| --- | ---: | ---: |
| Commit | 2,450 | 2,374 |
| First 16-record batch | 3,874 | 3,811 |
| Checkpoint | 1,262 | 1,358 |
| Finalize | 1,332 | 1,363 |
| Reset | 1,369 | 1,223 |
| Complete 20-instruction lifecycle | **68,702** | **67,295** |

Across the 84 successful vectors, paired Go CU differences range from **7.1%
lower to 11.9% higher**, with a median **2.4% lower**. The complete lifecycle is
the more useful weighted comparison for this workload. The Go ELF grew from
35,568 to 39,568 bytes during this optimization and remains 26.6% smaller than
Rust's ELF. This is a workload-level Go change; no protocol logic moved into C.

**Assessment:** this is a strong reason to continue the experiment. The narrow
Go pipeline avoids much of the build graph that caused the motivating pain,
while a modest Go-level adjustment achieves close CU efficiency. It remains an
experiment: this workload models dependency/test structure, not Basanos's source
scale or tight system requirements. The earlier source-scaling results still
apply, and the compiler still supports only the documented Go subset.

Saved evidence: [benchmark](../../results/protocol/benchmark.json),
[SBF verification](../../results/protocol/verification.json),
[CU summary](../../results/protocol/summary.json), and
[source/tool manifest](../../results/protocol/source-manifest.json).
The saved directory also includes Cargo timing HTML, logs, the unoptimized Go
body and before/after reports. Run `python3 scripts/protocol_save.py` after a
successful reproduction to refresh this separate evidence snapshot.

Raw reports distinguish cold builds, three source edits, three no-op builds,
child-process CPU times, exit codes, file counts, logical bytes and allocated
blocks. Source edits change a comment while forcing the affected crate/package
to rebuild. Integration compilation is measured with `--no-run`; executing tests
is a separate phase. Cache growth after edits is recorded separately from the
first cold build. Installed compilers, registry downloads and temporary validator
ledgers are not counted as project targets.
The compiler's temporary C/object/linker files are removed after each build;
reported disk sizes describe retained artifacts, not peak temporary storage.
The installed validator and tool binaries have separate sizes and hashes in the
source manifest, and can be shared across projects by either language.

The desktop was heavily loaded during this run. A saved observation during SBF
compilation reported a one-minute load average over 70. Treat wall-clock numbers
as observations on this machine, not portable speedup promises. CPU work and disk
counts help distinguish dependency work from scheduling noise. One cold sample
per final configuration is retained, rather than presenting cold-build medians.

This can demonstrate the advantage of a narrow SDK and avoiding per-project VM
compilation. It cannot establish that changing Rust syntax to Go alone produces
that advantage. Rust programs can use the external harness too. Likewise, the
current Go subset has no reusable user packages, general structs, allocator or
full standard library; adding those capabilities may change the cost model.

Baseline source and tool provenance are saved alongside results. Earlier small
AMM/token-swap/scaling evidence under the root `results/` remains historical and
is not overwritten by this experiment.
