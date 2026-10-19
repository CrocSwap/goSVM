# Finding a PDA bump on chain

A schema-2 SDK-2 program that calls `pda.Seeds.FindAddress`, the on-chain
equivalent of Solana's `find_program_address`: it appends each bump from 255
down to 1 and returns the first off-curve address, skipping on-curve results
exactly as Solana does.

- `Create` makes the record at the PDA `["record", authority, bump]`. Generated
  code checks the PDA for the supplied bump and creates the account; the
  handler then accepts only the canonical bump, as Anchor's `seeds`/`bump`
  constraint does.
- `Derive` returns the address and bump for its seeds as 33 bytes of return
  data.

`cmd/fixtures` writes `testdata/svm.json`
(`go run ./cmd/fixtures > testdata/svm.json`) with the host's
`fixture.FindProgramAddress` as the oracle. Its authority and one seed are
chosen so the canonical bumps are 252 and 250: the on-chain search skips
on-curve addresses, each attempt costing 1,500 CU. A valid but
noncanonical bump is refused and the creation rolls back.

```sh
gosvm test --svm
```

Root tests omit this standalone module; test it with `GOWORK=off go test ./...`.
The fixture seeds are local test identities.
