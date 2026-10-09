# Redeploy (upgrade) on Solana devnet and Fogo testnet — October 9, 2026

`gosvm deploy` now upgrades an existing program in place, at the same address,
when the payer is its upgrade authority. These runs used the
[network probe](../2026-10-09-go-bend-rust-probe/README.md) programs.

| Step | Solana devnet (SBPFv3) | Fogo testnet (SBPFv0) |
| --- | --- | --- |
| Redeploy identical build | no transactions ("already runs this exact build") | same |
| Larger changed build (refusal 200→201, new path returning 777) | program data extended by 10,240 bytes, buffer written, `Upgrade`; calls returned 201 and 777, accepted path unchanged | extended by 10,240 bytes, upgraded; returned 201 |
| Revert to the original, smaller build | upgraded without extension; refusal back to 200 | same |
| Redeploy identical build | no-op | no-op |

The first devnet revert attempt failed at the RPC level after creating its buffer,
leaving 0.081549 SOL in buffer `2Tb8KRi83sxtKqs71JMmHUuh6UB67Mqav9QhKF8ncG5J`. Two
changes followed: transactions are resubmitted on transient RPC errors (chunk
writes are re-signed with a fresh blockhash, since writes are idempotent), and every
deploy first closes leftover buffers whose authority is the payer. The next devnet
run reclaimed that buffer, and the payer balance rose by the same amount.

`solana-devnet.json` and `fogo-testnet.json` are the deploy records. Each holds the
latest deployment, with earlier ones under `history`. Final state: both programs
run the original probe build (ELF SHA-256 matches the first record's).
