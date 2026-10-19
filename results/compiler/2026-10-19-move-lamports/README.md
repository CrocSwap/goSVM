# Direct lamport moves: `solana.MoveLamports` (2026-10-19)

Question: can a goSVM program change balances directly, as Rust programs do
with `**from.lamports.borrow_mut() -= amount; **to.lamports.borrow_mut() +=
amount` (Hyperlane's Sealevel mailbox `claim_protocol_fees` and gas paymaster
`claim`), with the runtime's rules checked before anything changes?

Compiler: branch `gap1-move-lamports` (parent `79cf25c`); Go 1.22.0;
platform-tools v1.57; runners 0.5.1 (fogo-v0) and 0.6.0 (solana-v3). Local
only (`network_validation: false`).

## Design

`solana.MoveLamports(c, from, to, amount) uint64` ([SDK2](../../../docs/SDK2.md#rent-and-account-lifecycle)):

| Check, in order | Status |
| --- | ---: |
| `from` in range, writable, non-executable, owned by the program | 2005 / 2011 / 2012 |
| `to` in range, writable, non-executable (any owner, existing or not) | 2005 / 2011 |
| `from`'s balance at least `amount` | 2020 (new) |
| `to`'s balance plus `amount` fits in 64 bits | 2014 (now also credits) |

Then both balances change; the total is preserved. Within one account (the
same index or a duplicate descriptor) nothing changes, even at the maximum
balance. Checks apply to a zero amount too. Rent exemption is the
application's: the runtime rejects a transaction that leaves the source below
its minimum. The C runtime (`internal/compiler/lamports.c.txt`) is emitted only
for programs that call it; duplicate descriptors share one balance word.

## Results

| Check | Command | Result |
| --- | --- | --- |
| Native rules, statuses, atomicity, duplicates | `go test ./solana -run TestMoveLamportsRules` | pass |
| Native Go vs generated C, under UBSan | `go test ./internal/compiler -run TestSDK2MoveLamports` | 1,580 vectors match; runtime emitted only when called; SDK 1 rejects the call |
| SBF, both targets | `gosvm test --svm` in [examples/fee-vault](../../../examples/fee-vault/README.md) | 13 scenarios, 16 transactions pass on fogo-v0 and solana-v3 |
| Full-swap reference ELFs | `-no-cache` builds, both targets | `a66c5a97…` / `b419245b…`, unchanged |
| Example modules | `GOWORK=off go test ./...` in tutorial, escrow, full-swap, fee-vault | pass (snapshots refreshed) |
| Root suite | `GOTOOLCHAIN=go1.22.0 go test -timeout 90m -ldflags=-linkmode=external ./...` | pass except `TestStdlibAPI`, which reads `$GOROOT/api` files the downloaded Go 1.22.0 toolchain does not have (environment) |

The SVM scenarios: a claim of everything above the rent-exempt minimum (and a
second claim moving nothing), a fresh vault with nothing to claim, a claim to
another account (6007), credits to an unowned wallet, the signer and a new
address, a move within the vault, insufficient balance (2020), debits from an
unowned wallet and from the signer's wallet (2012), a readonly destination
(2011), a missing index (2005), the wrong authority (6007), and a debit below
the rent-exempt minimum (`InsufficientFundsForRent`, rolled back).

Not asserted: crediting a new account below its rent-exempt minimum is rejected
by the solana-v3 runner (LiteSVM 0.13.0) but accepted by the fogo-v0 runner
(LiteSVM 0.8.2); the fixture is shared, so it is left out. A credit overflow
needs more lamports than exist and is covered natively only.

| Scenario (step) | fogo-v0 CU | solana-v3 CU |
| --- | ---: | ---: |
| claim-fees-above-rent (1) | 4,206 | 4,206 |
| claim-fees-above-rent (2) | 4,206 | 4,206 |
| create-then-claim-nothing (1) | 15,572 | 15,518 |
| create-then-claim-nothing (2) | 4,206 | 4,206 |
| claim-to-another-account-fails (1) | 1,915 | 1,915 |
| move-to-any-writable (1) | 4,256 | 4,256 |
| move-to-any-writable (2) | 4,256 | 4,256 |
| move-creates-a-wallet (1) | 4,256 | 4,256 |
| move-within-vault (1) | 4,251 | 4,251 |
| move-more-than-balance-fails (1) | 3,688 | 3,688 |
| move-from-unowned-wallet-fails (1) | 3,395 | 3,395 |
| move-from-signer-wallet-fails (1) | 3,395 | 3,395 |
| move-to-readonly-fails (1) | 3,681 | 3,681 |
| move-to-missing-index-fails (1) | 3,676 | 3,676 |
| only-the-authority-moves (1) | 2,569 | 2,569 |
| move-below-rent-fails (1) | 4,256 | 4,256 |

Most of each step is the generated adapter (account checks, state decode and
write-back); `Claim` also reads the Rent sysvar. ELF SHA-256: fogo-v0
`651f1dd4…`, solana-v3 `9e9d9f0d…` (19,120 and 17,992 bytes).
