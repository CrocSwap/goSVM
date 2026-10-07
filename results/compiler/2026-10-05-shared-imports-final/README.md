# Shared-package compiler validation

October 5, 2026, macOS arm64. Module-aware imports compile the same canonical
Go model, quote and explicit wire codec used by an independent native client.
All 142 SBF scenarios passed in three repetitions with runner 0.5.0 and v1.51
LLVM/SBFv3. There are 140 native-Go single-instruction vectors and two transactions
checking successful-state-update-then-error rollback. Thirty-eight expected
failures cover invalid/overlarge inputs, sequence overflow and slippage. Exact
account state/metadata, errors, CU and logs are stable in all repetitions.

The [application](../../../examples/shared-packages/README.md) imports `model`,
`quote`, `wire` and a transitive `math` helper. Native expectations call the exact
same Go source with the ordinary Go toolchain. Root differential tests separately
compare 1,011 native-Go/generated-C vectors, including integer/domain boundaries.
The independent service builds against an isolated copy of only the pure libraries
and `go.mod`, with no SDK/compiler/on-chain package or dependency on the checkout.
It uses the same named Pool/SwapArgs/Amount/PublicKey types, round-trips codecs and
computes 1,998 output for 1,000 input at reserves 1,000,000/2,000,000. Its wire bytes
are also exercised by SBF. These modules are unpublished and use normal local
application-module replacements; public dependency distribution is not claimed.

The ELF is **2,048 bytes**, SHA-256
`f43f2ad0ce33659afb94bc8b5c3addfcdc2699c1a03e87f28c403e7e411007af`.
Clang reports a 64-byte static entry frame and zero-byte static handler/memory-helper
frames for this optimized fixture. Stack instrumentation links to the byte-identical
runtime ELF. This is a bounded arithmetic/state example with handwritten codecs,
not the full token swap, schema-2 generator, dependency-heavy protocol or a
performance comparison. There are no real token transfers in this example.

Live cache checks rebuild a copied application: no-op retains the output mtime,
a transitive math edit changes both receipt input and ELF, and restoring source
reproduces the original ELF. Root tests cover dependency source/file/module/sum
changes, host-only exclusion, loaded source snapshots, qualified symbol collisions,
SDK type identity, import/function cycles, illegal internal/main imports and
source-located unsupported-feature errors. Resolution uses private alternate
module metadata, offline Go and no package initialization.

- [Summary, source hashes, commands and limitations](summary.json).
- [Native inputs](native-input.json), [native expectations](native-expectations.json)
  and [native driver source](native-driver.go.txt).
- [Fixture suite](fixtures.json), [tested ELF](program.so), [generated C](program.c)
  and [stack usage](stack-usage.tsv).
- Runtime reports [one](svm-0.json), [two](svm-1.json), [three](svm-2.json).
- [Isolated service](isolated-service.log), [resolved dependencies](isolated-dependencies.log)
  and [live cache proof](cache-check.json).
- [Root and separate-module checks](checks.json).
- Rebuilt [starter/token/lifecycle/sysvar regressions](../../svm/2026-10-05-m2-import-regression/README.md),
  [scalar arrays](../2026-10-05-import-array-regression/README.md) and
  [1 KiB capacities](../2026-10-05-import-capacity-regression/README.md) retain
  their prior ELF hashes and exact outcomes.

Reproduce with `python3 scripts/import_verify.py --output results/compiler/new-imports`.
Choose a new output name; the earlier import snapshot is preserved. Native driver
source is saved as text; executable/module inputs remain in ignored staging.
[Supported imports and limits](../../../docs/GO_PACKAGES.md) apply. No fresh
validator or clean-host comparison is claimed; the user skipped those additional
checks when closing milestone 1. Methods/pointers, schema-2 generated clients and
full swap/escrow reuse remain milestone 2 work.
