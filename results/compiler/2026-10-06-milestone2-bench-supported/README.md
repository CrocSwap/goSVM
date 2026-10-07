# Milestone 2 framework/application measurements

October 6, 2026, macOS arm64 / Go 1.22 / platform-tools v1.51, SBF v3.
**PASS:** 125 serial measured command samples, five per workload/scenario, with
seeded shuffled workload order. `summary.json` includes every wall/child-CPU/load
sample, CLI phase timings, tool/source hashes, distributions and footprint.
`inputs/` preserves the exact staged application snapshots; `tools/` preserves
both frontend binaries. Prior measurements remain separate historical evidence.

## Build and test latency

Median wall seconds, five observations per cell:

| Workload | Project-clean build | Comment edit build | No-op build | Forced native tests | Native test edit | Native + lifecycle SBF |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Manual preloaded token swap | 0.143 | 0.138 | 0.011 | — | — | — |
| Historical generated swap only | 0.512 | 0.465 | 0.298 | 0.361 | 0.397 | — |
| Current generated managed swap | 0.960 | 0.917 | 0.342 | 0.406 | 0.463 | 0.613 |
| Generated SOL escrow | 0.581 | 0.520 | 0.289 | 0.341 | 0.349 | 0.504 |

The manual row is a direct compiler invocation. Generated rows include schema
validation/generation, Go package loading, compiler/cache work and CLI startup.
No-op outputs retain their mtime; comment edits rebuild but reproduce the same
ELF. Native tests use `-count=1`, and native test edits preserve the existing SBF
ELF. Integrated tests use the unchanged SBF build cache plus forced native tests.
These are local observations under variable shared-host load, not promises.
The failed earlier attempt had substantially slower timings under different load.

Installed LLVM and OS filesystem caches were warm. Each project-clean sample
deletes that staged project's build directory; it is not an empty host/tool cache.
The experiment starts a fresh native Go cache for CLI bootstrap: 8.528 seconds
fresh, 0.179 seconds warm. Offline pinned runner installation took 0.134 seconds.
Downloads, Cargo/validator startup and external developer time are excluded.
Native suites differ by workload and are not matched native-suite comparisons.

Direct CLI/runner execution of the same 108-scenario/110-transaction swap corpus
took median 0.142 seconds manual, 0.195 historical generated, and 0.192 current
generated. The separate managed lifecycle has 29 scenarios/36 transactions and
took 0.124 seconds; escrow has 32/42 and took 0.078. Reports separately record
runner startup, fixture preparation and VM execution. Every sample exactly
matches its authoritative case/error/CU/log report; no Rust timing rerun is claimed.

## Compute and artifacts

| Workload | ELF bytes | Build-only logical bytes | Build-only allocated bytes | Matched wide swap CU |
| --- | ---: | ---: | ---: | ---: |
| Manual preloaded token swap | 8,064 | 8,217 | 12,288 | 20,329 |
| Historical generated swap only | 21,536 | 21,689 | 28,672 | 45,941 |
| Current generated managed swap | 68,320 | 68,473 | 73,728 | 46,016 |
| Generated SOL escrow | 25,280 | 25,433 | 32,768 | — |

Build-only totals are ELF plus a 153-byte output receipt, measured immediately
after clean builds. Final retained totals also include the latest JSON test report:
139,999 logical/147,456 allocated bytes for managed swap and 83,095/94,208 for
escrow. The historical generated/manual rows do not include such project reports.
Complete staged project totals include source, SDK snapshots and test fixtures,
including SPL Token ELF in the managed example; they are not build-cache costs.

Across 69 successful single-step shared cases, CU median/range is
20,320 / 20,241–20,354 manual, 45,932 / 45,853–45,963 historical generated,
and 46,007 / 45,928–46,038 current generated. Median paired generated/manual
ratios are 2.260 and 2.264. This quantifies the framework cost on this corpus;
the generated safety policy and error categories are described in the application
proof. All three execute the same real SPL Token transfers.

Current managed creation/deposits use 86,742 CU, managed swap 47,858, and
drain/close 61,869. Escrow uses 20,508 open, 8,356 claim and 6,756 cancel; these
are separate lifecycle workloads. The current swap ELF contains four instructions
and two layouts; the manual and historical generated versions contain swap only.
The historical frontend/SDK are deliberately preserved, so the difference cannot
be attributed solely to newly added instruction code. This experiment does not
measure a bounded starter, dependency-heavy protocol or Whirlpools rewrite.

Shared costs are reported separately: CLI 7,674,560 bytes, runner executable
6,886,240, runner installed package/notices/receipts 8,884,202, final native Go
cache 151,516,869. Actual Clang + LLD binaries total 176,073,704 bytes (~168 MiB);
the preinstalled full LLVM directory totals 398,819,809 bytes, not a minimal
managed installation. The 3,165,750-byte runner archive and backend download
are setup costs, not per-project artifacts. Each current SDK snapshot is 30,864
logical bytes. `st_blocks * 512` gives allocated disk bytes separately.

Separate untimed uncached compiler probes sampled TMPDIR every 25 ms: logical
peaks were 38,711 bytes manual, 100,666 historical generated, 231,138 managed,
and 111,340 escrow. These are sampled lower bounds, not guaranteed peak memory
or disk usage. Dedicated probe directories were empty after all four builds.
The shared setup TMPDIR retained only the macOS `xcrun_db` (1,492 bytes).

## Evidence and reproduction

All original staged program/test files, generated wire/client/layout artifacts,
and authoritative source hashes are unchanged after restoration. ELF hashes match
the [managed proof](../2026-10-06-managed-swap-supported/README.md),
[swap/service proof](../2026-10-06-framework-swap-managed/README.md),
[escrow proof](../2026-10-06-escrow-token-lifecycle/README.md), historical swap-only
proof and preserved manual token corpus. No fresh-validator, clean-host,
independent-developer or production-support claim is made here.

```sh
python3 scripts/milestone2_bench.py --output results/compiler/new-m2-bench --samples 5
```

Use a new directory. The historical swap-only frontend/module are taken from
`build/framework-swap/2026-10-05-framework-swap-lifecycle-final`; the exact inputs
and binaries used here are archived in this report if that ignored stage is lost.
The original failed temp-directory assumption is preserved in the sibling attempt.
