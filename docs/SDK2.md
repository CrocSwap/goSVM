# Experimental SDK 2

SDK 2 adds checked classic SPL Token references, bounded multi-seed PDA builders,
generic CPI marshaling, current Clock/rent/lamport reads, return-data setting and bounded lifecycle operations.
Select it explicitly:

```sh
gosvm new -schema 2 -sdk 2 -module example.org/my/program my-program
```

The manifest records `sdk_version: 2`. The project receives a pinned local SDK
snapshot, including `gosvm/solana`, `gosvm/sdk/pda`, `gosvm/sdk/cpi`, `gosvm/sdk/ata` and
`gosvm/sdk/token` and `gosvm/sdk/system`. Native Go and the compiler select that snapshot's API.
Generation checks its contents and rejects unexpected Go files or drift.
The snapshot also holds the host-only [fixture builder](SVM_FIXTURES.md#writing-fixtures-in-go)
`gosvm/sdk/fixture`, which programs never import. Snapshots from before it existed
remain valid; generation adds it. A new SDK-2 project includes a fixture generator,
`cmd/fixtures`, and its output `testdata/svm.json`, so `gosvm test --svm` runs
scenarios immediately.
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

`solana.GetReturnData(c, output, program)` reads the current return data, set by
this program or by the last program it invoked that set any. It returns the full
size (0–1,024; 0 when there is none) and copies `min(len(output), size)` bytes.
When it copies at least one byte it also writes the setter's 32-byte key into
`program`; otherwise both buffers are unchanged, as in the runtime syscall. A
size above `len(output)` means `output` holds a prefix. `program` must be
exactly 32 bytes; any other length traps. Check the setter before decoding: the
runtime clears return data before every invocation, including a silent CPI.
The copied output region and `program` must not overlap; an overlap traps. Native contexts supply
`ReadReturnData func() (data, program []byte)`; a nil callback reads as no
return data. The [return-data SBF test](../internal/compiler/return_data_sbf_test.go)
reads data set by real self-CPIs, the program itself, a silent callee and an
earlier instruction, with prefix copies and the trap, on fogo-v0 and solana-v3.

`solana.Secp256k1Recover(c, hash, recoveryID, signature, output)` recovers the
uncompressed secp256k1 public key (64 bytes, without the 0x04 prefix) behind a
64-byte `r||s` signature of a 32-byte hash, using the runtime's
`sol_secp256k1_recover` syscall. `recoveryID` is 0–3 (Ethereum's `v - 27`).
Wrong hash, signature or output lengths return 2018 without a syscall;
otherwise the runtime's status propagates unchanged: 2 for an invalid recovery
ID (including any above 255) and 3 for an invalid signature (zero or
out-of-range scalars, or no curve point). Failures leave `output` unchanged.
The runtime accepts high-s signatures, recovering the same key with the other
parity, and recovery does not authenticate anything by itself: compare the
result, or `keccak256(output)[12:]` for an Ethereum address, with the expected
signer. The syscall costs 25,000 CU. Native contexts supply
`RecoverSecp256k1 func(hash []byte, recoveryID uint64, signature []byte) ([]byte, uint64)`;
a nil callback returns 2009. The [secp256k1 SBF test](../internal/compiler/secp256k1_sbf_test.go)
compares recovered keys and statuses with an independent `math/big` oracle on

`solana.Keccak256(c, input, offset, length, output, outOffset)` hashes
`input[offset:offset+length]` with Keccak-256 as Ethereum does (the original
0x01 padding, not SHA3-256) into `output[outOffset:outOffset+32]`, using the
runtime's `sol_keccak256` syscall. Bounds and statuses match `SHA256`: 2003 for
a range outside `input` or `output`, checked before the syscall, and 2004 for a
native result that is not 32 bytes. Hash a concatenation, such as a Merkle node
`keccak256(left || right)`, from one buffer. The syscall costs 85 CU plus half
a CU per input byte on the pinned runners. Native contexts supply
`Keccak func(input []byte) []byte`; a nil callback returns 2009. The
[Keccak SBF test](../internal/compiler/keccak_sbf_test.go) compares whole and
partial inputs from 0 to 10,240 bytes with a reference implementation on
fogo-v0 and solana-v3.

`solana.Log(c, message)` writes `Program log: <message>`, as Rust's `msg!`
does, using the `sol_log_` syscall (the larger of 100 CU and the message
length). `solana.Log64(c, v0, v1, v2, v3, v4)` logs five words as
`0x1, 0x2, 0x0, 0x0, 0x0` (`sol_log_64_`, 100 CU) and `solana.LogPubkey(c, key)`
a 32-byte key in base58 (`sol_log_pubkey`, 100 CU); a key of another length
returns 2019 without logging. They return 0 otherwise, so a call is an
ordinary statement. Pass a string constant to `Log` to avoid allocating;
`string(b)` copies `b` to the heap. The runtime fails the instruction when a
message is not valid UTF-8 (`ProgramFailedToComplete`, with a `failed: invalid
utf-8 ...` log line); the native SDK panics there. Native contexts receive the
text after `Program log: ` through `WriteLog`; without it lines are discarded,
since logging has no state to observe. Programs that do not log compile to the
same C. See [examples/logging](../examples/logging/README.md).

`pda.Seeds` supports `AddBytes`, `AddKey`, `AddByte`, `AddUint32` and `AddUint64`.
Integer seeds use little-endian encoding. Each builder accepts at most 16 seeds
of at most 32 bytes each, including any bump. `Address` derives an address using
an explicit program key; `Matches` compares it with a context account key.
`FindAddress(c, program)` is Solana's `find_program_address`: it appends each
bump from 255 down to 1 (never 0, as in Solana) and returns the first
off-curve address with its bump, using one create-program-address syscall
(about 1,570 CU) per attempt, so on-curve results are skipped exactly. The
builder is unchanged; add the bump for signing. A builder with 16 seeds has
no room for the bump (3001), no bump found is 3004, and other syscall statuses
propagate. Store the bump and use `Address` where it is known; see
[examples/find-address](../examples/find-address/README.md). The host
`fixture.FindProgramAddress` now also stops at bump 1.

`cpi.Metas.Add(index, writable, signer)` builds at most 64 account metas
(`cpi.MaxMetas`); repeated accounts count. `cpi.Signers.Add(&seeds)` copies at
most four seed groups (`cpi.MaxSigners`) into a 1,024-byte buffer; the combined
encoding must fit that buffer even if each group separately fits.
`cpi.Invoke` accepts an executable program's context index, metas, at most 10,240
instruction bytes, and optional signer groups. Generic CPI uses explicit account
indices and does not itself implement application owner, relationship or alias
policy. The SDK-2 account loader accepts up to 32 incoming account descriptors,
including duplicates; SDK 1 remains capped at 16. Schema-2 declared accounts
retain their separate 16-account bound. Custom handlers can
accept extra incoming accounts, while generated handlers retain their declared
count checks. See the [decoder extension and new pin](ACCOUNT_DECODER32.md).
Low-level builds can opt in to 64 incoming descriptors with `-max-accounts 64`,
which runs `Process` in its own stack frame (a few CU per transaction); see the
[64-account decoder](ACCOUNT_DECODER64.md).

### CPI bounds

| Bound | goSVM | Runtime (Agave 3.0) |
| --- | --- | --- |
| Account metas per CPI | 64 (`solana.Invoke` 2006, `Metas.Add` 3002) | 255 (`MAX_ACCOUNTS_PER_INSTRUCTION`) |
| Distinct accounts (account infos) | the incoming accounts, at most 32 | 64, or 128 with `increase_tx_account_lock_limit` |
| Signer groups | 4 (2007, `Signers.Add` 3003) | 16 (`MAX_SIGNERS`) |
| Seeds per group, bytes per seed | 16, 32 (2007; `pda.Seeds` 3001) | 16, 32 (`MAX_SEEDS`, `MAX_SEED_LEN`) |
| Encoded signer groups | 1,024 bytes (2007, 3003) | none |
| Instruction data | 10,240 bytes (2008) | 10,240 |

Every meta indexes the invocation's own accounts and `Invoke` passes those as
the account infos, so a CPI names at most 32 distinct accounts, within every
feature-set's account-info limit; more than 32 metas repeat accounts.
64 metas cover Hyperlane's Sealevel CPIs (22 for IGP `PayForGas` with quoted
accounts, 17 for fee `QuoteFee`) with room for repeated references, and keep the
C runtime's descriptor arrays (16 bytes per meta and per seed) in one SBF frame:
`sol_Invoke` is never inlined and takes 2,240 bytes of its own frame, so callers'
frames are unchanged. The runtime's 255 would need 4,080 bytes of metas alone.

`cpi.Metas` keeps its first 16 records (`cpi.InlineMetas`) in the builder.
The 17th `Add` allocates one 576-byte array on the invocation heap, copies the
inline records there and continues in it; that is the only SDK allocation, it
counts toward the heap budget like any other (the out-of-memory trap of
[ERROR_CONTRACT](ERROR_CONTRACT.md#traps)), and a builder that stays within 16
metas allocates nothing. Copying a `Metas` value after its 17th record shares
the heap array between the copies; pass builders by pointer.

`cpi.SingleSigner` encodes one signer group directly, avoiding the intermediate
`pda.Seeds` to `cpi.Signers` copy. It offers `AddBytes` and `AddByte`, with the
same maximum of 16 seeds, each at most 32 bytes including the explicit bump.
Its maximum encoded size is 530 bytes; failed additions return seed error 3001
without changing the builder. `cpi.InvokeSingle(c, program, &metas, data, &signer)`
uses the existing invoke boundary and propagates its statuses unchanged. Nil
metas return 3002; a nil signer invokes without PDA signing, matching `Invoke`.
A nonnil empty builder encodes one empty group (`[1, 0]`). Keep the borrowed
`Bytes` backing value alive and unmodified through invocation. Use the general
builder for two to four groups. See the [adoption evidence](COMPACT_SIGNER_ADOPTION.md).

Builders use caller-owned scalar arrays, except `Metas` beyond 16 records (above).
`Bytes` returns a borrowed view that must not escape its backing value. Failed
additions leave builders unchanged. These bounds are the SDK's experimental
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

## Token and Token-2022 instructions

`token.Program2022ID()` is the Token-2022 address and `token.IsProgram(c, index)`
accepts the executable classic Token or Token-2022 program. The checked
references above (`Load`, `Read`, `Transfer`, `Create`, `Close`) remain classic
only. Instruction builders take the token program's context index and work with
either program: `InitializeMint2` (optional freeze authority), `InitializeAccount3`,
`InitializeAccount` (owner and Rent sysvar as accounts; the owner may be the
account itself), `MintToChecked`, `BurnChecked`, `TransferChecked` and their
`...Signed` PDA forms, `GetAccountDataSize` (up to 16 extra extension types) and,
for Token-2022 only, `InitializeMetadataPointer`. A PDA authority may be the
mint or source account itself, so the same index appears twice in the metas,
as Hyperlane's self-authorized synthetic mint and escrow do.

They check the program, that each named mint or token account is a
non-executable account owned by that program, required writability, and the
authority's transaction signature or PDA seeds under the caller (`ErrMint`,
`ErrAccount`, `ErrWritable`, `ErrAuthority`, 2001 for the program). They do not
decode account bytes: Token enforces balances, mint relationships, `decimals`
and extension rules, and its errors propagate unchanged (for example 18
`MintDecimalsMismatch`, 4 `OwnerMismatch`, 1 `InsufficientFunds`).
`GetAccountDataSize` leaves its little-endian uint64 result in return data;
reading it needs a `get_return_data` boundary the SDK does not provide yet.
The [token CPI probe](../examples/token-cpi/README.md) runs every builder against
the real Token-2022 8.0.0 and Token 3.5.0 images bundled with LiteSVM 0.8.2.

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

## Token-2022 extended accounts

`token.ReadMint(c, index)` and `token.ReadAccount(c, index)` decode the current
mint (`MintState`: optional mint/freeze authorities, supply, decimals) or token
account (`State`) owned by classic Token or Token-2022, including Token-2022
extended layouts such as Hyperlane's 234-byte metadata-pointer mint and the
170-byte ATA with `ImmutableOwner`. Classic Token requires exactly 82/165 bytes.
A Token-2022 extended account must have zero padding to byte 165, the account
type at byte 165 (1 mint, 2 account), and TLV entries (type and length as
little-endian uint16) that fit the data; the 355-byte Multisig length is never
an extended account. Both reject uninitialized state and noncanonical COption
tags, and `ReadAccount` rejects wrapped native SOL like `Load`. They take an
index rather than a checked reference and reread current bytes on every call;
`Load`, `Read` and the classic helpers still accept only classic Token.

`token.FindExtension(c, index, kind)` returns an extension's data offset and
length (`ErrExtension`, 3015, when absent). `ReadTransferFeeConfig` decodes
`TransferFeeConfig`, and `token.Fee(c, mint, epoch, amount)` reproduces
Token-2022's fee for a pre-fee amount: the older or newer fee by epoch,
`ceil(amount * basis points / 10000)` capped at the maximum, zero without the
extension or for classic mints. Pass the epoch from `solana.Clock`. The
[extension probe](../examples/token-extensions/README.md) checks these against
accounts written by the real Token-2022 program, and the fee against what it
withholds across an epoch change.

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
With a threshold of exactly 2.0 (Fogo, and Solana before SIMD-0194) or 1.0
(after it) and `(128+size)*rate` below 2^53, the conversion and product are
exact, so the result is twice or once that product and the rounding emulation
is skipped: 480 to 760 CU less per call on SBF, and 2 to 8 CU more on other
thresholds ([measurements](../results/sdk/2026-10-19-rent-fast-path/README.md)).

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

PDA-signed System calls cover program vaults. `system.TransferSigned(c, system,
source, destination, amount, &seeds)` moves lamports out of a data-less
System-owned PDA under the caller, such as a native-SOL vault or an ATA payer.
`Allocate`/`AllocateSigned` set an empty System account's length (at most 10,240
bytes) and `Assign`/`AssignSigned` change the owner of a System account whose data
is empty or all zero; the plain forms need the target's transaction signature.
`system.CreatePDA` accepts a target address that already holds lamports, which
makes `CreateAccount` (and `CreateSigned`) fail, so anyone could otherwise block
creation by pre-funding the address. An unfunded target uses `CreateSigned`; a
funded one is topped up from the payer to the current rent minimum, then
allocated and assigned with its seeds. None of these fund later rent: a transfer
that leaves a vault below its minimum must be checked by the caller.
The [System PDA probe](../examples/system-pda/README.md) runs them against the
real System program under both runner profiles.

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

`solana.MoveLamports(context, from, to, amount)` changes two balances directly,
as a Rust program does with `**from.lamports.borrow_mut() -= amount` and
`**to.lamports.borrow_mut() += amount` (Hyperlane's fee claims, for example).
It enforces the runtime's rules before changing anything: the source must be
owned by the calling program, writable and non-executable (2005, 2011, 2012, as
for `ResizeAccount`); the destination must be in range (2005), writable and
non-executable (2011), and may be any account, owned or not, existing or not.
A source balance below `amount` returns 2020 and a destination overflow 2014.
The checks apply to a zero amount too. Moving within one account (the same
index or a duplicate descriptor) changes nothing; the total is always
preserved. Rent exemption is the application's check: the runtime rejects a
transaction that leaves the source below its minimum (`InsufficientFundsForRent`),
so claim only the excess over `system.MinimumBalance`, as
[examples/fee-vault](../examples/fee-vault/README.md) does. Programs that do not
call it compile to the same C as before.

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
2013 (self refund/malformed native key), 2014 (refund or credit overflow), 2015
(Rent output length) and 2020 (`MoveLamports` source balance below the amount). These are experimental API codes, separate from schema-2 errors.

## Associated token accounts

`ata.Address(c, wallet, tokenProgram, mint)` derives the canonical associated
token account and its bump with `pda.Seeds.FindAddress` (Solana's
`find_program_address`: bumps 255 down to 1, 3001 when the builder has no room
for the bump, 3004 when no bump gives an off-curve address) over the wallet,
token program and mint keys under the ATA program (`ata.ProgramID()`); `ata.Matches` compares
it with a context account. `ata.CreateIdempotent(c, ataProgram, payer, account,
wallet, mint, system, tokenProgram)` and `ata.Create` invoke the real ATA program
with its six accounts; the `...Signed` forms pay with a PDA under the caller.
They check the ATA, System and token (classic or Token-2022) programs, the mint
owner, the destination's writable flag, and a writable,
data-less System-owned payer with a transaction signature or matching seeds
(3030 program, 3031 account, 3032 payer authority). ATA errors propagate, such
as `IllegalOwner` for `Create` of an existing account. Under Token-2022 the ATA
program sizes the account for the mint (170 bytes with `ImmutableOwner` for a
mint without account-required extensions). The caller checks a PDA payer's
remaining rent if it must stay exempt, as Hyperlane's warp routes do.

Creation delegates canonical-address validation to the checked ATA program,
which returns `InvalidSeeds` for a substituted address. It does not repeat
that derivation in the SDK. Use `Address` or `Matches` for explicit preflight
checks when needed.
The [ATA probe](../examples/ata-create/README.md) runs creation and on-chain
derivation against ATA 1.1.1, Token-2022 8.0.0 and Token 3.5.0.

## Validation and open work

The [CPI bounds proof](../results/compiler/2026-10-19-cpi-limits/README.md) runs
[examples/cpi-limits](../examples/cpi-limits/program.go) on fogo-v0 and solana-v3:
3, 16, 17, 22, 32 and 64 metas with up to four PDA signer groups, through the
builders and the raw encoding, against a callee that checks its account count,
signers and every repeated-account position; 65 metas and five groups fail
before invoking (3002/3003, 2006/2007). A 3-meta builder CPI costs 9–10 CU more
than before and a 16-meta one 21–23 (the builder's heap pointer and branch); the
raw path is unchanged. Existing token/System CPIs in the escrow, swap and
tutorial suites cost 0–21 CU more per transaction, and every program using
`sdk/cpi` links the allocator (about 1.7 KB of ELF) even when it never spills.

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
