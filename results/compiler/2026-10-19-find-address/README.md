# On-chain bump search: `pda.Seeds.FindAddress` (2026-10-19)

Question: can a goSVM program find a PDA's canonical bump on chain exactly as
Solana's `find_program_address` does, using the `sol_create_program_address`
syscall the SDK already calls?

Compiler: branch `gap4-find-pda` (parent `79cf25c`); Go 1.22.0;
platform-tools v1.57; runners 0.5.1 (fogo-v0) and 0.6.0 (solana-v3). Local
only (`network_validation: false`).

## Solana's rule

`try_find_program_address` (solana-address 2.0.0 `syscalls.rs`, the on-chain
`sol_try_find_program_address` syscall likewise) starts at bump 255 and makes
`u8::MAX` = 255 attempts: bumps 255 down to **1**; bump 0 is never tried. An
on-curve result (`InvalidSeeds`) moves to the next bump; any other error ends
the search. The host builder `fixture.FindProgramAddress` used to also try
bump 0; it now stops at 1 (no committed fixture changed: no suite reaches
bump 0).

## Design

`FindAddress(c, program) ([32]byte, byte, uint64)` copies the builder, appends
bump 255 and rewrites that byte down to 1, calling `solana.CreateAddress` for
each: status 0 returns the address and bump, 1 (on curve) continues, any other
status returns it. No bump: 3004 (new). A builder with 16 seeds has no room
for the bump: 3001, at once (Solana also finds nothing; its syscall would spend
255 attempts). The builder itself is unchanged. It is SDK Go, so programs that
do not call it compile to the same ELF.

## Results

| Check | Command | Result |
| --- | --- | --- |
| Against Rust `solana-pubkey` 3.0.0 | `go test ./sdk/pda -run TestFindAddress` | 401 vectors (0..16 seeds of 0..32 bytes, canonical bumps 247..255) match address and bump; 417 on-curve bumps skipped, one attempt per bump from 255; builder unchanged; the host fixture agrees |
| Statuses | same | no bump: 255 attempts, 255..1, then 3004; other statuses propagate after one attempt; 2009 without a native callback; 16 seeds 3001; `Address` with the found bump agrees |
| SBF, both targets | `gosvm test --svm` in [examples/find-address](../../../examples/find-address/README.md) | 10 scenarios pass on fogo-v0 and solana-v3 |
| Full-swap reference ELFs | `-no-cache` builds, both targets | `a66c5a97…` / `b419245b…`, unchanged |
| Example modules | `GOWORK=off go test ./...` in tutorial, escrow, full-swap, find-address | pass, fixture staleness tests included |

[oracle/](oracle/) is the Rust generator of `sdk/pda/testdata/find_vectors.json`
(`cargo +1.88.0 run --release --offline`, from the local crate cache); a rerun
reproduces the file byte for byte.

The SVM scenarios create a record at the canonical PDA (bump 252: three
on-curve addresses skipped on chain), refuse a valid noncanonical bump (the
generated check accepts it; the handler's search does not) with rollback, and
return `find_program_address` results as return data for Hyperlane-like seeds,
a seed whose bump is 250, 15 seeds, and 16 seeds (3001).

| Scenario | fogo-v0 CU | solana-v3 CU |
| --- | ---: | ---: |
| create-at-the-canonical-bump-252 | 24,647 | 24,593 |
| noncanonical-bump-fails | 23,985 | 23,931 |
| derive-a-bump-255 | 4,256 | 4,257 |
| derive-fees-bump-254 | 5,857 | 5,858 |
| derive-outbox-bump-255 | 4,326 | 4,327 |
| derive-dispatched_message-bump-254 | 6,053 | 6,054 |
| derive-igp-bump-253 | 7,402 | 7,403 |
| derive-seed-1-bump-250 | 12,121 | 12,122 |
| derive-fifteen-seeds | 5,294 | 5,295 |
| derive-sixteen-seeds-fails | 2,577 | 2,577 |

Each skipped bump costs about 1,573 CU (`derive-a` at 255 to `derive-seed-1`
at 250: 7,865 CU for five more attempts), of which the syscall charges 1,500.
Rust's on-chain `find_program_address` uses `sol_try_find_program_address`,
1,500 CU per attempt inside one syscall; that syscall would save the ~73 CU of
Go per attempt but was not used, by request.
