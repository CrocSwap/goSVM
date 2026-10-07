# Phoenix compiler and return-data dependencies

October 6, 2026. The benchmark owner flagged two blockers in its preserved
[v0 multiplication handoff](../../goSVM-benchmarks/docs/PHOENIX_V0_MULTIPLY_HANDOFF.md)
and [placement return-data handoff](../../goSVM-benchmarks/docs/PHOENIX_RETURN_DATA_HANDOFF.md).
The owner-side implementation adds legacy helper linking and an SDK-2 setter.
It does not implement Phoenix placement; that application work remains with the
benchmark owner. Existing benchmark sources, historical ELFs, frontend pins and
runner packages remain frozen.

## Legacy multiplication and import validation

LLVM v1.51 can lower checked `uint64` multiplication to `__multi3` for SBF v0.
That is a compiler libcall, not a Solana syscall. The frontend now inspects the
compiled object and links its freestanding helper only when needed. The helper
uses unsigned 32-bit limbs and returns the product modulo 2^128. It needs no
Go runtime, libc, allocation or recursive multiplication helper. This does not
add a Go `uint128` type. The v0 CPU remains `generic`; O2 remains unchanged.

After linking, the compiler rejects unresolved symbols other than the explicit
runtime syscalls it emits. V3 retains its `--no-undefined` linker policy. Invalid
builds are rejected before publishing an ELF, rather than failing when the VM
reaches an unresolved call. This guard also exposed `__moddi3` from a signed
CPI meta-length remainder. Lengths are nonnegative; the SDK-2 C boundary now
uses unsigned bounds and remainder with the same valid length/error semantics.
SDK 1 retains its API and account layout.

[Native verification](../results/compiler/2026-10-06-phoenix-dependencies-final/focused-native.log)
checks 6,096 complete 128-bit helper products against `math/big`, with undefined
behavior sanitization. The [complete helper ABI SBF proof](../results/compiler/2026-10-06-multi3-sbf/summary.json)
passes 633 edge/random products with nonzero high limbs, for both targets × three
runs, checking exact simulation/committed account bytes. Disassembly verifies
that the helper itself has no recursive call or import. Its static frame is zero.
The final combined proof additionally checks 580 reduced Go checked-multiply
cases per target × three, preserving overflow aborts and rollback.

The [combined proof](../results/compiler/2026-10-06-phoenix-dependencies-final/summary.json)
rebuilds the accepted IOC/FOK/free-funds trading application with a matching new
SDK, preserves its Rust references, and compares all 631 matched scenarios across
Go v0/O2, Go v3/O2, scoped Rust v0 and full Rust v0 in three runs each. State,
errors, CPI logs and rollback assertions remain intact. The median paired successful-case CU ratios are **1.896918×** for Go v0/Rust v0
and **1.231522×** for Go v3/Rust v0. Default v3 CU is unchanged on every case.
Go v0's ELF is 45,512 bytes with a 2,560-byte maximum frame; v3 remains 44,880
bytes with a 2,496-byte maximum frame. The original failed v0
control remains excluded from accepted distributions. This workload omits events
in Go/scoped Rust and is separate from cancellation and Whirlpools metrics.

## SDK-2 return-data setter

```go
code := solana.SetReturnData(c, payload)
```

The setter accepts **0..1,024 exact bytes**, matching the runtime
[syscall limit](https://solana.com/docs/core/programs/syscall-reference).
Success returns 0. Oversized data returns 2017 before invoking the syscall;
a missing native callback returns 2009. Empty data clears current return bytes.
V0 calls `sol_set_return_data`; v3 uses its static syscall hash `0xa226d3eb`.
The runtime copies the bytes synchronously, so later buffer mutation does not
change the returned payload. No byte order conversion or order-ID encoding is
performed by the SDK.

Native contexts supply `WriteReturnData func(data []byte)`. The callback receives
an owned copy; it cannot mutate the caller's input and need not retain its backing
buffer. Native adapters model program identity, instruction/transaction reset
and CPI clearing in their invocation callbacks, as they model other CPI effects.
The setter is SDK 2 only. Existing SDK-2 example snapshots were explicitly
refreshed with the matching API; old project snapshots are not silently upgraded.

Set final IDs **after every CPI and final state update**. A CPI can clear earlier
return data or replace it with the callee's bytes/program identity. The proof
checks both paths and resetting it in the parent afterward. Phoenix should
retain upstream's conditional setter call: an empty ID list does not set or clear
return data. The SDK's empty-payload clear operation is a separate explicit action. Returned metadata
from a failed transaction is distinct from persisted account state: account
writes roll back, while failure metadata can still contain bytes already set.
The proof preserves and checks that distinction.

The native/C differential covers 165 lengths/payloads; additional native tests
check copying, empty data, oversized rejection, unavailable callbacks and guard
precedence. The actual SBF proof uses the owner's unchanged 20-byte serialized
order-ID reproduction and a wider probe covering 0/1/20/1,023/1,024/1,025 bytes,
later buffer mutation, clear, silent/returning/failing CPI, setting after CPI,
guard failure, account rollback and later instruction/transaction clearing.
Both v0 and v3 check exact bytes **and program identity in simulation and
submitted transaction metadata**, in three runs each. Boundary payloads are
preloaded in account data so the legacy transaction packets remain valid.

## Runner observability

The source runner now exposes `value.returnData` in `simulateTransaction` and a
small `getTransaction(signature)` metadata response (`err`, `returnData`,
`logMessages`, `computeUnitsConsumed`). This is synchronous test metadata, not a
ledger or a full Solana RPC implementation. Empty bytes are represented as null;
nonempty bytes use `programId` and `[base64, "base64"]`. Reset/snapshot restores
transaction metadata history alongside statuses and VM state. Six Rust runner
tests pass, including exact serialization and reset isolation.

The existing runner packages/pins are unchanged. The combined proof ships a
separately hashed return-data runner binary and source; adopt that pin explicitly
for these metadata assertions. `scripts/return_data_svm.py` checks both simulated
and submitted return bytes rather than accepting an omitted field as empty.
Standard comparative trading runs use the preserved original runner.

## Regressions and handoff

Root `make test` and `go vet ./...` pass. Separate generated modules pass native,
service and actual-SBF regressions; root tests alone do not cover those modules.

| SDK-2 corpus | Scenarios × three SBF runs | Evidence |
|---|---:|---|
| Checked token/CPI | 117 | [report](../results/compiler/2026-10-06-checked-token-return-data-final/summary.json) |
| Escrow | 32 / 42 transactions | [report](../results/compiler/2026-10-06-escrow-return-data-final/summary.json) |
| Managed swap | 29 / 36 transactions | [report](../results/compiler/2026-10-06-managed-swap-return-data-final/summary.json) |
| Generated swap/service | 108 / 110 transactions | [report](../results/compiler/2026-10-06-framework-swap-return-data-final/summary.json) |
| Optimized Whirlpools handler | 166 against both Rust references | [report](../results/compiler/2026-10-06-whirlpool-return-data-final/summary.json) |

The unsigned CPI guard causes a small v3 Whirlpools artifact/CU change: paired
successful-case median is **1.281201×** (prior 1.280853×); per-case changes range
from −3 to +10 CU. The new ELF is 83,480 bytes (+56), largest frame 2,368 bytes.
The previous optimization snapshot remains preserved. This is the fixed-fee
classic-token handler with events omitted, not a full-contract measurement.

Adopt a **new complete frontend/SDK pin**, preserving every older benchmark pin.
Both the C helper/linking change and SDK C boundary matter; an API hash alone is
insufficient. The [complete frontend archive](../results/compiler/2026-10-06-phoenix-dependencies-final/frontend.tar.gz)
ships the verified executable, matching SDK and full frontend source with a
121-file manifest. Its SHA-256 is
`37fcbe26a0d4fbcf744809abb7e194463c03aab96c7729acaceac31fbf42b3c1`;
the CLI SHA-256 is
`bb1c3b28e05cb8f1862cd7dddbb4a4ef508e4a957089934e51eda9e132d28294`.
The SDK API SHA-256 is
`c16d2232c17339649c0358ccf3919c20ec1dd1ac2972ec9816480af2d525dadd`.
The [return-data runner binary](../results/compiler/2026-10-06-phoenix-dependencies-final/return-data-runner)
has SHA-256
`bee1c54736539e973be42c6e76ccd37c9d8c54898830a666e2c49c4f78674479`;
its [complete patched source archive](../results/compiler/2026-10-06-phoenix-dependencies-final/return-data-runner-source.tar.gz)
has SHA-256
`266c3136f2d59ab69999c96138cfb369e6763a99aa87ed1b79cf9a77107c338c`.
The [delivery audit](../results/compiler/2026-10-06-phoenix-dependencies-final/delivery-verify.json)
checks manifest hashes, executable modes and exact v0 probe/multiply ELF rebuilds
with both extracted and source-rebuilt tooling. Native CLI byte reproducibility
is not required or claimed; the archive carries the exact verified executable.
 Refresh the SDK snapshot explicitly rather than mixing the new
frontend with an older API. The archive includes local macOS arm64 tooling;
installed LLVM v1.51 and the pinned runner/features remain separate dependencies.

```sh
python3 scripts/phoenix_dependencies_verify.py \
  --runner build/phoenix-sdk-target/release/gosvm-svm-runner \
  --output results/compiler/<new-proof>
```

The combined driver requires the preserved sibling fixture/source/reference
hashes to match. It writes only inside goSVM. The first incomplete run is retained:
all multiply/trading comparisons passed, then the v0 CPI build guard rejected
`__moddi3`; the unsigned length fix is validated in the final run. None of these
local LiteSVM checks is presented as fresh-validator or clean-host acceptance.
