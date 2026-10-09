# Actual Bend consumer fixture

`program.go.txt` and `program_test.go.txt` are source templates copied into a
fresh `gosvm new` module by `scripts/verify_target_profiles.py`. Its generated
Bend package is imported from the producer's pinned manifest rather than checked
in with host-specific object binaries.

Native tests call the generated cgo API: accepted/refused/stop results, malformed
tag/bool/padding, short/null buffers and overlap, preserving output on failure.
`sbf.json` exercises five valid inputs, three malformed inputs and a successful
instruction followed by malformed input in one transaction. It checks exact
simulation/submission errors and persisted state rollback against the real Go/Bend
SBF image. Target mismatch and contract/object mutation tests are separate.

Reproduce with the command in the [target acceptance report](../../results/compiler/2026-10-07-target-profiles-final/README.md).
