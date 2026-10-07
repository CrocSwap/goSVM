# Draft multi account binding design

October 5, 2026. This proposal turns the roadmap's private-alpha authoring goal
into a bounded implementation sequence. Its first [schema-2 prototype](SCHEMA2.md)
is implemented; token/PDA/CPI/lifecycle portions remain proposed and the API is
experimental. Preserve the current schema-1 starter and manual token program as
regression baselines.

The first target is the existing full-width swap: eight accounts, checked classic
SPL Token transfers, PDA signing, and one pool mutation. The next target is escrow
with initialization and closing. These two applications should determine the
required language and framework work. The swap alone cannot validate a complete
account lifecycle.

## Current boundary

Schema-1 `internal/project/generate.go` emits a single `Process` adapter, validates one
owned writable account, decodes flat unsigned fields, calls a handler by value,
and writes its returned state only on success. Wire layout is explicit and
independent of native Go/C layout. The compiler supports nested value structs,
[bounded scalar arrays](GO_ARRAYS.md) and [shared-package imports](GO_PACKAGES.md),
[multiple results/control flow](GO_CONTROL.md), [constrained pointers/methods](GO_POINTERS.md),
and [borrowed byte views](GO_VIEWS.md). Schema-1 wire generation still
excludes arrays. The SDK
already provides account data/keys/flags, PDA checks, and two narrow token-transfer
calls; token account validation is handwritten in the full swap.

Keep the successful value-based update model for the first binding prototype.
Constrained pointers now have a verified lexical borrowing subset. The first
binding prototype can retain by-value updates; pointer APIs must obey that subset.

## Schema and generated artifacts

Introduce an explicitly versioned schema 2 rather than changing schema 1 in
place. Each instruction declares its handler, argument/result types, ordered
accounts, and constraints. Account names resolve to compile-time indices;
constraints resolve to typed declarations and checked SDK operations, never
arbitrary generated expression strings.

The proposed shape is illustrated by this partial swap declaration. It omits
the six remaining accounts and is not accepted by today's CLI:

```json
{
  "schema": 2,
  "instructions": [{
    "name": "Swap",
    "handler": "Swap",
    "args": "SwapArgs",
    "result": "SwapResult",
    "accounts": [
      {"name": "Pool", "kind": "state", "type": "Pool", "access": "write", "owner": "program_id"},
      {"name": "User", "kind": "signer"}
    ],
    "alias_policy": "distinct"
  }]
}
```

Generate instruction dispatch, argument/account codecs, checked account bundles,
Go clients, and IDL from one resolved schema model. Dispatch uses explicit
eight-byte instruction discriminators. Enforce unique discriminator bytes and
instruction names. Keep account field order and widths explicit, including
`[32]byte` public keys, with little-endian unsigned scalars and no native padding.
Separate account layout version from schema/generator version.

Write a layout manifest with type names, field types, offsets, sizes, version,
and discriminator. A detected layout change fails `check` until an explicit
version/migration decision is recorded; regeneration must not silently bless it.
Initialization encodes the correct layout. Exact length checks remain the default;
extensions require an explicit versioned layout, not permissive trailing bytes.

Preserve schema-1 generation and its SDK snapshot. Do not silently upgrade
existing projects. Schema-2 projects pin their SDK and generator compatibility,
and generated files stay inspectable and checked into the project.

## Handler contract and write ordering

A proposed handler receives context, a generated validated account bundle, and
typed arguments, and returns a typed result containing an error code and updated
owned state values. [Multiple results](GO_CONTROL.md) are now supported, so an
updated-state/status pair is also available; choose one explicit generated
contract per schema version.
The account bundle exposes pool values and checked token/account references.
Raw account indices remain an explicit low-level escape hatch, not the tutorial
authoring surface. Context carries native-test callbacks as it does today.

Validation and decoding finish before application code or CPI runs. The handler
executes checked token helpers, then returns the updated pool. The adapter writes
only declared writable program-owned state, only after a successful handler.
It must never serialize token snapshots over changes made by CPI. Token helper
reads that depend on a prior CPI must read current account bytes again rather
than use decoded balances from before the call.

A failed handler or CPI propagates its exact failure category. Runtime transaction
rollback remains authoritative, including an earlier successful instruction in
the same transaction. Native tests model callbacks and business logic; compiled
SVM tests prove CPI/state/rollback behavior. Never infer runtime atomicity from
native Go tests alone.

## Validation policy

For each instruction, check its discriminator and exact argument length, exact
account count, required privileges, account identity/owner, layout, relations,
and PDA constraints in a documented deterministic order. Existing error mappings
must remain independently asserted when migrating the manual swap.

| Account kind | Required checks |
| --- | --- |
| Program-owned state | Owner, non-executable, exact layout/discriminator, writable for updates |
| Signer/authority | Required signature, declared relationship to state/token authority |
| Classic token account | Classic token owner, 165-byte layout, initialized state, mint, authority, applicable writable flag |
| Program | Expected key and executable flag |
| PDA | Declared program ID, ordered seed encoding, stored/explicit bump, off-curve derivation |

Readonly declarations prevent framework writes. They need not reject an account
whose transaction-wide privileges are writable: Solana merges privileges across
instructions. Required signer/writable privileges are minimum requirements.
Exact non-executable checks still apply to state. Frozen classic token accounts
reach SPL Token for its error, preserving current failed-CPI fixtures. Native
token accounts, Token-2022, delegates, and multisig remain unsupported until
separately specified and tested.

Aliasing defaults to distinct account keys. Any allowed alias is an explicit
named pair with a reason, and conflicting mutable state views are rejected.
Duplicate instruction references must be checked by key after runtime decoding;
distinct index numbers do not establish distinct accounts.

PDA seeds support a bounded number/size of literal bytes, public keys, and unsigned
scalars with specified little-endian widths. Reject missing/ambiguous seed types
and enforce the runtime seed limits. Seed/bump provenance must be visible in IDL
and clients. Cross-program PDAs require an explicitly declared derivation program.

Reserve framework error codes centrally and reject collisions with handler codes.
IDL names describe count, privilege, owner, layout, alias, relation, and PDA
failures. Helper errors preserve downstream instruction errors rather than
collapsing every rejected CPI into a generic handler error.

## Minimum compiler and SDK work

| Slice | Capability and acceptance evidence |
| --- | --- |
| Fixed arrays | `[32]byte`, array values/copies/indexing/literals, zero values, bounds failures, native-Go/generated-C/SBF comparisons; reject unsupported array operations explicitly |
| Module imports | Resolve project-local packages without executing package initialization; qualified names, deterministic dependency order, cycle/unsupported-feature diagnostics; retain SDK allowlist |
| Binding prototype | Multiple instructions/accounts, explicit layouts and validation, value-based update contract; migrate full token swap without handwritten byte offsets |
| Checked token/PDA helpers | Typed account references, bounded seed marshaling, transfers with and without PDA authority, full byte/error/CPI/rollback fixtures |
| Methods and pointers | Receiver semantics and caller-owned storage with alias/lifetime analysis; reject escaping pointers, unsupported address-taking, and recursion before code generation |
| Lifecycle | System-program create/fund/assign, rent, token initialization, close; explicit authority/payer and transaction failure coverage |

Arrays and module imports are foundations for reusable key/token types. Implement
them with differential tests before making the new framework a recommended user
API. A generator may prototype account indices and byte access using today's
subset, but that prototype is not evidence that the language-foundation gate is
complete. Stack use must be measured as account bundles and by-value arrays grow.
Scalar arrays and module-aware imports are implemented and validated. Imports
preserve named-type identity, qualified calls and transitive dependencies, with
an isolated native client reusing the same types/quote/handwritten codecs.
Richer array elements remain open. Schema-2 binding generation now implements
the owned-value prototype; checked token/PDA/CPI and lifecycle are next.

Initialization accepts only a declared uninitialized account state with appropriate
owner, signature or PDA authority, and payer privileges. Determine rent from the
runtime, create/fund through System Program CPI, and encode only after successful
initialization. Do not generalize the genesis-preloaded swap into an initialization
claim. Closing/resizing needs a separate policy for recipients, authorization,
zeroing, and reuse; implement the subset actually required by escrow first.

## Shared on-chain and off-chain Go packages

Code reuse by ordinary Go services is a milestone 2 requirement. Author account
and instruction types, constants and pure calculations once in shared packages.
The on-chain handler and native Go backend import those declarations. Generated
Go clients accept and return the same named Go types, including qualified types
from shared packages; redefining a matching struct is not equivalent Go type
identity. Wire bytes still follow explicit codecs, never Go memory layout.

Separate pure model/math code from account context, validation and CPI operations.
The compiler checks the on-chain entrypoint's reachable package graph. A backend
can use HTTP, databases, goroutines and other ordinary Go facilities outside that
graph. Shared code compiled on chain must satisfy the supported subset, with
native-Go/SBF comparisons for its semantics. Host services use the standard Go
toolchain and do not execute SBF or emulate runtime syscalls to run a quote.

The schema-1 starter already defines `Pool`, `SwapArgs` and `Swap` as ordinary Go,
and its client imports those types directly. A separate native Go module can use
them with local module/SDK replacements. This is a demonstrated starting point,
not finished dependency packaging: the starter's `gosvm => ./.gosvm/sdk` directive
does not provide a consumer's dependency configuration. Schema 2 must provide
normal imports of shared types/logic without requiring consumers to install SBF
tools, copy definitions or hand-wire the embedded SDK dependency. Decide exact
module boundaries during the import/generator work.

The [shared-package example](../examples/shared-packages/README.md) now proves
pure model/quote/wire reuse by an isolated service module without the compiler or
SDK. It uses the bounded legacy ABI and handwritten codecs; this does not finish
the schema-2 or full-token swap gate. Normal local module replacements stand in
for the unpublished example module. SDK packaging remains open for applications
that directly import runtime APIs.

Acceptance includes a separate Go service module importing canonical swap types,
round-tripping account/instruction codecs and computing a quote with the same
function as the program. It must work without the goSVM checkout, compiler,
runner or validator. Compare native-Go and SBF outputs for common business-logic
vectors and test codec bytes against independent wire expectations. No general
standard-library or heap support on chain is implied by host-side reuse.

## Implementation sequence and gates

1. Completed: fixed scalar arrays, borrowed byte views, module-aware imports,
   methods/constrained pointers and multiple returns/control flow, with semantic,
   lifetime, bounds and stack coverage.
2. Implemented for the prototype: resolve schema 2 into one internal model; test invalid schema/constraint
   references, discriminator collisions, alias rules, and stable layout manifests.
3. Implemented: generate a two-instruction/two-state prototype using value-based handlers.
   Exercise mixed writable privileges and rejection before application execution.
4. Add checked token/PDA helpers and migrate the full swap to schema 2. Preserve
   the manual baseline and compare every current fixture, CU, ELF, and build cost.
5. Implement escrow initialization, exchange/cancel, and close using the supported
   methods, pointers and borrowed views.
6. Have an independent trial developer complete both applications through the
   documented CLI and use their shared types/logic from a separate normal Go
   service. Record manual wiring and unsupported-feature encounters.

Each slice should be independently reviewable. Schema-2 generation uses Go
type inspection before entry generation; the full application/SDK gates remain
next. There is no calendar commitment yet. The first runner
spike and this design do not complete either roadmap milestone.

## Open decisions

Settle whether schema declarations remain in JSON or move to typed Go annotations
after trying the prototype; JSON is the proposed first implementation because
it extends the existing manifest and allows precise diagnostics. Decide the
public account-reference API through the swap migration, keeping validation
capabilities separate from raw context indices. Evaluate TypeScript/Anchor IDL
compatibility after the common schema model exists. The initial proposal makes
no Anchor compatibility claim.
