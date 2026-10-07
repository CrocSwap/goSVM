# Optimized Whirlpools final proof

October 6, 2026. **1.280853× scoped Rust**, median paired successful-case CU, at O2 on the event-omitting classic-token fixed-fee handler. This meets the 1.33× target.

- All 166 handler scenarios × three runs × Go, unchanged scoped Rust and full upstream Rust pass exact account bytes/metadata, errors, CPIs and rollback. Rust CU/logs reproduce the original references.
- All 29,713 component cases × three runs × Go and unchanged Rust pass, including 5,000 independent-integer MulDiv boundary/random cases.
- Native shared-module tests repeat three times, including pinned Rust vectors, swap transitions, math/big/old-algorithm differentials and public Apply validation after mutation.
- All 103 successes improve from the 1.702520× checkpoint: median saving 14,416 CU / 21.91%. Of 63 failures, 29 improve and 34 stay unchanged. None regress.
- ELF: 83,424 bytes; SHA256 `15fc075e82d857a5eca89f71fdf0a55154ca3bc3809497b2ac2d3f78c9673398`. Largest static frame: 1,472 bytes. Individual successful ratios range from 1.226617× to 1.617088×.

[Summary/pins/commands](summary.json), [per-case CU](per-case-cu.json) and [application report](../../../docs/WHIRLPOOLS_133_TARGET.md). Exact verify.py.txt is preserved alongside the outputs.

Use the separately checked [delivery archive](../2026-10-06-whirlpool-optimized-snapshot/optimized-snapshot.tar.gz), which records executable mode, includes complete frontend source and rebuilds the measured artifact from extracted modules. This proof's original archive is retained as historical evidence. Benchmark defaults and prior source/pins remain unchanged.
