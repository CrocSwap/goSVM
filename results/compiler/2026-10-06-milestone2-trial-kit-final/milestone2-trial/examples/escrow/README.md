# Cancellable SOL hashlock escrow

This separate schema-2/SDK-2 application starts with an unallocated PDA, funds
rent and a SOL deposit, and closes it after claim or cancellation. Handler code
uses generated named account bundles and shared model types without wire offsets.

`Open` requires a writable System-account creator signer. Generated validation
binds the beneficiary argument to the beneficiary account, checks the PDA using
`"escrow"`, creator key, little-endian nonce and explicit bump, then creates the
121-byte owned state using current Rent. The handler deposits the requested SOL.
`Claim` requires the recorded beneficiary signer, matching nonce and a 32-byte
preimage whose SHA-256 matches the stored commitment. `Cancel` requires the
recorded creator signer. Generated authorized closing refunds all lamports and
removes the state; the same PDA can subsequently be recreated.

The creator can cancel at any time, including before a beneficiary claims. There
is no expiry or fair-exchange guarantee. Claim pays the entire balance, including
rent and donations, to the beneficiary; cancellation pays it to the creator.
This is an experimental SOL example, not a token exchange or audited application.

From this directory with the CLI, v1.51 LLVM and runner 0.5.0 installed:

```sh
gosvm check
gosvm test -- -count=1
gosvm test --svm -svm-fixtures testdata/svm.json -- -count=1
gosvm build
```

The fixture seeds are deterministic local test identities. No RPC or public
cluster is used. Root tests omit this standalone module, so test it separately.
For a fresh scaffold use `gosvm new -schema 2 -sdk 2 -module
example.org/gosvm/escrow <new-directory>`; the proof below copies only application
Go/config/layout history into that scaffold and keeps its SDK snapshot unchanged.

`model/` defines the canonical `Escrow`, `OpenArgs`, `ClaimArgs` and empty
`CancelArgs`. `client/` imports those actual types and provides generated codecs
for ordinary Go services without the runtime/compiler SDK. The account layout is
121 bytes; instruction sizes are 89, 48 and 8 bytes respectively. Default
version-1 discriminators and explicit versions are recorded in `idl.json` and
`layouts.json`. Preserve the layout history, including the initial scaffold's
retired layouts; version changes record policy and do not migrate account data.
No application module has been published.

[Generated lifecycle policy](../../docs/SCHEMA2.md) checks payer, signer, owner,
address, relationship and alias requirements before funding. Application errors
are 4100 (zero amount), 4101 (beneficiary), 4102 (secret), and 4103 (nonce).
Framework errors are 6000..6009 and SDK/System errors propagate. Failed native
callbacks do not establish transaction atomicity; compiled-SBF tests do.

The [proof](../../results/compiler/2026-10-05-escrow-supported/README.md) passes
32 scenarios/42 transactions in three SBF repetitions, including wrong authority,
substituted/duplicate accounts, failed deposit CPI, custom/free Rent, claim/cancel,
PDA reuse and atomic rollback after creation or closing. The ELF is 25,280 bytes,
with a largest static frame of 1,984 bytes. The tested lifecycle uses 20,508 CU for
open, 8,356 for claim and 6,756 for cancel. These are escrow measurements, separate
from the bounded starter, full token swap and dependency-heavy protocol.

Reproduce from the repository root into a new directory:

```sh
python3 scripts/escrow_verify.py --output results/compiler/new-escrow-proof
```

[Repeated build/CU/footprint measurements](../../results/compiler/2026-10-06-milestone2-bench-supported/README.md)
and the managed swap lifecycle now pass. The [independent developer trial](../../docs/MILESTONE2_TRIAL.md)
remains the final milestone-2 gate.
