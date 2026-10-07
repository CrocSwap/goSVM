# Fixed-array compiler validation

October 5, 2026, macOS arm64. The first milestone 2 increment supports bounded
scalar arrays, including `[32]byte` keys. Three real-SBF repetitions passed all
147 scenarios/147 transactions, with 148 instructions per repetition. Thirteen
expected failures check bounds aborts, an explicit handler error, and atomic
rollback after an earlier successful array-writing instruction.

The 146 single-instruction expectations come from the same source compiled by
ordinary Go, independently of goSVM's generated C and VM outputs. There are 128
randomized value cases, valid edge accesses, zero-length arrays, byte wrapping,
and out-of-range indexes including 2^32, 2^63 and uint64 maximum. For failures,
the full account must retain its initial state. The multi-instruction transaction
also retains its initial account bytes. The general runner checks full state and
metadata during simulation/submission; exact case results, CU and logs are stable
across all three repetitions.

The native-Go/generated-C differential test separately passed 1,000 random value
vectors and 15 edge/failure vectors. A custom host abort driver observes effects
before a failed bounds check, so evaluation-order bugs cannot be hidden by VM
rollback. Multi-file ordering, unsupported forms and size-limit checks pass.

The tested ELF is **4,432 bytes**, SHA-256
`1f74cffe37bb94ef27d4121f45aa58f785b09607dba0f819fd74b9f1feb7bafd`.
Clang `-fstack-usage` reports static frames of **320 bytes** for `go_Process` and
**64 bytes** for `entrypoint`. The instrumented object links to a byte-identical
ELF, binding these measurements to the runtime artifact. This is a compiler
fixture, not the bounded starter, full token swap or dependency-heavy protocol.
No comparative build-latency or CU-ranking claim is made.

## Evidence

- [Summary, source/artifact hashes and commands](summary.json).
- [Native inputs](native-input.json), [native expectations](native-expectations.json)
  and [driver source](native-driver.go.txt).
- [Fixture suite](fixtures.json), [tested ELF](program.so), [generated C](program.c)
and [stack usage](stack-usage.tsv).
- Full SBF reports [one](svm-0.json), [two](svm-1.json), [three](svm-2.json).
- [Differential/diagnostic test log](differential-tests.log) and
  [root/nested verification](checks.json).
- [Prior-workload regression](../../svm/2026-10-05-m2-arrays-regression/README.md).

Reproduce with `scripts/array_verify.py --output results/compiler/new-arrays`;
see [array support/limits](../../../docs/GO_ARRAYS.md). Scalar elements and a
1,024-byte per-value limit are intentional; nested/struct elements, slices,
pointers and package imports are not implemented by this increment. The stack
measurement does not establish a bound for all user programs. Runner 0.5.0 and
the package pin remain unchanged; no fresh-validator comparison is claimed.

The earlier `../2026-10-05-arrays/` and `../2026-10-05-arrays-complete/` snapshots
remain intact. Added evidence-only module boundaries prevent their original Go/C
driver artifacts from being discovered by root tests. The final script saves
the driver as text and builds it in ignored staging instead.
