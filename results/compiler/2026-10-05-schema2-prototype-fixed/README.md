# Schema 2 compiled SBF prototype

The two-instruction/two-layout ledger prototype passes 161 native-Go vectors
against independent packed-wire/math/error expectations, then 165 compiled-SBF
scenarios/166 transactions in three repetitions. All account bytes/metadata,
errors and repeated CU/log outcomes agree. There are 34 expected failing steps.

Coverage includes wide uint64 moves, nested unaligned fields and public keys,
owner/authority/privilege/discriminator/length/duplicate failures, deterministic
validation ordering, readonly results, merged writable privileges and transaction
rollback after an earlier successful instruction. CLI scaffold/check/native tests,
build and project `test --svm` execute the same supported authoring workflow.

The ELF is 7,088 bytes with SHA-256
`1ed86332915c41857147282ceaf7cf0d4f042ae8aebb81677d9ee6f310f8df0d`.
Clang's entry frame is 1,408 bytes; memcpy/memset frames are zero. Stack
instrumentation links to the exact tested ELF. A single move uses 4,863 CU and
SetLimit uses 1,733 CU in this corpus. These are ledger-prototype observations,
not starter, token-swap or protocol comparisons.

This snapshot predates the additional recorded-migration/historical-manifest
guards and positive alias/array tests. Its runtime result remains historical
evidence for these exact source hashes. It proves no token transfer, initialization,
PDA constraint, escrow lifecycle, fresh-validator or external-developer trial.
The earlier driver's JSON null/empty-array failure is preserved separately.

Reproduce against current source in a new directory:

```sh
python3 scripts/schema2_verify.py --output results/compiler/new-schema2-proof
```
