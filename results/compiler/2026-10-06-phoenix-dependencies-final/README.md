# Phoenix compiler/SDK dependencies: passing proof

Read [the complete report and new frontend/SDK/runner handoff](../../../docs/PHOENIX_DEPENDENCIES.md).

- 580 checked Go multiplication cases per target × three actual-SBF runs pass,
  including overflow aborts and rollback.
- All 631 IOC/FOK/free-funds trading scenarios pass Go v0/O2, Go v3/O2, scoped
  Rust v0 and full Rust v0 × three. Go v3 CU stays unchanged, median paired
  successful-case ratio 1.2315218746104948×. The separate same-ISA Go v0 control
  measures 1.8969177209815742×; it does not replace the v3 benchmark.
- The original 20-byte order-ID return setter and 16-case/17-transaction broader
  return-data probe pass both v0/v3 × three, checking exact bytes and program ID
  in simulation and submitted transaction metadata, CPI clearing/replacement,
  reset, failed paths and account rollback. Boundary payloads use preloaded
  account data to fit valid legacy packets.
- Native helper/return-data differentials, root tests/vet, runner tests and all
  four SDK applications plus optimized Whirlpools regressions pass separately.

`summary.json` records commands, source/artifact pins, static frames and metrics.
`trading-per-case-cu.json` preserves all paired CU rows. `sdk1-control.json` proves
an exact SDK-1 no-op ELF match to the prior frontend. `verify.py.txt` and
`return-data-svm.py.txt` are the exact drivers used.

`frontend.tar.gz` ships the exact local macOS arm64 CLI, matching SDK, complete
frontend source and 121-file manifest. `probes.tar.gz` preserves the reproduction
modules with that SDK. `return-data-runner` and `return-data-runner-source.tar.gz`
ship the separate observable runner and its complete patched source/lockfile.
`delivery-verify.json` records archive/file/executable checks and exact v0 ELF
rebuilds with the extracted executable and source-rebuilt CLI. Old runner
packages, frontend pins and benchmark sources are untouched.

`root-tests.log`, `root-vet.log`, `runner-tests.log` and `runner-build.log` retain
validation. The [full-width helper ABI proof](../2026-10-06-multi3-sbf/summary.json)
adds 633 complete 128-bit SBF products × both targets × three.
The initial incomplete dependency proof remains at
`../2026-10-06-phoenix-dependencies`: its new guard caught `__moddi3` before
runtime, prompting unsigned CPI meta-length lowering. It is not final acceptance.
No fresh-validator, clean-host or public release is claimed.
