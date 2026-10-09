# Fogo and Solana targets

Projects name networks; GoSVM maps each to a profile that selects the
instruction set, ELF format, linker layout, pinned backend and local test
runtime. Application Go source, wire layouts, SDK API, IDL and generated
clients remain identical across targets.

```json
"targets": ["fogo", "solana"]
```

| Network | Profile in this compiler | Why |
| --- | --- | --- |
| `fogo` | `fogo-v0` | Fogo testnet/mainnet (`solana-core` 20.0.0) have no SBPF v1–v3 feature gates |
| `solana` | `solana-v3` | SBPFv3 deployment (`5cC3foj…`) is active on mainnet (slot 428,976,000), devnet and testnet |

Cluster state was read on October 9, 2026. A later compiler may remap a network
name when its cluster changes (for example when SIMD-0500 disables v0–v2
deployment); outputs, reports and deploy records always name the resolved profile,
so an old artifact is never relabelled. `gosvm deploy` rechecks the live gates.

| Profile | Backend | ISA / ELF | Purpose |
| --- | --- | --- | --- |
| `fogo-v0` | platform-tools v1.57 / LLVM 22.1.2 | v0, machine 263, flags 0 | Current Fogo compatibility target |
| `solana-v3` | platform-tools v1.57 / LLVM 22.1.2 | modern v3, machine 247, flags 3 | Modern Solana compatibility target |
| `legacy-v1.51-v3` | platform-tools v1.51 / LLVM 19.1.7 | experimental v3, machine 263, flags 3 | Preserve historical GoSVM experiments |

A shared “v3” name or flags value does not make the historical and modern formats
interchangeable. Modern v3 puts readonly data at zero and executable code at
`0x100000000`; the historical linker script remains frozen. V0 retains dynamic
syscall resolution; modern v3 uses static syscall identifiers. Both modern
profiles link the freestanding multiplication helper when the backend emits it.

## Install and select

The v1.57 backend is qualified locally on **macOS arm64** with exact Clang/LLD
content pins in `internal/targets/profile.go`. This qualifies the two compiler
executables for the recorded corpus, not every component of platform-tools,
all host platforms or an entire network release. Version text alone is insufficient.

`gosvm toolchain install` downloads the official platform-tools v1.57 macOS arm64
archive (430,747,800 bytes, SHA-256 `48c32c2e…bc932b34a`, both pinned in the
frontend), verifies it before decompression, keeps only Clang and LLD, and refuses
to install unless they equal the content pins. One install serves `fogo-v0` and
`solana-v3`. Alternatives use the same pins: `-archive <local copy>` or
`-llvm <existing platform-tools v1.57 llvm dir>` (receipts record
`official-archive` or `qualified-local-backend`). `gosvm doctor` reports backend
and runner state for both profiles with the exact fix command.

[Rust libraries](RUST_LIBRARIES.md) need the complete platform-tools tree
(rustc, cargo, their SBF sysroots, LLVM utilities). `gosvm toolchain install -rust`
extracts it from the same pinned archive (one download also installs the
Clang/LLD backend if missing) into a separate managed root,
`<cache>/toolchains/v1.57-full/darwin-arm64` (about 1.4 GB). Every directory,
file and symlink is kept; paths and links must stay inside the tree. The install
is staged, published by rename with a receipt, and refused unless Clang, LLD,
rustc, cargo, the rustc driver, llvm-objcopy and llvm-ar equal their content
pins. It is optional; plain `gosvm toolchain install` still keeps only Clang and
LLD. `status` and `doctor` report it.

```sh
gosvm toolchain install          # or -archive file.tar.bz2, or -llvm dir
gosvm toolchain install -rust    # optional: full tree for Rust libraries
gosvm toolchain status
gosvm doctor

gosvm new my-program             # targets fogo and solana
cd my-program
gosvm check
gosvm test                       # native Go tests (once)
gosvm build                      # every target
gosvm test --svm                 # every target, each with its matching runner
gosvm build -target solana       # one target for this command
```

New projects target both networks (`-target fogo` or `-target solana` creates a
single-target project). `-target` on build/test/check/generate/doctor selects one
profile for that invocation without changing `gosvm.json` or application files;
it accepts network names or full profile names. A failure on one target is
reported and the remaining targets still run. `SBF_LLVM` or an explicit `-llvm`
can select a backend directory, but modern builds still require its content pin.
`GOSVM_CACHE` selects the managed cache root. Other archives and host binaries
are refused; the historical v1.51 installer is unchanged and remains available
through `-target legacy-v1.51-v3`.

Each build publishes `build/fogo/program.so` and `build/solana/program.so`, copied
from the isolated outputs below.

Normal modern outputs are:

```
build/<profile>/<platform-tools>/<backend-content>/deps-<dependency-content>/program.so
```

The dependency namespace includes the frontend executable, SDK, imported Go
sources, module metadata, and every selected Bend manifest/artifact. The receipt
also includes application source and output hashes. Application edits invalidate
that receipt within the same namespace. Changed backend or dependencies retain
separate outputs. Historical projects retain `build/program.so` and their original
report paths. Generated build artifacts remain untracked and can be rebuilt.
Low-level `gosvm -target <profile> -o <explicit-output> source.go|directory` is also
available; callers own output isolation there. Raw `-arch v0|v3` keeps its old meaning.

## Explicit migration

Existing configurations without `target` remain exactly v1.51 experimental v3.
They never become modern Solana builds implicitly. Conflicting target/backend/ISA
fields are rejected. Change configuration explicitly:

```sh
gosvm migrate-target -target fogo,solana -dir my-existing-project
# Or a single network/profile; only gosvm.json changes.
```

A `targets` list records no single ISA: it requires `platform_tools` v1.57 and no
`target`/`sbf` fields.

Migration preserves source, SDK snapshot, generated Go, account/argument layouts,
client interfaces and IDL. A schema-1 and schema-2 regression checks those bytes
through all three profiles. Historical results, compiler pins and backend scripts
under `results/` remain historical evidence; rebuild into a new directory for a
changed profile. Benchmark ratios measured with experimental v1.51 v3 do not
become modern-v3 or Fogo measurements.

## Local runtime qualification

Both runners are frontend-pinned packages with separate managed cache slots.
`gosvm runner install -archive <package>` recognizes which one it is; `gosvm runner
status` reports both. No public runner download exists. The 0.6.0 package is built
by `scripts/package_modern_runner.py` (the 0.5.0 recipe is unchanged).

```sh
gosvm runner install -archive gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz
gosvm runner install -archive gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz
gosvm test -target solana-v3 --svm     # selects managed 0.6.0
gosvm test -target fogo-v0 --svm       # selects managed 0.5.0
# General fixture paths are relative to the caller's working directory.
gosvm test -dir examples/full-swap -target solana-v3 --svm \
  -svm-fixtures examples/full-swap/testdata/lifecycle.json
```

Selection order: `-svm-runner`, `GOSVM_TEST_RUNNER`, the verified managed runner
for the target, then `gosvm-svm-runner` on `PATH`. `solana-v3` uses 0.6.0;
`fogo-v0` and legacy use 0.5.0. `fogo-v0` falls back to managed 0.6.0 only when
`GOSVM_TEST_FEATURES` is set. A damaged managed runner fails closed.

The historical runner uses LiteSVM 0.8.2 and the observed local validator 3.0.15
feature list. The separate [modern runner](../benchmarks/svm-runner-modern/README.md)
uses LiteSVM 0.13.0 / Agave 4.0 / SBPF 0.14.4 and its all-enabled feature set.
Both have an isolated rent-error precedence patch. The new runner neither replaces
nor relabels historical packages. `GOSVM_TEST_FEATURES` overrides defaults explicitly;
feature IDs must exist in the selected engine. A modern runner used for v0 needs
an explicit feature file compatible with that engine.

Reports record target, backend, actual runner identity/hashes and
`network_validation: false`. Different runtime feature sets and builtin versions
prevent treating cross-profile CU as a matched performance comparison. The old
`--sbf` validator workflow remains pinned to local validator 3.0.15; it does not
qualify a modern Solana validator or a Fogo network.

[Local acceptance evidence](../results/compiler/2026-10-07-target-profiles-final/README.md)
covers the same token swap, managed lifecycle, Clock controls and failures/rollback
on both targets, plus the actual v0 Go/Bend image. **No Fogo or Solana public-network
deployment or transaction acceptance has been established by this work.**

## Deploy and call

```sh
gosvm deploy -network solana-devnet -check   # build, then read the cluster's feature gates
gosvm deploy -network solana-devnet          # deploy (fund the printed payer first)
gosvm call -network solana-devnet -data <hex>        # simulate; -send commits
```

Networks: `fogo-testnet`, `fogo-mainnet`, `solana-devnet`, `solana-testnet`,
`solana-mainnet` (mainnets also require `-mainnet`). Deploy builds the network's
target, refuses a cluster whose live feature accounts reject that format, then runs
`solana program deploy` with `--skip-feature-verify` (older CLIs do not know current
gates; GoSVM performed the check). The fee payer/upgrade authority is
`deploy/keys/<network>-payer.json` and the program address keypair is
`deploy/keys/<network>-program.json`; both are created on first use in a Git-ignored
directory. GoSVM never falls back to the Solana CLI's configured keypair, which may
belong to an unrelated production program. `deploy/<network>.json` records program
ID, authority, ELF hash, profile and cluster genesis hash. `gosvm call` passes the
payer as the instruction's only (signer) account and prints error, return data,
compute units and logs.
