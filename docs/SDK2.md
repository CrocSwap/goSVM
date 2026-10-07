# Experimental SDK 2

SDK 2 adds checked classic SPL Token references, bounded multi-seed PDA builders,
generic CPI marshaling, current Clock/rent/lamport reads, return-data setting and bounded lifecycle operations.
Select it explicitly:

```sh
gosvm new -schema 2 -sdk 2 -module example.org/my/program my-program
```

The manifest records `sdk_version: 2`. The project receives a pinned local SDK
snapshot, including `gosvm/solana`, `gosvm/sdk/pda`, `gosvm/sdk/cpi` and
`gosvm/sdk/token` and `gosvm/sdk/system`. Native Go and the compiler select that snapshot's API.
Generation checks its contents and rejects unexpected Go files or drift.
SDK 1 remains the default and retains its original API/runtime; existing projects
are not upgraded automatically. Schema 1 cannot select SDK 2.

## Interfaces and bounds

`solana.SetReturnData(c, data)` sets up to 1,024 exact bytes, returning 0 on
success or 2017 for an oversized payload. Empty data clears the current bytes.
Set final payloads after CPIs, which can clear or replace prior return data.
Native contexts supply `WriteReturnData func(data []byte)` and receive an owned
copy; an absent callback returns 2009. Native invocation adapters model program
identity, clearing and reset semantics. SDK 1 has no setter. See the
[Phoenix compiler/return-data handoff](PHOENIX_DEPENDENCIES.md) for actual v0/v3
simulation/submission proofs, new frontend/SDK pins and runner observability.

`pda.Seeds` supports `AddBytes`, `AddKey`, `AddByte`, `AddUint32` and `AddUint64`.
Integer seeds use little-endian encoding. Each builder accepts at most 16 seeds
of at most 32 bytes each, including any bump. `Address` derives an address using
an explicit program key; `Matches` compares it with a context account key.
These methods use supplied seeds; a bump-search convenience API is not provided.

`cpi.Metas.Add(index, writable, signer)` builds at most 16 account metas.
`cpi.Signers.Add(&seeds)` copies at most two seed groups into a 1,024-byte buffer;
the combined encoding must fit that buffer even if both groups separately fit.
`cpi.Invoke` accepts an executable program's context index, metas, at most 10,240
instruction bytes, and optional signer groups. Generic CPI uses explicit account
indices and does not itself implement application owner, relationship or alias
policy. The SDK-2 account loader accepts up to 32 incoming account descriptors,
including duplicates; SDK 1 remains capped at 16. Outgoing CPI metas and schema-2
declared accounts retain their separate 16-account bounds. Custom handlers can
accept extra incoming accounts, while generated handlers retain their declared
count checks. See the [decoder extension and new pin](ACCOUNT_DECODER32.md).

`cpi.SingleSigner` encodes one signer group directly, avoiding the intermediate
`pda.Seeds` to `cpi.Signers` copy. It offers `AddBytes` and `AddByte`, with the
same maximum of 16 seeds, each at most 32 bytes including the explicit bump.
Its maximum encoded size is 530 bytes; failed additions return seed error 3001
without changing the builder. `cpi.InvokeSingle(c, program, &metas, data, &signer)`
uses the existing invoke boundary and propagates its statuses unchanged. Nil
metas return 3002; a nil signer invokes without PDA signing, matching `Invoke`.
A nonnil empty builder encodes one empty group (`[1, 0]`). Keep the borrowed
`Bytes` backing value alive and unmodified through invocation. Use the general
builder for two groups. See the [adoption evidence](COMPACT_SIGNER_ADOPTION.md).

Builders use caller-owned scalar arrays. `Bytes` returns a borrowed view that
must not escape its backing value. Failed additions leave builders unchanged.
There is no allocation on chain. These bounds are the SDK's experimental
contract, not a claim to cover all transactions accepted by Solana.

`token.Load(context, index, writable)` returns an opaque checked `token.Account`.
It checks the classic Token owner, non-executable flag, exact 165-byte layout,
initialized/frozen state, non-native account marker and required privileges.
`token.Read` validates the reference's current identity/data again and returns
mint, authority, amount and frozen state. It rereads current account bytes after
CPI rather than retaining a decoded balance snapshot.
`token.Balance` revalidates the same account contract while decoding only its
current amount. Transfers validate current token bytes without copying unused
decoded state. These changes retain identity, owner, privilege and layout checks.

`token.Transfer` and `TransferSigned` check writable references, matching mints,
source authority and the classic executable Token program, then invoke the real
Token program. Unsigned transfers require the authority's transaction signature;
signed transfers accept a PDA seed builder. Token errors propagate unchanged,
including insufficient funds, overflow and frozen-account errors. This is a
checked subset, not a complete decoder of every SPL account field. Token-2022,
wrapped native SOL, delegate-authorized transfers and multisig are unsupported.

`solana.Lamports` reads the current runtime balance. Native contexts provide
`InvokeInstruction` and `DeriveAddress` callbacks for these new operations;
the compiler uses runtime syscalls instead. Native `solana.Account` construction
is a testing facility, not an on-chain value type. Context remains opaque on chain.

## Token account lifecycle

`token.Create(c, systemProgram, tokenProgram, payer, account, mint, authorityKey)`
creates an empty non-native classic-token account: rent-funded System allocation
of 165 bytes, then SPL `InitializeAccount3` with the supplied `[32]byte` authority.
`CreateSigned` takes an additional `*pda.Seeds` for the new account's PDA under the
caller; the payer remains a transaction signer. It validates the exact initialized
82-byte classic mint and canonical mint COption tags before allocation, rejects
the wrapped-SOL native mint, and rereads the created account after initialization.
System/SPL failures propagate. Native callbacks do not roll back a successful
allocation if a subsequent initialization fails; the SBF transaction does.

`token.Close(c, tokenProgram, source, refundIndex, authorityIndex)` takes a checked
writable token reference. `CloseSigned` adds explicit PDA signing seeds. The
helper revalidates current key/owner/layout/privileges, a distinct writable
non-executable refund account and the current effective close authority. A valid
explicit close-authority COption overrides the token owner; otherwise the owner
must match the signer. Frozen empty accounts may close. SPL checks nonzero token
balance and refund overflow and returns its actual errors (11/14 in the tested
runtime). Close does not transfer a remaining token balance automatically.
System/incinerator token ownership's special burning path, wrapped SOL and
multisig are outside this helper contract. After a successful close, the old
checked reference fails subsequent reads because its storage is no longer a
token account. Read current views after CPIs rather than retaining data slices.

The [creator-managed swap](../examples/full-swap/README.md) uses signed vault
creation and draining/closing through these APIs. Native tests additionally
check instruction/meta encoding, preconditions, close-authority selection and
propagated failures. The new helper is part of the pinned SDK-2 snapshot; adopt
a matching frontend and complete SDK snapshot rather than adding it to an old CLI.

## Runtime Clock

`solana.Clock(context, output)` writes exactly 40 bytes from the current runtime
Clock sysvar. The five little-endian words are slot, epoch-start timestamp bits,
epoch, leader-schedule epoch, and Unix timestamp bits. Signed timestamps retain
their two's-complement bit representation; the API does not reject negative time
or enable signed Go arithmetic. Applications can decode the final word as uint64
and check its high bit before accepting it as a nonnegative timestamp.

Wrong output length returns 2016 without calling the syscall. Failed syscall
statuses propagate and preserve the output. Native contexts use a `ReadClock`
callback; a missing callback returns 2009. Output is first read into temporary
storage, so partially written failed native callbacks cannot corrupt the caller's
buffer. The SBF adapter uses the actual Clock syscall and serializes the words
explicitly; no caller-supplied timestamp replaces the runtime read.

[Clock validation](../results/compiler/2026-10-05-clock2-supported/README.md) covers
1,010 native-Go/generated-C vectors under undefined-behavior sanitization and
12 SBF scenarios/15 transactions in three runs. Signed timestamp extremes, large
unsigned words, fresh reads after controlled changes, negative/regressing time,
unchanged time and rollback are tested. The example's uint64 reward calculation
is a syscall-consumer test, not a verification of Orca's U128 reward arithmetic.

Clock requires the updated frontend and SDK snapshot together. The new API SHA
is `07f4f3f188bdadd803b1fa7d5350b2216810f5aefba74431d6d964fa5dd25012`.
It is not compatible with a frozen SDK lacking Clock; create a fresh SDK-2
scaffold with the matching CLI and preserve the previous pin/results separately.
SDK 1 remains frozen.

## Rent and account lifecycle

`system.MinimumBalance(context, size)` reads the current Rent sysvar and returns
`(lamports, status)`. It does not assume the default rate or exemption threshold.
`MinimumBalanceFor(rate, thresholdBits, size)` performs the same pure calculation
using IEEE-754 threshold bits. Integer operations reproduce the pinned
`solana-rent 3.0.0` conversion/multiplication rounding and saturating final cast,
without floating-point SBF runtime helpers. Sizes above 10 MiB, a uint64 overflow
in `(128+size)*rate`, and negative/nonfinite thresholds return `ErrRent` (3020).
Both signed zeros are accepted. The integer-overflow rejection is an intentional
bounded policy; it does not reproduce Rust release-mode wrapping of that product.

`solana.Rent(context, output)` exposes the raw runtime values as exactly 17 bytes:
little-endian uint64 rate, little-endian uint64 threshold bits, and one burn byte.
It preserves output on failure. Native contexts supply a `ReadRent` callback with
that encoding; a missing callback returns 2009. Neither the burn percentage nor
an off-chain constant determines the exemption minimum.

`system.Create(context, systemIndex, payerIndex, targetIndex, size, owner)` invokes
the real System program to allocate and fund an empty, unfunded System target.
Payer and target must be distinct writable, non-executable transaction signers;
the payer must have no data and be System-owned. The supplied System account must
have the zero public key and be executable. Funding is the current rent minimum,
with a one-lamport floor so a free-rent target survives account cleanup. Initial
allocation is at most 10,240 bytes. `CreateSigned` takes a final `*pda.Seeds`, checks
the target PDA under the calling program, and supplies its signer seeds; the payer
still needs a transaction signature. It does not search for a bump or support
already-funded targets. Real System failures propagate unchanged, including
insufficient payer funds. `system.Transfer` uses the same checked System payer
contract to fund a distinct writable destination, including an existing owned
account before growth.

`solana.ResizeAccount(context, index, newLength)` requires a writable,
non-executable account owned by the calling program. It updates the serialized
length and all duplicate descriptors. Newly exposed bytes are zeroed, including
after shrinking and regrowing. Growth cannot exceed the invocation's original
length by 10,240 bytes, and total length cannot exceed 10 MiB. The loader records
the original length once; shrinking or an earlier CPI cannot reset that limit.
The caller must fund any additional rent explicitly. A successful resize that
violates transaction rent requirements still fails in the VM and rolls back.

`solana.CloseAccount(context, sourceIndex, refundIndex)` requires an owned writable,
non-executable source and a distinct writable, non-executable destination. It
checks refund overflow before mutations, transfers all lamports, truncates the
source data and assigns the System owner. Current balances/lengths/owners reflect
the change through duplicate references. The source can subsequently be recreated
with appropriate signing authority, including within the same invocation. A
zero-lamport closed account is absent after commit; simulation represents its
zero balance, empty data and System owner separately.

These boundary operations do **not** decide application authorization. Handlers
and generated policies must validate the creator/admin, payer, recipient and
account relationships before moving funds or closing state. Checked SPL Token
closing is now available through the lifecycle helpers above.
[Generated init/close policy](SCHEMA2.md) implements
explicit payer, signer authority and refund declarations for owned state. Do not reuse old
data or owner views after operations that can resize/replace/close storage; read
current views again. Native tests set each account's `OriginalDataLen` to its
invocation-entry length and preserve it across CPI callbacks.

System helper errors are 3021 (program), 3022 (account/alias/layout/privilege),
3023 (signer/PDA authority), and 3024 (initial allocation size), in addition to
3020 and propagated boundary/downstream errors. Owned lifecycle errors are
2005 (index), 2010 (size/growth), 2011 (privilege/executable), 2012 (owner),
2013 (self refund/malformed native key), 2014 (refund overflow), and 2015 (Rent
output length). These are experimental API codes, separate from schema-2 errors.

## Validation and open work

The [checked-token fixture](../examples/checked-token/program.go) and
[evidence](../results/compiler/2026-10-05-checked-token-lifecycle-final/README.md)
cover multi-seed derivation, two signer groups, real classic Token/System CPIs,
current balances, exact errors and transaction rollback. The proof passes
116 native vectors, 1,011 native-Go/C CPI-boundary vectors and 117 compiled-SBF
scenarios in three repetitions. The fixture's largest static frame is 3,648 bytes;
measure stack use for new applications rather than assuming ample headroom.

The [lifecycle boundary proof](../results/compiler/2026-10-05-lifecycle2-supported/README.md)
passes 35 scenarios/46 transactions in three real-SBF repetitions, plus 100,000
random ordinary-Go rent comparisons, 1,000 generated-C rent vectors and 1,035
native/C lifecycle vectors under undefined-behavior sanitization. It checks actual
System creation/signing/funding, current nondefault rent, shrink/regrow zeroing,
10 KiB boundaries, duplicate descriptors, close/recreate, free-rent account
retention, refund overflow and transaction rollback. Its largest static frame is
1,664 bytes. The manual token baseline rebuild remains byte-identical. This is
an SDK fixture rather than generated application lifecycle policy.
[Generated token/PDA constraints](SCHEMA2.md) and the
[preloaded full swap migration](../examples/full-swap/README.md) are now implemented.
The [full swap proof](../results/compiler/2026-10-05-framework-swap-lifecycle-final/README.md)
also covers checked balance reads and transfers after the copying optimizations.
[Generated rent/payer/authorized-close rules](SCHEMA2.md) now pass the distinct
[SOL escrow proof](../results/compiler/2026-10-05-escrow-supported/README.md), starting
with unallocated state and exercising claim/cancel, reuse and rollback. The swap
preserves its original preloaded instruction and now also has a complete
[creator-managed lifecycle](../results/compiler/2026-10-06-managed-swap-supported/README.md).
The same expanded ELF passes the [legacy corpus/service proof](../results/compiler/2026-10-06-framework-swap-managed/README.md).
New checked-token, swap/service and escrow regressions also pass with the
optional compact signer helper and reproduce their previous ELF hashes.
Comparative framework measurements are complete; the independent developer
trial remains open in the [milestone audit](MILESTONE2_ACCEPTANCE.md).
SDK-helper revisions require a matching frontend and complete SDK snapshot,
even when the low-level API-file hash is unchanged.
