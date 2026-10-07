# Interleaved forced-build observations

PASS: five forced (`-no-cache`) SBF builds per frontend, alternating first-run order, using one identical copied application/SDK and installed cached tooling. Median baseline **5.64 s**, optimized **6.05 s** on the shared desktop. The sample is small and does not establish a causal speedup or stable slowdown. This measures complete uncached frontend/backend builds, not source-edit/no-op/test latency or download/bootstrap cost.

Both final ELF hashes match their preserved verification checkpoints. [Summary](summary.json) records each command/sample, hashes and medians; stdout logs are retained. See [canonical proof](../2026-10-06-packed-store-canonical-final/README.md).
