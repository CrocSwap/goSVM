# Anchor comparison results

Measured October 4, 2026 on the shared Apple M2 desktop, using pinned Anchor
0.32.2, platform-tools v1.51 / SBF v3, and validator 3.0.15.

**CU is mixed; build time and artifact footprint favor Go.** The 486-CU generated
starter beats this Anchor implementation, but the full token-transfer swap does
not. This is not evidence of a general Go runtime advantage over Anchor.

| Workload | Implementation | Valid swap CU | Clean build (one sample) | Edit build (median of 3) | No-op (median of 3) | ELF | Retained project artifacts |
|---|---|---:|---:|---:|---:|---:|---:|
| bounded | Go | 486 | 1.44 s | 0.24 s | 0.036 s | 3,216 B | 3.3 KiB |
| bounded | Lean Rust | 348 | 2.71 s | 0.66 s | 0.185 s | 1,784 B | 115.6 KiB |
| bounded | Anchor 0.32.2 | 768 | 159.02 s | 9.09 s | 1.437 s | 154,344 B | 276.33 MiB |
| token | Go | 20,329 | 2.61 s | 0.59 s | 0.047 s | 8,064 B | 8.0 KiB |
| token | Lean Rust | 20,392 | 1.92 s | 0.94 s | 0.203 s | 12,288 B | 169.7 KiB |
| token | Anchor 0.32.2 | 19,431 | 155.74 s | 2.47 s | 0.325 s | 188,768 B | 295.95 MiB |

The bounded track uses the actual generated Go project. The token track uses the
existing manually written Go SDK program, adapted to the same discriminated wire
format as Anchor and lean Rust. A generated multi-account Go framework is not
implemented yet. “Swap” in the bounded track means arithmetic/state mutation;
only the token track executes two real SPL Token transfers.

The wide token case consumes 9,290 CU inside SPL Token for every implementation.
Go's total is 20,329 CU versus Anchor's 19,431 (Go uses **4.6% more**; Anchor uses
4.4% less). The small starter is 486 versus 768 CU (Go uses **36.7% less**).
Anchor's default instruction-name log is included. These are typed `Account`
programs, not zero-copy/hand-optimized Anchor variants, and not Anchor 1.x.

Correctness: 315 token simulations and 42 bounded simulations, 6 committed token
swaps and 6 token rollback checks, plus all bounded fixtures submitted and checked
for persistent state. Invalid-path error categories and CPI traces are explicit.
Successful token paths check full bytes of the pool and four token accounts
against independent arithmetic; rollback checks verify unchanged state.

The build rows measure ELF compilation with installed tools and downloaded
sources, not first-machine setup. Rust profiles match optimization level 2, LTO,
and one codegen unit. Token-2022 and unrelated Anchor SPL features are disabled.
IDL/TS generation, Go project regeneration, native Rust test harness compilation,
shared Go/Cargo caches, and downloaded toolchain sizes are excluded. The cold
rows are single observations; edits/no-ops have three samples. Wall/CPU times
and load averages are retained because this desktop is not isolated benchmark
hardware. See the [methodology](../../benchmarks/anchor/README.md) for overlap
and compatibility limitations. No Basanos sources or targets were changed.

One follow-up candidate is CPI marshaling: our Go and lean Rust adapters pass
all eight account infos to each token CPI, whereas Anchor SPL passes the three
required infos. That is a source-level difference worth isolating, **not a
measured attribution** of the CU gap. This benchmark does not change the Go SDK
or tune the implementations after observing which one wins.

- [Raw build timings, commands, tools, and source hashes](benchmark.json)
- [Token verification, Token program hash, errors, and per-case CU](token-verification.json)
- [Generated Go bounded verification](bounded-go-verification.json)
- [Lean Rust bounded verification](bounded-lean-rust-verification.json)
- [Anchor bounded verification](bounded-anchor-verification.json)
- [Summary and final source manifest](summary.json)

Reproduce with `scripts/anchor_bench.py`, `scripts/verify-anchor.sh`, then
`scripts/anchor_report.py`, following the dependency preparation steps in the
methodology. Earlier results directories are preserved as historical snapshots.
