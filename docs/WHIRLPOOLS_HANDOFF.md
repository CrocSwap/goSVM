# Whirlpools agent handoff

October 6, 2026. A scoped Whirlpools port can continue alongside milestone 2.
A faithful full-contract port cannot yet use only the supported framework APIs.
Read [HANDOFF.md](../HANDOFF.md), [README.md](../README.md),
[the acceptance checklist](MILESTONE2_ACCEPTANCE.md) and the language references
before choosing representations. Preserve experiments in new result directories;
keep Basanos untouched. The local worktree has substantial uncommitted work.

## Updated benchmark coordination

The newer [Phoenix dependency frontend](PHOENIX_DEPENDENCIES.md) adds SDK-2 return
data and v0 multiplication linking. An unsigned CPI meta-length check avoids a
v0 signed-remainder helper and causes a small Whirlpools v3 change: −3..+10 CU
per case, median 1.281201× rather than 1.280853×. All 166 cases pass against both
Rust references × three; ELF is 83,480 bytes and maximum frame 2,368. The original
optimization and decoder-only pins remain frozen. Upgrading to this API requires
a new complete frontend and matching SDK snapshot.

The [SDK-2 decoder extension](ACCOUNT_DECODER32.md) now admits up to 32 incoming
accounts after Phoenix exposed the old 17-account rejection. Whirlpools' optimized
166-case handler regression passes three runs against unchanged scoped/full Rust
with identical CU and the same 1.280853× ratio. The new frontend produces a new
83,424-byte ELF and a 2,368-byte largest frame. Preserve the optimization snapshot;
this decoder update requires its own complete frontend/SDK pin. Outgoing CPI
metas and schema-2 declared accounts remain separately bounded at 16.

The sibling benchmark's `docs/HANDOFF.md` now reports a verified fixed-fee swap
core: 3,085 Rust math vectors, 104 full swap transitions, and matching Go/Rust
SBF corpora repeated three times. Its unsigned limb math and borrowed full-size
tick-array views work without new native signed/wide types or larger owned-array
limits. These are no longer blockers for that scoped port. Its reported
60,416-byte Go / 182,792-byte Rust ELFs and 2.44 median successful paired CU ratio
apply to the core adapters, before production validation and token transfers.
The benchmark owns its source, evidence and interpretation.

The latest fee-growth checkpoint reduces the complete default-O2 handler ratio
to 1.81, with an 84,448-byte Go ELF and all 166 cases passing three runs against
unchanged Rust. Bounded division, inverse tick, wide division and fee growth were
optimized separately in application code; the benchmark preserves every prior
source/result checkpoint. Its subsequent derived-SDK compact signer checkpoint
passes all 166 cases three times, reducing the median paired handler ratio to
1.74 and the Go ELF to 84,192 bytes. All 103 successful cases save 2,418 CU;
20 failures improve and 43 remain unchanged. The benchmark's SDK-only control
reproduces its baseline ELF/CU exactly. That tested variant is now available as
optional canonical `cpi.SingleSigner`/`InvokeSingle`; see the
[adoption evidence and pin requirements](COMPACT_SIGNER_ADOPTION.md).
Preserved benchmark pins and the default handler remain unchanged.
Tick-validation reuse remains application work with explicit buffer/key/mutation
provenance.

The user's optimistic performance target is now **1.33× Rust**, with **1.5×**
as an intermediate checkpoint. Track median paired successful-case CU at
default O2 on this matched event-omitting classic-token handler corpus, keeping
the Rust references and runtime pins fixed. Prioritize validated tick-view reuse
and separate probes for tick decoding, crossing arithmetic and encoding; the
goSVM owner handles compiler changes driven by these reproductions. Preserve
all failure/security checks and report complete-corpus correctness, ratio
distributions, failed-path costs, ELF size and stack changes for each experiment.
This is an optimistic objective, not a forecast or a claim of full-contract
performance. See [the roadmap target](../ROADMAP.md#performance-and-competitive-evaluation).

The owner-side [packed-store compiler optimization](PACKED_STORE_OPTIMIZATION.md)
now measures **1.702520×** for the unchanged compact handler, with all 166 cases
passing three times. Its ELF is 84,208 bytes (+16), largest frame 1,472;
all successes improve, with median saving 746 CU and no failed-path regressions.
Adopt the recorded new frontend with a matching complete SDK in a separate pin
and rerun the benchmark's own Rust comparisons. No application changes are
required for the recognized write loops. The sibling default and compact pins
remain preserved. Packed-read and comparison-parameter controls did not justify
compiler changes. Validated-view reuse and remaining loop arithmetic remain
the next benchmark-side investigations.

The owner-side [1.33× target proof](WHIRLPOOLS_133_TARGET.md) now measures
**1.280853×** for a separately derived application snapshot at O2. Bounded
`MulDiv`, small-operand multiplication and a private core after the existing
handler validation preserve the public checked `Apply` entry. All 166 cases ×
three pass against unchanged scoped/full Rust, plus 29,713 arithmetic SBF cases
× three in Go and Rust. All successful cases improve from the 1.702520× owner
checkpoint; 29 failures improve, 34 stay unchanged and none regress. ELF is
83,424 bytes, largest frame 1,472. This completes the specified median target;
the per-case success range remains 1.226617×–1.617088×.

The [delivery archive](../results/compiler/2026-10-06-whirlpool-optimized-snapshot/optimized-snapshot.tar.gz)
contains the exact source modules, complete matching SDK, executable CLI,
frontend source archive, measured ELF and file/mode manifest. An extracted
rebuild reproduces that ELF. Record a new application/frontend/SDK pin and
replay your own benchmark proof before default adoption. The sibling source,
default handler and all previous pins remain untouched; this result still
excludes events, adaptive fees and dynamic arrays.

Its newer October 6 handoff now verifies the classic-token fixed-fee handler,
including real Clock, deposits/PDA withdrawals, oracle/account checks and
rollback: 166 scenarios repeated three times for Go, scoped Rust and unmodified
upstream Rust. Reported matched handler ELFs are 72,096 / 267,656 bytes, with a
2.07 median successful paired CU ratio. Both scoped handlers omit events; those
measurements are separate from the preserved core comparison above. The next
reported SDK gap is `sol_log_data` for the canonical 121-byte Traded event.
The port can optimize next-price division and inverse tick conversion while
that boundary is coordinated, retaining Rust/math-big and actual-SBF parity.
The benchmark owns adaptive fees, sparse/dynamic arrays and protocol lifecycle
as separately scoped extensions; existing SDK lifecycle APIs are unused there.

The requested [Clock API](SDK2.md#runtime-clock) is now implemented and passes
[12 SBF scenarios/15 transactions × three runs](../results/compiler/2026-10-05-clock2-supported/README.md).
Use `solana.Clock(c, clock[:])` with a `[40]byte` buffer. Unix timestamp bits occupy
bytes 32..39; decode little-endian, reject the sign bit with the upstream error,
and preserve time-regression/reward behavior in application math. Clock-accessor
validation is not a substitute for the port's own reward/error corpus.

The classic-token handler and oracle/trade-enable/PDA/CPI failure corpus now
pass in the benchmark. Continue event parity and arithmetic profiling/optimization.
Preserve the
old frontend/SDK pin and results, then pin a new matching frontend plus freshly
scaffolded SDK 2. API SHA:
`07f4f3f188bdadd803b1fa7d5350b2216810f5aefba74431d6d964fa5dd25012`.
Do not copy only the new SDK into the old CLI. SDK/compiler changes stay with
the goSVM owner; benchmark application/math/fixtures stay with the port agent.
Wrapped SOL, delegate/multisig policy, adaptive fees, dynamic arrays and complete
lifecycle require explicit subsequent coverage before wider parity claims.

## Available foundations

Module-aware offline imports, multiple source files, canonical shared Go types,
unsigned arithmetic, nested value structs, scalar arrays, multiple results,
switch/continue, methods and constrained pointers are supported. See
[packages](GO_PACKAGES.md), [arrays](GO_ARRAYS.md), [control flow](GO_CONTROL.md),
[pointers](GO_POINTERS.md) and [byte views](GO_VIEWS.md) for exact semantics.
Unsigned scalar widths are byte/uint32/uint64, plus boolean values for ordinary
logic. Pointer/view escapes from local or copied storage are rejected; callers'
storage can be borrowed within the documented subset.

[Schema 2](SCHEMA2.md) now generates dispatch, account bundles, packed unsigned/
array/nested-struct codecs, clients using canonical types, layout history,
privilege/owner/identity checks, key relationships and explicit alias policies.
The [multi-state example](../examples/multi-state/README.md) passes native and
compiled-SBF validation, including multi-instruction rollback. This is the first
framework prototype; its API remains experimental.

## Whirlpools integration gaps

The upstream [tick state](https://github.com/orca-so/whirlpools/blob/main/programs/whirlpool/src/state/tick.rs)
uses signed tick indices and i128/u128 liquidity and fee values. Its
[wide-math implementation](https://github.com/orca-so/whirlpools/blob/main/programs/whirlpool/src/math/u256_math.rs)
is a separate reference for arithmetic. Native signed Go values and built-in
128/256-bit numbers are not available on chain here. Checked limb-based arithmetic
can use unsigned structs, methods and multiple results, but exact signed behavior,
overflow, division and rounding need differential tests against the pinned Rust.
Normal-Go `math/big` is suitable for off-chain test oracles, not the reachable
on-chain package graph. uint16 and general `int` variables are also unsupported.

Upstream [tick arrays](https://github.com/orca-so/whirlpools/blob/main/programs/whirlpool/src/state/tick_array.rs)
contain 88 ticks; a fixed tick record is 113 bytes. Arrays of structs/nested arrays
are unsupported and owned scalar array values are capped at 1 KiB. Schema-2
owned-value layouts also cap total encoded size at 1 KiB. Account-backed byte
views can access larger serialized buffers without copying the entire tick array
onto the stack, as the benchmark has now demonstrated. Generated view/codec
convenience remains future work.
Report the required operations/layouts rather than reducing tick coverage or
changing the account format merely to fit existing tests.

[SDK 2](SDK2.md) now provides checked classic-token references/transfers,
multi-seed PDA derivation/signing and bounded generic CPI, including two signer
groups. Select `gosvm new -schema 2 -sdk 2`; the default is still SDK 1 and existing
projects do not upgrade implicitly. Import `gosvm/sdk/pda`, `gosvm/sdk/cpi` and
`gosvm/sdk/token` from the pinned snapshot. The
[new proof](../results/compiler/2026-10-05-checked-token-lifecycle-final/README.md) passes
117 real-SBF scenarios in three repetitions, including token/System transfers,
current balances and rollback. Generated PDA/token constraints now support
typed scalar/key/literal seeds, token mint/authority relationships and named
state references; see [schema 2](SCHEMA2.md). The
[generated full swap](../examples/full-swap/README.md) retains all 108 manual
corpus scenarios/110 transactions in three SBF runs, with explicit framework
error mappings. SDK-2 rent reads, System creation/funding, and owned resize/close/reuse
now pass [35 scenarios/46 transactions](../results/compiler/2026-10-05-lifecycle2-supported/README.md)
in three real-SBF runs. Generated rent-funded initialization and authorized
owned-state closing now pass the [SOL escrow proof](../results/compiler/2026-10-05-escrow-supported/README.md).
Checked non-native token creation/closing and the [complete creator-managed
swap lifecycle](../results/compiler/2026-10-06-managed-swap-supported/README.md)
are now verified. The helper snapshot adds `token/lifecycle.go`; pin the complete
frontend/SDK together even though the Clock API-file hash is unchanged.
Protocol-specific liquidity/lifecycle policy still belongs to the port. Read the
current [SDK contracts](SDK2.md) before using these boundaries; they do not supply
application authorization. SDK-2 helper snapshots are experimental and must match the current
frontend; coordinate updates instead of mixing old snapshots with new helpers.
The checked token API does not cover Token-2022, wrapped native SOL accounts,
delegate-authorized transfers or
multisig. Coordinate those interfaces before wiring a complete contract.
Whirlpools' own discriminator and layout compatibility must be explicitly matched;
goSVM's default schema-2 discriminators do not imply Anchor compatibility.

Current bounds are 16 context accounts, 16 CPI metas, two signer groups with a
combined 1,024-byte encoding, and 16 seeds per group (including bump), each at most
32 bytes. The SDK fixture has a 3,648-byte static frame; measure
the actual port's frames and avoid copying large account buffers onto the stack.
Schema-2 packed wire fields do not currently accept boolean values either.

## First deliverables and coordination

1. Pin an upstream commit and toolchain/feature profile. These source links use
   `main` for orientation, not a benchmark revision.
2. Establish reproducible Rust builds and independent fixtures. Start with a
   classic-token swap path covering both directions, tick crossings, rounding,
   slippage/limits, overflow and exact failure behavior. Record omitted features.
3. Make the first SBF target a scoped classic-token swap with preloaded pool and
   tick accounts, not the entire pool/position lifecycle. Separate pure shared
   model/math from runtime/CPI integration. Port math and
   state transitions and compare them against the pinned Rust before CU claims.
   Native-Go scaffolding can progress while an unsupported on-chain operation is
   waiting for compiler/framework support.
4. Report each gap with a minimal failing Go program, upstream reference vector,
   required account layout/operation and expected behavior. Coordinate changes to
   `internal/compiler`, `internal/project` and `solana` with the milestone agent;
   those modules are under active development.
5. Execute the actual SBF artifact with matched account bytes, validation, CPI,
   errors and rollback. Measure scoped Rust and Go implementations with equivalent
   functionality; keep the full upstream artifact as a separately scoped reference.

Use a separate worktree if available or agree on file ownership. The port should
own its application/reference fixtures and report compiler/framework needs to the
milestone agent. Do not revert others' edits or silently weaken security checks.
Measure setup, empty/no-op/edited builds, native/SBF tests, successful/failed CU,
ELF/project/shared-tool footprints separately. No full-port completion, production
readiness claim has been established. The scoped optimistic CU target above
does not establish a production acceptance threshold.
