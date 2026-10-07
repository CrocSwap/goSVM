# Existing workload pointer regression

October 5, 2026. A newly source-installed CLI containing the constrained pointer
and method compiler changes rebuilt the existing programs and retained the
historical fixture outcomes and ELF hashes. This installed-host rehearsal uses
the pinned local runner package and an isolated cache with verified v1.51 LLVM.

| Workload | Passing coverage |
| --- | --- |
| Generated bounded starter | 14 default fixtures and 3 general scenarios; separate native tests |
| Manual full token swap | 108 scenarios and 110 transactions, including real classic-token CPIs and rollback |
| Token lifecycle | 6 scenarios and 26 transactions, including initialization/close/reuse and failure paths |
| Stateful sysvars | 6 scenarios and 15 transactions |
| Metadata negative | Expected rent_epoch mismatch rejects the run and removes the stale report |

[summary.json](summary.json) binds source hashes, exact commands, installer/cache
checks, historical fixture hashes and report identities. Runtime reports compare
complete recorded case results, not only success counts. The scaffold includes
its own module, generated codecs/client and embedded SDK; the general fixtures
are copied from the preserved starter corpus.

Root tests/vet and separately discovered starter/transport module tests also
passed after this compiler change. The [new pointer fixture](../../compiler/2026-10-05-pointer-values/README.md)
provides the new feature's native-Go/C/SBF comparisons and borrowing checks.

This is not a fresh validator or clean-machine check, and does not reopen the
user-waived milestone 1 gates. It does not run the dependency-heavy protocol
benchmark or certify remaining milestone 2 framework/application requirements.
