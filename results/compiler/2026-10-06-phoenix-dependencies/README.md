# Retained incomplete Phoenix dependency verification

This run completed all 580 checked-multiply cases for v0/v3 × three and all
631 matched trading cases for Go v0/v3, scoped Rust and full Rust × three.
The 20-byte return-data reproduction also passed three v0 runs.

The broader v0 CPI return-data probe then failed at build time: the new import
guard rejected `__moddi3`, emitted from the SDK's signed `slice.len % 9` meta
check. Slice lengths are nonnegative; the final compiler uses unsigned bounds
and remainder without changing valid length/error semantics. The guard prevented
an unsupported helper from reaching SBF execution.

This directory remains an incomplete run (`passed: false`); it is not final
acceptance. The exact attempted driver and adapter, logs and reports remain.
See [the final proof](../2026-10-06-phoenix-dependencies-final/summary.json).
