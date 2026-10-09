# Go+Bend+Rust on Solana devnet and Fogo testnet — October 9, 2026

**One Go program calling a pure Bend library and a pure Rust library was deployed
to both public test networks and returned identical results.** This is the first
GoSVM public-network validation. It was approved by the user, using throwaway
payers created by gosvm.

| Network | Profile | Program ID | ELF bytes | Deploy signature |
| --- | --- | --- | ---: | --- |
| Solana devnet | solana-v3 (SBPFv3) | `Db91CBr77NWLrQJ83ud2h3nL1AtEYrF1EftwKzznsVpp` | 15,888 | `3JbK1bPP…xP6qsWtEPxn4xfF` |
| Fogo testnet | fogo-v0 (SBPFv0) | `G1aZmYxscrVjKg7Mw7u4PForusZoWCtn2Ry9N5vFtThW` | 16,840 | see `fogo-testnet.json` |

The program is the [network probe](../../../tests/network-probe/README.md):
Bend's `mixed.evaluate` admits a value, the Rust `quote` crate prices an amount
against it, and Go returns the 8-byte quote as return data. Bend pins: fogo-v0
`ec18012f…` (v1.53) and solana-v3 `9db669ce…` (v1.57, published October 9); the
Rust crate was built by GoSVM's pinned v1.57 producer.

Committed transactions (`calls/*.json` holds signatures, logs and full output):

| Case | Expected | Solana devnet | Fogo testnet |
| --- | --- | --- | --- |
| accepted | return `f906000000000000` (1785) | ✓, 650 CU | ✓, 666 CU |
| Bend refusal | custom error 200 | ✓, 278 CU | ✓, 292 CU |
| Bend malformed input | custom error 104 | ✓, 232 CU | ✓, 246 CU |
| Rust malformed input | custom error 304 | ✓, 366 CU | ✓, 382 CU |

Before deploying, `gosvm deploy` read each cluster's live feature accounts. The
SBPFv3 gate `5cC3foj…` is active on devnet. Fogo testnet runs `solana-core` 20.0.0
with no v1–v3 gates. Deployment used GoSVM's native upgradeable-loader path
(buffer, chunked writes, `DeployWithMaxDataLen`): Solana CLI 3.0.15 cannot parse
SBPFv3 ELF headers locally. Payer keys stay in the ignored `build/network-probe`.

Scope: two test networks and one probe program. Mainnets, other programs, CPI
and account-heavy flows on public networks remain unvalidated. CU differs between
networks because runtimes and ISAs differ; it is not a matched comparison.
