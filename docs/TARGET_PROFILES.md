# Explicit SVM target profiles

GoSVM selects the instruction set, ELF format, linker layout and pinned backend
through an explicit project target. Application Go source, wire layouts, SDK
API, IDL and generated clients remain identical across targets.

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

Install official platform-tools v1.57 separately, or use an existing installation.
Then import only the two qualified executables:

```sh
gosvm toolchain install -target solana-v3 \
  -llvm "$HOME/.cache/solana/v1.57/platform-tools/llvm"
gosvm toolchain status -target fogo-v0

gosvm new -target fogo-v0 my-program
cd my-program
gosvm check
gosvm test
gosvm build
gosvm build -target solana-v3
```

New projects default to `solana-v3`. `-target` on build/test/check/generate/doctor
selects a profile for that invocation without changing `gosvm.json` or application
files. `SBF_LLVM` or an explicit `-llvm` can select a backend directory, but modern
builds still require its content pin. `GOSVM_CACHE` selects the managed cache root.
The modern importer refuses unqualified archive downloads and other host binaries;
the historical installer remains available through `-target legacy-v1.51-v3`.

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
gosvm migrate-target -target fogo-v0 -dir my-existing-project
# Or select solana-v3; only gosvm.json changes.
```

Migration preserves source, SDK snapshot, generated Go, account/argument layouts,
client interfaces and IDL. A schema-1 and schema-2 regression checks those bytes
through all three profiles. Historical results, compiler pins and backend scripts
under `results/` remain historical evidence; rebuild into a new directory for a
changed profile. Benchmark ratios measured with experimental v1.51 v3 do not
become modern-v3 or Fogo measurements.

## Local runtime qualification

Use a compatible explicit runner:

```sh
gosvm test -target fogo-v0 --svm -svm-runner /path/to/historical-0.5-runner
gosvm test -target solana-v3 --svm -svm-runner /path/to/modern-0.6-runner
# General fixture paths are relative to the caller's working directory.
gosvm test -dir examples/full-swap -target solana-v3 --svm \
  -svm-fixtures examples/full-swap/testdata/lifecycle.json \
  -svm-runner /path/to/modern-0.6-runner
```

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
