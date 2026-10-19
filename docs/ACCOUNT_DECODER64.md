# Opt-in 64-account decoder

October 19, 2026. Hyperlane's Sealevel cross-collateral `transfer_remote`
takes 33 accounts, one more than the SDK-2 loader's 32
([decoder extension](ACCOUNT_DECODER32.md)), which returns 1001 before the
handler runs.

Low-level SDK-2 builds can now opt in to **64 incoming descriptors**,
including duplicates: `gosvm -max-accounts 64 ...` (`BuildOptions.MaxAccounts`,
`Program.MaxAccounts = compiler.WideAccounts`). The default is unchanged: 16
for SDK 1 and target-less programs outside an SDK-2 module, 32 for SDK 2.
SDK 1 rejects the option; a target-less (SDK 0) build with it gets the SDK-2
loader. More than 64 still returns 1001. Programs without the option compile
to the same C and ELFs, and their CU is unchanged.

## Why opt-in, and the frame

The loader copies each descriptor into a 56-byte `AccountInfo` table in the
entrypoint's frame, the array `sol_invoke_signed_c` takes for CPI. At 64
entries the table is 3,584 bytes of a 4,096-byte SBF frame. `Process` is
normally inlined into the entrypoint, and then the frame overflows: 4,224
bytes for the tutorial and 4,672 for the escrow, a build error. With the
option, the entrypoint calls `Process` through a `noinline` trampoline
(`gosvm_run`, `wideTrampoline` in `internal/compiler/solana.go`), so `Process` gets a frame of
its own (and no longer shares one with the table). Sixty-four entries is the
ceiling for a table on the stack; a larger one would need heap storage, which
would change the contracted heap accounting.

Enabling it for every SDK-2 program costs that call in every transaction and
changes every SDK-2 ELF, so it is not the default. Measured with the existing
fixtures, the same programs built with the option and run with
`gosvm test --svm -elf`:

| Program (steps) | fogo-v0 CU change, min / median / max | solana-v3 |
| --- | --- | --- |
| tutorial (9) | +4 / +5 / +6 | +4 / +5 / +6 |
| escrow (42) | −1 / +6 / +11 | −1 / +6 / +11 |
| full swap (15) | +1 / +2 / +4 | +1 / +2 / +4 |

Every error, account state and rollback is unchanged.

## Limits that remain

- Schema-2 instructions still declare at most 16 accounts and generated
  handlers check the exact count (6000); the option is for low-level `Process`
  programs, which read extra accounts by index.
- Outgoing CPI supports 64 metas and 4 signer groups (`cpi.Metas`); CPI
  passes the whole table, so a callee can use any of the 64 accounts.
- A legacy transaction holds at most 33 unique accounts for one small
  instruction with the payer as its only signer (1,228 of 1,232 bytes for the
  test's instruction). Transactions with more unique accounts need address
  lookup tables, which the SVM fixtures do not implement; 64 descriptors are
  tested with duplicate keys.
- The SVM fixture harness's per-instruction reference limit was 32; it is now
  255 ([fixtures](SVM_FIXTURES.md)).

## Verification

- `TestSDKAccountDecoderCapacityAndAliases` runs the serialized loader under
  undefined-behaviour sanitization for SDK 1 (16), SDK 2 (32) and the option
  (64): 272 cases each, counts up to 65 and 2^64−1, duplicate chains,
  malformed forward references, an alias of index 40 at index 63.
- `TestMaxAccountsOptIn` checks loader selection and rejects SDK 1 and other
  limits.
- `TestAccounts64Frames` builds the tutorial and escrow with the option for
  both targets.
- `TestAccounts64SBF` builds [examples/accounts64](../examples/accounts64/README.md)
  for both targets with and without the option and runs its committed
  fixtures: 33 unique accounts, 64 descriptors (aliases at indices 32–63), 65
  rejected (1001), a System transfer through program, payer and destination
  at indices 63, 33 and 40, and rollback after writes; without the option, 32
  pass and 33 return 1001.

[Results](../results/compiler/2026-10-19-accounts64/README.md).
