# Canonical checked packed stores

PASS: the canonical compiler exactly reproduces the isolated write-only control ELF and passes the frozen compact Whirlpools handler corpus, 166 cases × three runs. Matched account bytes/metadata, errors, real Token CPIs and rollback pass. Token consumed CU stays identical; caller consumed/remaining budgets change. The preserved Rust denominator is unchanged.

Median paired successful-case ratio: **1.737883 → 1.702520**. All 103 successes improve, median **746 CU (1.34%)**, range 443–9,230. Of 63 failures, 23 improve and 40 are unchanged; no regression. Median ratios and median per-case savings are different distributions.

ELF: **84,208 bytes** (+16), SHA-256 `88c724968cb445c0709d02912e814b4aa4e3783634268e22d31536e79eeee54b`. Largest static frame: **1,472 bytes**, unchanged. SDK/API/benchmark application sources remain unchanged; only the matching frontend's recognized encoding loops change.

[Summary](summary.json), [per-case CU](per-case-cu.json), source hashes, generated C/LLVM IR, stack report and all command logs preserve evidence. Native tests cover 12,000 independently encoded vectors in both fast and fallback C builds under UBSan, nil/short/unaligned buffers, overflow offsets and partial-write panics; similar loops and legacy/SDK-1 output are checked. Root [tests](root-tests.log) and [vet](vet.log) pass.

- [422-case direct SBF bounds/rollback proof](../2026-10-06-packed-store-probe/README.md).
- [Five isolated controls](../2026-10-06-packed-codegen-controls-final/README.md).
- [117 checked-token scenarios](../2026-10-06-checked-token-packed-store/README.md).
- [29/36 managed-swap lifecycle scenarios/transactions](../2026-10-06-managed-swap-packed-store/README.md).
- [108/110 full-swap/service scenarios/transactions](../2026-10-06-framework-swap-packed-store/README.md).
- [32/42 escrow scenarios/transactions](../2026-10-06-escrow-packed-store/README.md).
- [Interleaved forced-build observations](../2026-10-06-packed-store-builds/README.md): baseline/optimized medians 5.64/6.05 seconds, five samples each on the shared desktop. This small run does not establish causal build-time improvement or a stable slowdown. Both build outputs reproduce the expected ELF hashes.

## Frozen frontend for benchmark adoption

[frontend.tar.gz](frontend.tar.gz) contains the ad hoc signed macOS arm64 CLI, complete SDK snapshot, frontend source and [manifest](frontend-manifest.json). This is an experimental local snapshot, not a public release. Source identity is `e2cb0da62929a9b1257cdca4248885345e814bdf394cd1473341ee7ad61f97a2`, computed as SHA-256 of the compact, key-sorted JSON source-hash map. The manifest binds every source/SDK file and CLI. The source snapshot supports rebuilding the CLI; full repository regression fixtures remain in the checkout/results. Archive checksum is in [frontend.tar.gz.sha256](frontend.tar.gz.sha256).

Extract outside the source checkout and record a separate benchmark pin. Create a fresh schema-2/SDK-2 scaffold with this CLI or preserve the included complete SDK; do not overwrite original/default pins. Build the existing compact handler and rerun the benchmark's own Rust comparisons. No application rewrite is required. Toolchain remains platform-tools v1.51/SBFv3/O2, with the existing feature profile and compute budget.

Reproduce in a new directory with `python3 scripts/packed_store_verify.py --output results/compiler/NEW-EXPERIMENT`; preserved canonical compact-handler source/fixture/runtime tools are prerequisites. The verification scripts are also copied here as text. See [implementation, scope and next work](../../../docs/PACKED_STORE_OPTIMIZATION.md).
