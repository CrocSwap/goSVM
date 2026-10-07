# Compact single-signer performance prototype

This document preserves the earlier isolated owner-side prototype. The later
benchmark-tested variant is now an optional canonical SDK-2 helper; see
[canonical adoption](COMPACT_SIGNER_ADOPTION.md) for its contract and evidence.
In particular, canonical `InvokeSingle` accepts a nil signer for an unsigned
invocation and offers `AddBytes`/`AddByte`; the older prototype below rejected
nil and included additional convenience methods. The prototype script and its
historical results remain unchanged.

The Whirlpools agent's [signer-copy reproduction](../../goSVM-benchmarks/docs/SDK_COPY_VALIDATION.md)
identified avoidable copying between a 529-byte seed builder and a 1,024-byte
general signer builder. The goSVM-side
[controlled experiment](../results/compiler/2026-10-06-single-signer-supported/README.md)
now tests an isolated compact builder with the same flat serialization and
unchanged low-level invoke boundary. No compiler extension was needed.

The proposed implementation is
[`scripts/single_signer_prototype.go.txt`](../scripts/single_signer_prototype.go.txt).
It was staged into an isolated SDK copy as `sdk/cpi/single.go`; that experiment
did not alter canonical SDK snapshots or existing benchmark pins.

Proposed use in a new derived snapshot:

```go
var signer cpi.SingleSigner
// Check AddBytes/AddKey/AddByte/AddUint32/AddUint64 statuses normally.
code := signer.AddBytes(prefix[:])
if code != 0 { return code }
// Add the remaining application seeds and explicit bump.
code = cpi.InvokeSingle(c, tokenProgram, &metas, instruction[:], &signer)
```

`SingleSigner` supports at most 16 seeds of at most 32 bytes, including bump;
its encoded maximum is 530 bytes, below the existing 1,024-byte combined limit.
It returns seed errors 3001. Invoke rejects nil metas with 3002 and nil signer
with 3003, then uses existing `solana.Invoke` validation and exact error/status
behavior. Bytes borrow the backing builder; keep it alive and stable through
invocation. These are the earlier prototype's semantics. The existing general
signer builder remains required for two groups.

In one actual-SBF test program, original/compact construction measures
5,590/3,327 CU and original/compact signed System transfer 6,357/4,019 CU. Those
are probe comparisons, not full-handler attribution. Native independent wire
comparisons and repeated actual-SBF errors/rollback pass. At that checkpoint,
full Token/Whirlpools integration and performance were still required. The
benchmark subsequently validated its compact variant in a separate derived
snapshot across 166 handler scenarios in three runs; see the adoption evidence
above. Preserved baseline pins are not overwritten.

Tick-view validation reuse remains application work. A checked view needs to bind
the exact backing buffer and pool key and preserve mutation/alias lifetime rules.
Independently callable checked `Apply` must remain. This investigation neither
removes validations nor asks the compiler to cache checks across arbitrary writes.
