# Experimental schema 2 authoring

Schema 2 provides the first generated multi-instruction/account prototype. It
supports program-owned state, signer/program/raw account references, minimum
privileges, key relationships and explicit aliases. Optional [SDK 2](SDK2.md)
provides checked token/PDA/CPI helpers and generated token/PDA constraints.
SDK 2 also supports rent-funded initialization and authorized owned-state
closing, verified by the [escrow application](../examples/escrow/README.md).
Schema 1 and its SDK snapshot
remain supported without an implicit upgrade.

```sh
gosvm new -schema 2 -module example.org/my/ledger my-ledger
cd my-ledger
gosvm check
gosvm test
gosvm build
```

The [multi-state example](../examples/multi-state/README.md) has `Move` and
`SetLimit` instructions, a packed `Vault` layout and a `Policy` layout. Move
updates two owned ledgers using a readonly policy and an authority signer. These
are preloaded ledger accounts, not SPL-token accounts or an initialization demo.

## Canonical types and handlers

`imports` in `gosvm.json` maps schema qualifiers to ordinary Go import paths.
`layouts` names exported struct types from shared packages and assigns explicit
nonzero layout versions. `instructions` declares handlers, canonical argument
types, generated bundle names, versions and ordered accounts. Shared wire
packages must be independent of the runtime SDK; clients import their actual Go
types rather than redeclaring matching structs.

```go
func Move(c solana.Context, a MoveAccounts, args model.MoveArgs) (MoveAccounts, uint64)
```

Generated bundles contain decoded state values and `GosvmAccountRef` values
(`Index uint64`, `Key [32]byte`) for other accounts. Context is passed separately.
SDK-2 token declarations contain opaque checked `token.Account` values.
An optional `ref` on state/token declarations names a separate `GosvmAccountRef`
field, for example `"ref": "PoolRef"`; it must be distinct from every other bundle
field. The reference gives handlers a validated key/index without manual account
positions. Returned references are not serialized.
The handler returns its updated bundle and an unsigned status. After success,
the adapter commits declared writable owned state. Readonly state results and
non-state references are not serialized. A handler can use explicit low-level
SDK calls; this generated write policy does not sandbox those calls.

Type inspection uses provisional bundle declarations in memory, replacing stale
generated adapters before resolving the import graph. Full compiler/body/borrow
validation runs against the complete adapter before generated artifacts change.
Module resolution remains offline and never executes package initialization.

## Packed wire format and layout history

Wire structs encode fields in declaration order with no native padding. Supported
fields are byte/uint32/uint64 (including named versions), bounded scalar arrays,
and nested exported value structs. Embedding, tags, signed values, boolean wire
fields, pointer/slice fields, arrays of structs and nested arrays are rejected.
Each encoded layout, including its eight-byte discriminator, is capped at 1,024
bytes. Account-backed byte views can represent larger buffers at the low-level
SDK boundary; schema-2 owned-value codecs do not yet support those large layouts.

Default discriminator bytes are the first eight SHA-256 bytes of
`gosvm:<account|instruction>:<Name>:v<Version>`. An explicit discriminator is
eight hex bytes. Wire versions are separate from project schema and SDK pinning;
the default version is incorporated in the discriminator rather than a separate
payload word. Instruction payloads have exact lengths. State buffers have exact
lengths and discriminator checks before field decoding.

`layouts.json` preserves canonical type paths, recursive field offsets/sizes,
versions, discriminators and migration notes. Generation and check reject changes
to an existing version, version downgrades, reused historical discriminators,
malformed history and a missing lock beside an existing schema-2 adapter. A new
version of a recorded layout requires a nonempty `migration` policy and appends
history. This records the decision; it neither migrates accounts nor implements a
migration instruction. Restore deleted history rather than regenerating it away.

`client/zz_gosvm.go` generates `EncodeAccountVault`, `DecodeAccountVault`,
`EncodeInstructionMove`, and corresponding functions/constants for other layouts.
Clients use ordinary Go allocation and encoding libraries. They import the pure
model packages, so an independent service needs no compiler, SBF tools, runtime
SDK replacement or copied offsets to use them. Normal Go module resolution is
still required for the unpublished application module.

## Account validation and error contract

Each instruction requires 1..16 ordered accounts; a schema supports 1..32
instructions. State declares a known layout and `owner: "program_id"`.
Signer declarations require a signature. A program declaration requires an exact
32-byte hex `address`, executable flag and read access. Raw references can require
an owner and address but do not certify a token layout. Required privileges are
minimums: readonly declarations accept merged writable message privileges.
Non-program accounts must be non-executable. All supplied keys must be 32 bytes.

Relations declare an exported `[32]byte` state/argument field (for example
`args.Beneficiary`) or a checked token's `Mint` or `Authority` field. Exactly one target is required: `account` names a runtime
account key, or `equals` names another state/argument/token key field. For example,
`{"field":"VaultX.Mint","equals":"Pool.MintX"}` binds token data to the canonical
pool model. Token fields are read from current checked SDK state before the
handler. Scalar fields cannot be used as key relationships.
Duplicate keys are rejected by
default. `aliases` permits named pairs with a nonempty reason. Two state views
cannot alias if either is writable; duplicate indices alone do not prove identity.

SDK 2 adds `kind: "token"`: no owned layout, owner override or transaction signer
is allowed. `access` controls its writable capability. The SDK checks classic-token
ownership/layout/state; this does not declare a mint account or Token-2022 support.

An optional `pda` on a non-program, non-transaction-signer account declares its
derivation program and seeds. `program` is `program_id` or the name of a declared
program account. Seeds are structured data, not arbitrary Go expressions:

```json
{"name":"Authority","kind":"account","access":"read",
 "pda":{"program":"program_id","seeds":[
   {"kind":"key","account":"Pool"},
   {"kind":"byte","field":"Pool.Bump"}
 ]}}
```

`key` uses a named account's runtime key; `bytes` uses at most 32 hex-encoded bytes
(including an empty seed). `byte`, `uint32` and `uint64` name exported canonical
state fields or `args` fields of the matching underlying width. Integer encoding
is little-endian. There are at most 16 seeds including the explicit bump; no
implicit bump search is generated. All argument/state fields are type-checked
before any artifacts change. Derivation failure or address mismatch returns 6008.
Handlers still build signing seeds explicitly with the checked PDA API.

Validation order is instruction discriminator/length, account count, privileges,
identity/owner, aliases, owned-state layouts, checked-token layouts, relations,
PDA constraints, handler, writable-state commit. Optional phases appear in the
IDL only when the instruction set needs them.
Within each phase accounts follow declaration order. Framework codes are 6000
count, 6001 flags, 6002 owner, 6003 state layout, 6004 instruction layout, 6005
identity, 6006 alias, 6007 relation/close authority, 6008 PDA, and
6009 initialization target/payer. Token-helper errors propagate
from checked layout/state loading. Root handler constants named `Err*` must
be integer codes 1..5999, checked after Go constant evaluation. Handler statuses
propagate unchanged; downstream CPI mappings need their own application tests.

## Generated initialization and closing

Lifecycle declarations require explicit SDK 2 and writable `kind: "state"` with
`owner: "program_id"`. A target can declare one lifecycle action per instruction.

```json
"init": {"payer": "Creator", "system": "System"}
```

The payer must be a distinct writable transaction signer with an empty System
account. System must be a declared fixed executable System program. The target
must be an empty, unfunded System account and either a transaction signer or a
PDA derived under `program_id`. Every init target and payer is checked before any
creation CPI. Aliases involving lifecycle targets are forbidden. Pre-handler PDA
seeds and relationships cannot read freshly initialized fields; use arguments or
existing state. Argument key relationships are checked before rent funding.

After validation, the generated adapter calls `system.Create` or `CreateSigned`
with the exact packed layout size and current Rent. The handler receives a zero
state value and any declared reference. Successful state commit writes its
versioned discriminator and fields into the newly created storage. Handlers must
validate application-specific amounts/commitments and perform additional funding.

```json
"close": {"to": "Beneficiary", "authority": "Beneficiary",
          "authority_field": "Escrow.Beneficiary"}
```

`authority` must name a distinct transaction signer. `authority_field` must be an
exported canonical `[32]byte` field of the state being closed; its decoded value
must match that signer's key before the handler. `to` must name a distinct writable
non-program account with no lifecycle action. Matching recipient policy beyond
this explicit declaration belongs to the application.

On handler success, the adapter closes the source through `solana.CloseAccount`,
refunding all lamports and assigning System ownership with zero data. It never
serializes the returned closed state. Other writable state in a lifecycle
instruction is committed through fresh data/owner views after the handler/CPI.
Native mocks do not provide transaction atomicity: a failed handler, close, CPI or
later instruction requires actual-SBF testing to establish rollback.

The IDL records optional `init_target`, `instruction_arguments`, `close_authority`,
`initialize` and `owned_state_close` phases where applicable. Instruction argument
relationships cause argument decoding before relations; other instructions retain
the validation order described above. Initialization follows PDA checks and
precedes the handler; authorized closing follows handler success.

## Validation

The [prototype evidence](../results/compiler/2026-10-05-schema2-supported/README.md)
compares independent packed-wire/math/error expectations against native Go and
compiled SBF: 161 native vectors and 165 scenarios/166 transactions in three
repetitions. It includes wide uint64 values, authority/owner/layout/privilege and
duplicate failures, merged privileges, readonly result suppression and atomic
rollback after a successful earlier instruction. The 7,088-byte ELF has a
1,408-byte static entry frame; instrumented linking reproduces its hash.

Generator tests additionally cover canonical independent-service imports, named
scalar arrays, readonly aliases/program identities, layout history and malformed
declarations. Reproduce a new source snapshot using:

```sh
python3 scripts/schema2_verify.py --output results/compiler/new-schema2-experiment
```

The generated escrow additionally passes [32 scenarios/42 transactions in three
SBF runs](../results/compiler/2026-10-05-escrow-supported/README.md), including
current rent, failed deposit CPI, close/reuse and multi-instruction rollback.
Generator diagnostics reject 27 invalid lifecycle declarations without changing
generated artifacts; a multi-target native case verifies every initialization
target is checked before the first funding CPI. The [complete managed swap
lifecycle](../results/compiler/2026-10-06-managed-swap-supported/README.md) now
passes 29 scenarios/36 transactions × three, including generated transaction-signer
initialization, SDK signed vault creation, drains/closes/reuse and rollback. Final
comparative measurements and external developer trials remain in the
[milestone-2 acceptance checklist](MILESTONE2_ACCEPTANCE.md).
