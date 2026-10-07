# Milestone 2 benchmark harness bring-up

This attempt completed all 125 measured command samples, including exact
ELF/case/error/CU/log checks, but failed before final footprint aggregation.
`summary.json` remains `passed:false`: the shared temporary directory contained
macOS's 1,492-byte `xcrun_db`, violating an overly strict empty-directory
assumption. This does not establish a compiler temporary-file leak.

The corrected script uses a new dedicated TMPDIR for each untimed compiler
probe, records shared temporary retention separately, and places `-no-cache`
after the project `build` subcommand. The latter latent issue was not reached
in this attempt. Sources, tool snapshots, raw samples and logs remain preserved.
Use [the new passing experiment](../2026-10-06-milestone2-bench-supported/README.md)
for the final distributions and footprint conclusions.
