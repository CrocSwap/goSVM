# Tutorial counter

The finished program from the [first-program tutorial](../../docs/TUTORIAL.md):
a per-owner counter with a limit, scaffolded by `gosvm new -schema 2 -sdk 2
-module example.com/counter counter`.

- `Create` makes the owner's counter at the PDA `["counter", owner, bump]`;
  generated code checks the accounts and the PDA and creates the rent-exempt
  57-byte account, paid by the owner.
- `Increment` adds to the count; only the recorded owner can sign.
- `Preview` applies the same rule to its arguments and returns the new count
  as return data. Its only account is a signer, so `gosvm call` can invoke it.

`model/` holds the state, arguments, error codes and the `Next` rule, shared by
the program and the off-chain code. `cmd/counter` is an ordinary Go client for a
deployed counter; `solrpc/` is the small transaction/RPC helper it uses.
`cmd/fixtures` writes `testdata/svm.json` with the SDK's fixture builder,
`gosvm/sdk/fixture`, and the generated client
(`go run ./cmd/fixtures > testdata/svm.json`).

From this directory, with the CLI and toolchain installed:

```sh
gosvm check
gosvm test
gosvm test --svm
gosvm deploy -network solana-devnet -check
```

Root tests omit this standalone module; test it with `GOWORK=off go test ./...`.
The fixture seeds are local test identities. The tutorial's devnet run is
recorded in [results/svm/2026-10-18-release-devnet](../../results/svm/2026-10-18-release-devnet/README.md).
