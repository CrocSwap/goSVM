# Generated full token swap

This experimental schema-2/SDK-2 application migrates the manual full-width,
exact-input classic-token swap into shared types, generated account validation
and checked token/PDA helpers. The manual program and historical measurements
remain in `../tokenswap/` and `../../benchmarks/anchor/go-token/`.
The original `Swap` instruction retains its preloaded 145-byte pool format for
the preserved comparison. `CreatePool`, `ManagedSwap` and `ClosePool` now provide
a complete creator-managed lifecycle with a separate 177-byte `ManagedPool`.

```sh
gosvm check
gosvm test
gosvm test --svm -svm-fixtures testdata/svm.json
gosvm test --svm -svm-fixtures testdata/lifecycle.json
gosvm build
```

The handler in `program.go` uses named account fields and no wire offsets.
`model/` owns the canonical `Pool`, `SwapArgs` and full-width quote function.
`client/` encodes/decodes those actual types for ordinary Go callers. The
[independent service](../full-swap-service/README.md) uses them without importing
the handler, compiler or runtime SDK.

`gosvm.json` declares eight ordered accounts, checked token references, required
privileges, vault key identity, token mint/authority relationships, duplicate-key
rejection and the pool-key/bump PDA constraint. `PoolRef` exposes the validated
pool key for the handler's seed builder. The adapter only serializes the pool
counter after successful transfers; token bytes are owned by SPL Token.
Failed-CPI/transaction atomicity must be checked in SBF, not inferred from native
callback behavior.

The 145-byte pool and 24-byte instruction use explicit discriminators matching
the preserved matched-token corpus. This is application-specific wire agreement;
it does not establish general Anchor IDL compatibility. Layout versions and
history are recorded in `layouts.json`; changing a recorded layout requires a
higher version, distinct discriminator and migration policy.

Framework errors use their documented categories (6000..6008 and token-helper
3010..3014), while application arithmetic errors retain 109/112/113/114 and real
SPL CPI errors propagate unchanged. The proof records an explicit mapping from
manual validation errors; successful account bytes and security rejection policy
are compared with the preserved corpus.

The packaged fast suite has 13 scenarios covering real transfers, invalid account
policy and stateful rollback. The [full migration proof](../../results/compiler/2026-10-05-framework-swap-lifecycle-final/README.md)
passes all 108 preserved scenarios/110 transactions in three repetitions. That historical swap-only module used 45,941 CU versus the manual baseline's
20,329 and a 21,536-byte ELF versus 8,064. The expanded current module uses
46,016 CU for that same wide swap and a 68,320-byte ELF; see the lifecycle below. These costs apply to the full token workload, not the
bounded starter or dependency-heavy protocol.

Reproduce into a new results directory:

```sh
python3 ../../scripts/framework_swap_verify.py --output ../../results/compiler/new-full-swap-proof
```

That command rebuilds an isolated CLI, scaffolds SDK 2, generates this application,
tests the independent service and native vectors, then executes the actual ELF
against the full preserved SBF corpus. It requires installed v1.51 LLVM and the
pinned local runner archive. Keep the SDK snapshot and generated artifacts with
this module; no package has been published. Root `go test ./...` omits this nested
module and the separate service.

## Creator-managed lifecycle

`CreatePool` takes 11 named accounts: the new pool signer, writable creator signer,
creator X token account, X/Y vault PDAs, creator Y token account, authority PDA,
classic Token and System programs, and X/Y mints. The creator's token accounts
must match the declared mints and creator authority. Both reserve deposits must
be positive and available. Generated policy rent-funds the 177-byte pool; checked
SDK helpers create rent-funded 165-byte token vaults, initialize them under the
pool authority and transfer both deposits. Mint and user token holdings are
external inputs; the program creates the pool and its vaults from unallocated
accounts.

The pool authority uses `[pool key, Bump]`. Vaults use `["vaultx", pool key, XBump]`
and `["vaulty", pool key, YBump]`. All bumps are explicit; clients perform bump
search before encoding `model.CreatePoolArgs`. The new pool is a transaction
signer, while the program signs vault allocation using the declared PDA seeds.
Current Rent is used with a one-lamport floor at free rent. Failed creation or a
later deposit rolls back the pool, vaults and earlier CPIs in SBF.

`ManagedSwap` validates the same eight-account token policy against
`ManagedPool.Core` and calls the shared swap handler/math. The creator is preserved
while the core swap counter updates. It accepts a trader's signed token accounts.

`ClosePool` requires the recorded creator signer and matching creator-owned
refund token accounts. The handler drains both vaults using the authority PDA,
closes them to the creator through SPL, and generated policy closes the owned
pool to that same creator. All rent/donations return with closing. Empty frozen
vaults can close without a transfer; frozen nonempty vaults or refund accounts
retain the actual SPL transfer rejection. A failed later transfer/close restores
all preceding effects through transaction rollback.

This example has creator-owned liquidity and no LP tokens or distributed
withdrawal accounting: the creator may drain and close at any time. It is an
experimental reference application, not an audited AMM. Existing 145-byte `Pool`
accounts are not reinterpreted or migrated; managed instructions require the new
layout/discriminator. `layouts.json` preserves both formats and their versions.

[Lifecycle evidence](../../results/compiler/2026-10-06-managed-swap-supported/README.md)
passes 29 scenarios/36 transactions in three SBF runs, including initialization,
funding, swaps, drain/close/reuse, custom/free Rent, invalid authorities,
substitution/duplicates, failed CPI, overflow and atomic multi-instruction rollback.
[The preserved corpus/service regression](../../results/compiler/2026-10-06-framework-swap-managed/README.md)
passes 108 scenarios/110 transactions in three runs, plus native vectors, the
13-case project suite and independent ordinary-Go service in both layout modes.
Native handler tests exercise creation, swaps, refunds and recreation, without
claiming VM atomicity from callbacks.

The tested managed lifecycle uses 86,742 CU for create/deposits, 47,858 for swap,
and 61,869 for drain/close. The largest static frame is 3,968 bytes, within the
4,096-byte SBF limit; account value copies have little additional stack headroom.
Both suites run the same 68,320-byte ELF and verify instrumented relink identity.
[Repeated build/CU/footprint measurements](../../results/compiler/2026-10-06-milestone2-bench-supported/README.md)
now pass; the [independent developer trial](../../docs/MILESTONE2_TRIAL.md) remains
the final milestone-2 gate.

```sh
python3 ../../scripts/managed_swap_verify.py --output ../../results/compiler/new-managed-swap-proof
```
