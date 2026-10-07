# Optional compact signer in canonical SDK 2

October 6, 2026. Canonical SDK 2 now includes the exact `cpi.SingleSigner` and
`cpi.InvokeSingle` implementation validated by the independent Whirlpools
benchmark. The canonical `sdk/cpi/cpi.go` is byte-identical to that derived
snapshot's file, SHA-256
`83f5fccd880bc9f5abbbd419a94c61853c3074b95593ef2ede3a8476dbd9dd02`.
No compiler or low-level invoke change was needed. Existing general signer APIs
and generated handler behavior are preserved; applications opt in explicitly.

## Contract and adoption

```go
var signer cpi.SingleSigner
if code := signer.AddBytes(prefix[:]); code != 0 {
    return code
}
if code := signer.AddByte(bump); code != 0 {
    return code
}
return cpi.InvokeSingle(c, tokenProgram, &metas, instruction[:], &signer)
```

The builder offers `AddBytes`, `AddByte` and borrowed `Bytes`. One group accepts
at most 16 seeds of at most 32 bytes, including bump, with a 530-byte maximum
encoding. Invalid additions return 3001 without changing the builder. Keep its
backing value alive and unmodified through invocation. Nil metas return 3002;
nil signer invokes without PDA signing. A nonnil empty builder encodes `[1, 0]`.
This is the benchmark-tested contract; the
[earlier owner prototype](SINGLE_SIGNER_INVESTIGATION.md) rejected nil signers
and offered additional convenience methods. Its historical script is not the
canonical implementation.

Create a fresh `gosvm new -schema 2 -sdk 2` project with the matching frontend,
or explicitly adopt and record a complete matching frontend/SDK snapshot.
Strict generation detects helper drift. The low-level Clock API-file SHA remains
`07f4f3f188bdadd803b1fa7d5350b2216810f5aefba74431d6d964fa5dd25012`;
that hash alone does not identify this helper revision. The local full-swap and
escrow SDK-2 snapshots were explicitly refreshed. SDK 1 remains frozen.
The benchmark's original pin, derived pin and default handler are untouched.

## Full-handler evidence

The independent [benchmark report](../../goSVM-benchmarks/docs/COMPACT_SIGNER.md)
compares the preserved fee-growth handler with the compact variant against
unchanged scoped and full Rust references: 166 cases, three repetitions.
The event-omitting classic-token fixed-fee handler's median paired CU ratio falls
from 1.81 to 1.74. All 103 successes save 2,418 CU; 20 failures improve and 43
remain unchanged. Go ELF size falls 256 bytes to 84,192. The transfer helper's
static frame falls from 1,344 to 256 bytes; the largest frame remains 1,472.
An SDK-only control reproduces baseline ELF and CU exactly.

A separate [canonical rebuild](../results/compiler/2026-10-06-compact-signer-canonical-final/README.md)
copies the handler/shared module into a goSVM-owned directory, builds a fresh
canonical CLI and SDK-2 scaffold, and executes all 166 cases three times.
It reproduces the benchmark ELF byte for byte, SHA-256
`19daf911c6348dbb6ba86df8452a0637fe6c415ce9322d582a91232741e83bad`,
and matches every case's errors, CU and logs. Fixtures, runtime/transaction-maker
hashes, features and compute budget also match the preserved reference report.
This rebuild reuses the independently validated corpus; it does not rerun or
modify the Rust references. Source hashes, commands and native/SBF logs are
recorded. The sibling checkout is only read.

## Canonical regressions

Root `make test` and `go vet ./...` pass. Native tests cover 10,000 independently
encoded seed sequences against the general builder, rejected additions,
maximum capacity, signed/unsigned invocation encoding, guards and raw status
propagation. Project tests confirm fresh SDK-2 scaffolds expose the new type.
The separate application modules additionally pass native/scaffold/SBF checks:

| Regression | Scenarios | SBF repetitions | ELF bytes |
| --- | ---: | ---: | ---: |
| [Checked token](../results/compiler/2026-10-06-checked-token-compact-canonical/README.md) | 117 | 3 | 14,784 |
| [Managed swap lifecycle](../results/compiler/2026-10-06-managed-swap-compact-canonical/README.md) | 29 / 36 transactions | 3 | 68,320 |
| [Full swap and ordinary-Go service](../results/compiler/2026-10-06-framework-swap-compact-canonical/README.md) | 108 / 110 transactions | 3 | 68,320 |
| [Escrow lifecycle](../results/compiler/2026-10-06-escrow-compact-canonical/README.md) | 32 / 42 transactions | 3 | 25,280 |

These existing applications do not opt into the compact helper. Every ELF hash
matches its previous checkpoint, preserving default behavior. These are distinct
workloads from the bounded starter and the Whirlpools handler.

The first canonical setup attempt stopped before building because its script
assumed a root `go.sum`; the dependency-free root has none. That attempt is
preserved separately. A corrected proof passed, followed by the final run adding
explicit reference runtime/fixture pin checks. Historical results are retained.

Independent developer acceptance remains the sole milestone-2 closure gate.
Tick-view validation reuse remains application work; no validation was removed.

The exact 1.74× canonical rebuild above is a preserved checkpoint. A later
[packed-store compiler optimization](PACKED_STORE_OPTIMIZATION.md) verifies
1.702520× on the unchanged compact handler. Its separate proof/script targets
the newer frontend; the old canonical rebuild script's exact-ELF assertion
intentionally detects such code-generation changes.
