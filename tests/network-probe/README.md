# Network probe: Go + Bend + Rust on a public cluster

A schema-2/SDK-2 project (`gosvm new -schema 2 -sdk 2 -module example.com/probe probe`)
with one added instruction (`instruction.json`: the payer as its only signer) and
`model.ProbeArgs {Enabled byte; Value uint32; Reserve uint32; Amount uint64}`.
`Probe` asks Bend's `mixed.evaluate` to admit `Value`, has the Rust `quote` crate
price `Amount` against (admitted, `Reserve`), and returns the 8-byte quote as
return data, so no state account is needed. Errors: 100+Bend status, 200 Bend
refusal, 300+Rust status.

Instruction data (from the generated client; see `probe_test.go.txt`):

| Case | Data (hex) | Expected |
| --- | --- | --- |
| accepted | `80896b050708dd82010a000000d00700006400000000000000` | return `f906000000000000` (1785) |
| Bend refusal | `80896b050708dd820129000000d00700006400000000000000` | custom error 200 |
| Bend malformed | `80896b050708dd820201000000d00700006400000000000000` | custom error 104 |
| Rust malformed | `80896b050708dd820101000000000000006400000000000000` | custom error 304 |

```sh
gosvm deploy -network solana-devnet      # and fogo-testnet; fund the printed payer
gosvm call -network solana-devnet -data <hex>          # simulate
gosvm call -network solana-devnet -data <hex> -send    # commit
```
