# Scalar-array regression after package imports

October 5, 2026. All 147 array scenarios pass in three SBF repetitions after the
module-aware compiler refactor. Native expectations, exact errors/account bytes,
rollback, CU/log repetition and stack measurements pass. The 4,352-byte ELF retains
the [prior array snapshot's](../2026-10-05-arrays-supported/README.md) SHA-256
`a96b92efb0798b2fc2de812b70bf2886c42a9ec20b10789c7b9b427615e64574`.

[Summary and current source hashes](summary.json) bind this run to the import
increment; earlier evidence is preserved. Test logs, emitted C, ELF, native vectors,
fixtures, stack usage and three runtime reports are saved alongside the summary.
See the [current import evidence](../2026-10-05-shared-imports-final/README.md) for
root/nested checks. No fresh validator or universal stack-bound claim is made.
