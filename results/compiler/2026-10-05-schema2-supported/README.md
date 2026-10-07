# Schema 2 supported prototype

The current schema-2 generator passes the CLI scaffold/check/native/build/SVM
workflow and 165 SBF scenarios/166 transactions in three repetitions. The 161
native vectors match independent packed-wire/math/error expectations; repeated
SBF account bytes/metadata, errors, CU and logs agree. There are 34 expected
failing steps, including multi-instruction atomic rollback.

Generator tests cover canonical types/independent service imports, nested packed
fields, named scalar arrays/zero arrays, minimum privileges, readonly aliases,
program identities, collisions, reserved constant errors, source diagnostics and
publication after validation. Layout-history tests enforce explicit higher
versions, recorded migration policies, distinct historical discriminators,
malformed-history rejection and restoration of a missing lock.

The 7,088-byte ELF has SHA-256
`1ed86332915c41857147282ceaf7cf0d4f042ae8aebb81677d9ee6f310f8df0d`.
Its static entry frame is 1,408 bytes; memory helper frames are zero. Instrumented
linking reproduces the same artifact. A single wide Move uses 4,863 CU; SetLimit
uses 1,733 CU in this corpus. Frame/CU/footprint observations apply to this ledger
prototype, not the bounded starter, full token swap or dependency-heavy protocol.

These are preloaded program-owned ledgers. Checked classic-token/PDA/CPI APIs,
initialization/closing, full swap/escrow lifecycle, fresh-validator comparison and
external trial-developer evidence remain open. Root `make test`, `go vet ./...`
and whitespace/link checks also passed after this implementation.

Reproduce against a new source snapshot in a new results directory:

```sh
python3 scripts/schema2_verify.py --output results/compiler/new-schema2-proof
```
