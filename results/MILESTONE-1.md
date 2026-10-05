Historical first-milestone report; see the repository README for the current decision.

# goSVM

An experiment in compiling a restricted Go subset to Solana SBF. The first
milestone works: Go source compiles to an ELF that executes on a local Agave
validator, mutates persistent account state, and has compute-unit use close to a
minimal Rust equivalent.

**Decision: go for another experiment, not yet a supported Go toolchain.** The
current workload is bounded constant-product swap arithmetic. It has no token
balances, custody, authentication, liquidity operations, or CPI. It is deliberately
not a deployable AMM.

## How it works

```text
Go source → standard Go parser/type checker → checked C → Solana Clang → LLD → SBF ELF
```

The frontend is written in Go and has no external modules. Contract builds use a
prebuilt `gosvm` executable and two backend executables; they do not invoke Cargo
or link a Go runtime. Native `go test` can test the same contract source.

This is a small Go-to-C compiler, not a new backend for the upstream Go compiler.
Using C gives this experiment LLVM optimization without LLVM bindings or a custom
TinyGo fork. TinyGo remains a possible route if broader language support becomes
more important; it has not been benchmarked here.

## Reproduce

Requirements for the full experiment:

- Go 1.22 or later, host Clang for differential tests, Python 3.10+ for benchmarks.
- Solana CLI/test-validator and `cargo-build-sbf` (tested with 3.0.15).
- Solana platform-tools **v1.51**, already installed under
  `~/.cache/solana/v1.51/platform-tools`. Override with `SBF_TOOLS`; the supplied
  scripts and linker layout are pinned to this toolchain, not arbitrary versions.

If platform-tools are missing, `cargo-build-sbf --tools-version v1.51 --install-only`
installs them. Downloads/install time are excluded from the build measurements.

```sh
make test       # Native Go properties and Go-vs-generated-C differential tests
make build      # Build the frontend and matching Go/Rust v3 ELFs
make verify     # Isolated local validator: execution, CU, persistence, rollback
make bench      # Serial timing samples and disk measurements; about a minute
```

The verification harness uses only the Go standard library, starts its own local
validator, and stops it and removes its temporary ledger afterwards. It does not
access a public cluster or use your wallet. Its deterministic key is a local test
fixture. The macOS wrapper works around Go 1.22's incompatibility with macOS 26's
Mach-O loader by externally linking and ad-hoc signing the harness.

Once the frontend is built, the ordinary Go contract build is:

```sh
build/gosvm -o build/amm-go.so examples/amm/amm.go
```

`-llvm /path/to/platform-tools/llvm` selects the backend. `-emit-c -o build/amm.c`
lets you inspect the intermediate representation. `-arch v3` is the default;
`SBF_ARCH=v0 make verify` repeats the older target experiment with the validator's
v0-disable test feature deactivated. The v3 test uses the validator's default
features. There is no mainnet deployment or cluster feature-compatibility claim.

## Workload and correctness

`examples/amm/amm.go` implements an X-to-Y constant-product swap with a 30bps fee:

```text
effective = amountIn × 9970
amountOut = reserveY × effective / (reserveX × 10000 + effective)
```

State is three little-endian uint64s: reserve X, reserve Y, swap count. Instruction
data is two little-endian uint64s: amount in and minimum amount out. Reserves are
capped at 1,000,000,000 and input at 1,000,000, so intermediate products fit in
uint64. These are benchmark constraints, not sufficient arithmetic for a general
token AMM.

The experiment's fixed ABI adapter accepts exactly one writable, non-executable,
program-owned 24-byte account and a 16-byte instruction. It checks ownership and
lengths before calling `Process`. All input-dependent validation happens before
state writes. The ABI adapter is intentionally specific to this workload; the
frontend is more general than the adapter.

Validation includes:

- 10,000 native swaps checked against independent `math/big` arithmetic and the
  non-decreasing constant-product invariant.
- 2,000 generated-C/native-Go differential cases, including invalid ranges.
- Compiler rejection tests plus unsigned wraparound, large shifts, zero values,
  short-circuit evaluation, function evaluation order, and lexical scopes.
- 81 vectors per SBF backend: successful swaps, deterministic random inputs,
  slippage, limits, overflow, malformed lengths, ownership, write permissions,
  missing accounts, and duplicate account references.
- Three additional SBF checks proving valid indexing works and bounds/division
  violations invoke the abort syscall.
- Two committed swaps per backend and a failed two-instruction transaction per
  backend proving the earlier instruction's state writes roll back.

Both ELF target versions are checked before comparison; SHA-256 hashes are stored
in the verification report. Rust uses the same account checks and formula with
`no_std`, no SDK dependencies, optimization level 2, and LTO. This gives a lean
Rust comparison rather than an Anchor/dependency-heavy baseline.

## Supported source subset

One import-free Go source file, with the entry function:

```go
func Process(state []byte, instruction []byte) uint64
```

The current frontend accepts `uint8`/`byte`, `uint32`, `uint64`, `bool`, byte slices,
constants, local zero-initialized variables, direct functions with at most one
result, indexing, `len`, unsigned arithmetic/conversions, comparisons, `if`,
three-clause/conditional `for`, and unlabeled `break`. Assignments are single
`=`/`:=` operations; loop counters support `++`/`--`.

Unsupported constructs are rejected. They include imports, structs, named types,
arrays, pointers, signed variables/arithmetic, globals, `init`, allocation,
`append`, slicing, interfaces, maps, strings, generics, closures, recursion,
multiple results/assignment, `range`, `continue`, `switch`, `defer`, goroutines,
and channels. Folded numeric constants follow Go's type checker.

The emitter preserves unsigned wraparound, checks division and indexing, handles
oversized shifts, and sequences expression evaluation. A panic becomes a VM abort;
there is no recovery, garbage collector, scheduler, or dynamically growing stack.
This frontend is a proof of concept, not a complete or audited Go implementation.

## Measurements and remaining gates

2026-10-04, Apple M2, macOS 26.6.2, Go 1.22.0, Solana platform-tools v1.51.
Median of seven builds per scenario; both SBF programs target v3:

| Metric | Go → SBF | Minimal Rust → SBF | Native Go reference |
| --- | ---: | ---: | ---: |
| Project-clean build | 169 ms | 760 ms | 550 ms |
| Source-edit build | 158 ms | 605 ms | 368 ms |
| No-op build | 151 ms | 136 ms | 301 ms |
| Program file | 1,824 bytes | 1,704 bytes | 1,366,450 bytes |
| Representative swap | 351 CU | 338 CU | — |

The swap is **3.8% more CU** than Rust. Go contract edit builds are about **3.8×
faster** in this small workload. No-op Rust wins slightly; the Go frontend does
not cache builds yet. The native executable includes the Go runtime and is not a
like-for-like size comparison. These are small local samples on a working machine,
not isolated hardware benchmarks.

The stripped frontend is 3.1 MiB. Clang plus LLD add 167.9 MiB; a contract build
was verified with just those two executables in an isolated backend directory.
That suggests a roughly **171 MiB contract-building distribution** without Cargo,
Rust, or the Go SDK. Packaging/downloading that distribution is not implemented.
The currently installed full Solana platform-tools occupy about 1.36 GiB; the
installed Go SDK occupies about 211 MiB, both measured as logical file sizes.
The validator and test tools are additional and excluded from that distribution.

One-time frontend compilation with a fresh Go build cache took 17.4 seconds and
created 46.2 MiB of cache. Ordinary contract builds leave only the ELF; generated
C and object files are temporary and removed. The minimal Rust benchmark leaves
about 11 KiB in its target/deployment directories, so **large project artifact
savings over minimal Rust are not established**. The useful disk difference here
is the build toolchain, not this dependency-free Rust project's artifacts.

Raw, saved results from this machine are in [benchmark.json](benchmark.json)
and [verification.json](verification.json); rerunning the commands
writes fresh reports to `build/benchmark.json` and `build/verification.json`.
Build samples include process startup, parsing, type checking, compilation, and
linking. Go SBF timings use the already-built frontend, as an installed compiler
would. Toolchains and operating-system caches are warm. Rust project-clean builds
delete the target/output directories; native Go project-clean builds force a new
package build while retaining the standard-library cache. Source edits change a
comment, preserving the workload while invalidating source caches. No-op Go SBF
builds currently recompile everything.

The native Go comparison compiles the same arithmetic source with a tiny native
driver. Its runtime/linking requirements differ from SBF, so it is a build-latency
reference, not a runtime-performance comparison. A separately measured fresh Go
cache accounts for the one-time cost of building the frontend itself.

The next gates are multi-account decoding and CPI (including token transfers and
PDA signing), realistic wide-integer arithmetic, and measuring the same metrics
on a substantially larger program. Named structs, reusable packages, usable
diagnostics, and broader semantic testing are prerequisites for a useful supported
toolchain. The current speed and size do not establish how that larger system will
behave.

## Implementation references

- [Solana program entrypoint ABI](https://solana.com/docs/programs/rust/program-structure).
- [Solana C account deserializer](https://github.com/solana-labs/solana/blob/master/sdk/sbf/c/inc/sol/deserialize.h),
  also checked against the installed 3.0.15 SDK.
- [TinyGo compiler](https://github.com/tinygo-org/tinygo), an alternative frontend
  candidate, not used by this experiment.
- [Golana](https://github.com/oxfeeefeee/golana), prior Go/Goscript-on-Solana work,
  not used by this experiment.

The v3 linker memory layout matches the installed v1.51 Rust target specification:
`rustc -Z unstable-options --print target-spec-json --target sbpfv3-solana-solana`.
SBF versions and toolchain versions must be matched explicitly. During exploration,
changing targets while sharing Cargo's deployment output left a stale ELF; the
scripts now use target-specific deployment directories and the harness rejects
mismatched ELF versions.
