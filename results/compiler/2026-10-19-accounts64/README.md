# Opt-in 64-account decoder (2026-10-19)

Question: can an SDK-2 program take more than 32 incoming accounts (Hyperlane's
Sealevel cross-collateral `transfer_remote` takes 33) without making the common
case cost more?

Compiler: branch `gap2-accounts64` (parent `79cf25c`); Go 1.22.0;
platform-tools v1.57; runners 0.5.1 (fogo-v0) and 0.6.0 (solana-v3). Local
only (`network_validation: false`). Design: [ACCOUNT_DECODER64](../../../docs/ACCOUNT_DECODER64.md).

## Raising the default first

With `GOSVM_MAX_ACCOUNTS` simply 64 for SDK 2, the full swap built (its
`Process` is not inlined) but the tutorial and escrow did not: `Process`
inlined into the entrypoint beside the 3,584-byte table needs 4,224 and 4,672
bytes of a 4,096-byte frame. Calling `Process` through a `noinline`
trampoline fixes that, at the cost below, and changes every SDK-2 ELF
(full swap `a66c5a97…` → `69cb4ed5…` on fogo-v0). So the 64-entry table is
opt-in (`-max-accounts 64`) and the default stays 32.

## Results

| Check | Command | Result |
| --- | --- | --- |
| Serialized loader, UBSan | `go test ./internal/compiler -run TestSDKAccountDecoderCapacityAndAliases` | 272 cases each for SDK 1 (16), SDK 2 (32) and the option (64) |
| Selection and validation | `-run TestMaxAccountsOptIn` | wide table only when opted in; SDK 1, 32 and 65 rejected |
| Frames | `-run TestAccounts64Frames` | tutorial and escrow build with the option on both targets |
| SBF, both targets | `-run TestAccounts64SBF` | with the option: 5 scenarios pass; without: 2 (32 pass, 33 → 1001) |
| CLI | `gosvm -target T -max-accounts 64 -o … .` then `gosvm svm-test -target T -elf … -fixtures examples/accounts64/testdata/svm.json`, in a module with the SDK-2 snapshot | same results and CU as the test, both targets |
| Default C | `-run TestCorpusMatchesPreCheckCompiler` | frozen corpus C unchanged: the templates are untouched and the 64-entry variant is substituted only for opted-in programs |
| Default ELFs | `gosvm build -no-cache` of tutorial, escrow, full swap, both targets | identical to `main` (`5bf3006a…`/`571ca8b3…`, `78847122…`/`aedab88b…`, `a66c5a97…`/`b419245b…`) |

[examples/accounts64](../../../examples/accounts64/README.md), with the option:

| Scenario | fogo-v0 CU | solana-v3 CU |
| --- | ---: | ---: |
| thirty-three-unique (payer + 32 owned, 1,228-byte packet) | 15,938 | 15,938 |
| sixty-four-with-duplicates | 52,231 | 52,231 |
| sixty-five-fails (1001) | 5 | 5 |
| system-transfer-beyond-32 (program 63, payer 33, destination 40) | 29,678 | 29,644 |
| failure-rolls-back | 52,354 | 52,354 |

Most of this is the example's own per-account loop (a 32-byte owner compare
for each descriptor). A target-less build outside an SDK-2 module defaults to
16 accounts; with the option it gets the SDK-2 loader and 64 (the CLI run
there costs 636 CU more for the transfer case: target-less builds emit
bytewise `memcpy`/`memset` helpers where SDK-2 builds copy words; the loader
is the same).

Cost of the option on existing programs: each built with `-max-accounts 64`
and run against its committed fixtures with `gosvm test --svm -elf`, compared
with the default build:

| Program | Target | Steps | CU change min | median | max |
| --- | --- | ---: | ---: | ---: | ---: |
| tutorial | fogo-v0 | 9 | +4 | +5 | +6 |
| tutorial | solana-v3 | 9 | +4 | +5 | +6 |
| escrow | fogo-v0 | 42 | −1 | +6 | +11 |
| escrow | solana-v3 | 42 | −1 | +6 | +11 |
| full swap | fogo-v0 | 15 | +1 | +2 | +4 |
| full swap | solana-v3 | 15 | +1 | +2 | +4 |

Errors, account states and rollback are unchanged in every step.
