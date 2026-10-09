# Upgrade authority on Solana devnet and Fogo testnet — October 9, 2026

`gosvm authority -network N` shows, transfers or removes a program's upgrade
authority. `gosvm deploy -authority key.json` upgrades as a non-payer authority;
the payer still pays fees and any extension rent.

| Check | Solana devnet | Fogo testnet |
| --- | --- | --- |
| Show current authority | ✓ | ✓ |
| `-set new.json` (SetAuthorityChecked; the new key co-signs) | ✓ | ✓ |
| Deploy as the old payer is refused, naming the actual authority | ✓ | ✓ |
| `deploy -authority new.json` upgrades; changed code live (201) | ✓ | ✓ |
| Revert upgrade as the new authority; original code live (200) | ✓ | ✓, see note |
| Transfer back to the payer (`-authority new.json -set payer.json`) | ✓ | ✓ |
| `-set-address` without `-unchecked` is refused (no transaction) | ✓ | — |
| `-set-address ADDR -unchecked` transfers; checked transfer back | ✓ (throwaway program) | ✓ (throwaway) |
| `-immutable` without `-confirm <program id>` is refused | ✓ | — |
| `-immutable -confirm <id>`: authority removed | ✓ `938KLVtR…` | ✓ `Nn9ULfqF…` |
| Deploy to an immutable program is refused | ✓ | ✓ |

Immutability was tested on separate throwaway starter programs. The network probe
programs keep their payer as upgrade authority and run the original probe build.
Records: `probe-*.json` (with `authority_changes` and upgrade `history`) and
`immutable-*.json`.

Note: the first Fogo revert attempt stalled because the laptop lid was closed
(macOS "Clamshell Sleep" at 16:03 EDT, per `pmset -g log`). Go's timers pause during
sleep. After waking, the next deploy reclaimed the interrupted buffer
(0.118355 FOGO). The revert completed as the payer once authority was back.

Robustness added during this work: RPC calls retry transport errors and HTTP
429/5xx, and `gosvm authority` (show) resynchronizes the deploy record with the
chain after an interrupted change. The devnet immutable change succeeded on chain
while the RPC connection was reset, which is why this resync exists.
