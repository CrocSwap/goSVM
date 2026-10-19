# Fee vault: moving lamports directly

A schema-2 SDK-2 program that holds fees in a program-owned PDA and pays them
out with `solana.MoveLamports`, the direct balance change Hyperlane's Sealevel
mailbox and gas paymaster make in `claim_protocol_fees` and `claim`
(`**vault.lamports -= amount; **beneficiary.lamports += amount`).

- `Create` makes the vault at the PDA `["vault", authority, bump]` and records
  its authority and beneficiary.
- `Claim` moves every lamport above the vault's current rent-exempt minimum
  (`system.MinimumBalance`) to the recorded beneficiary.
- `Move` passes two account positions and an amount straight to
  `MoveLamports`, so the fixtures can try every rule: a debit needs an owned,
  writable, non-executable source; a credit needs a writable, non-executable
  destination, owned or not, existing or not.

`cmd/fixtures` writes `testdata/svm.json`
(`go run ./cmd/fixtures > testdata/svm.json`): claims, credits to a wallet, the
signer and a new address, a move within one account, each SDK error, and a
debit below the rent-exempt minimum that the runtime rejects. Rent exemption
is the application's check, as in Hyperlane, which claims only the excess.

```sh
gosvm test --svm
```

Root tests omit this standalone module; test it with `GOWORK=off go test ./...`.
The fixture seeds are local test identities.
