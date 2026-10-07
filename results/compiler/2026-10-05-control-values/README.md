# Multiple-result and control-flow validation

October 5, 2026, macOS arm64. Three actual-SBF repetitions pass all 141 scenarios,
with 140 independently compiled ordinary-Go expectation vectors and one atomic
success-then-bounds-failure transaction. Ten expected failures cover slice/array
bounds and custom status 9. Exact state/metadata, error categories, CU and logs
repeat. A native-Go/generated-C differential test separately checks 1,000 random
vectors and 10 edge/error vectors, observing effects before abort so runtime
rollback cannot hide evaluation-order errors.

The ordinary-Go [fixture](../../../examples/control-values/program.go) tests
multiple results/assignments/declarations, forwarding and sole-argument tuple
calls, new/existing `:=` bindings, blank destinations, saved index operands,
struct/array copies, dynamic case calls, multiple/default cases, array tags,
nested continue/break and loop posts. Imported multi-result functions and
source-located unsupported forms have separate compiler tests.

The tested ELF is 2,936 bytes, SHA-256
`b99d93fa50732df5c8fa4a0b18b7fbf664d874a0aec6f9f58d02880e63aa8ce9`.
Static Clang frames are 64 bytes each for handler and entrypoint, and zero bytes
for memory helpers. Stack instrumentation links to the exact tested ELF.
These measurements apply to this fixture, not arbitrary functions or call graphs.

- [Summary, source/artifact hashes and commands](summary.json).
- [Native inputs](native-input.json), [native expectations](native-expectations.json)
  and [driver source](native-driver.go.txt).
- [Fixtures](fixtures.json), [ELF](program.so), [generated C](program.c),
  [stack usage](stack-usage.tsv).
- Runtime reports [one](svm-0.json), [two](svm-1.json), [three](svm-2.json).
- [Focused test log](differential-tests.log) and [root/nested checks](checks.json).
- [Prior workload regression](../../svm/2026-10-05-m2-control-regression/README.md)
  and [shared-package regression](../2026-10-05-control-import-regression/README.md).

Reproduce with `python3 scripts/control_verify.py --output results/compiler/new-controls`.
Each output must be new. This is native-Go/SBF comparison, not a fresh validator;
the user skipped milestone 1's final fresh-validator and clean-host checks.
[Language semantics and limits](../../../docs/GO_CONTROL.md) apply. This compiler
fixture is not the bounded starter, full token swap or dependency-heavy protocol
benchmark. Named results, interfaces/error, labeled branches and fallthrough
remain rejected; explicit unsigned statuses are the documented error contract.
