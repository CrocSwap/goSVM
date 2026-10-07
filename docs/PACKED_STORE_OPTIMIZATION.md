# Checked packed-store optimization

October 6, 2026. Canonical SDK-2 compilation now recognizes a narrow unsigned
little-endian encoding loop and adds a checked fast path. The unchanged compact
Whirlpools handler improves from **1.737883× to 1.702520× Rust** at default O2.
All 103 successful cases improve; 23 failed cases improve and 40 are unchanged.
No path regresses. Median successful saving is **746 CU (1.34%)**, ranging from
443 to 9,230 CU. Go ELF increases **16 bytes**, to **84,208 bytes**; the largest
static frame remains **1,472 bytes**. These are event-omitting classic-token
fixed-fee handler results, separate from the starter/generated swap workloads.

[Canonical proof](../results/compiler/2026-10-06-packed-store-canonical-final/README.md)
records every case, source hash and command. The compiler produces the exact
ELF of the independently tested lowered-C write-only variant, SHA-256
`88c724968cb445c0709d02912e814b4aa4e3783634268e22d31536e79eeee54b`.
No benchmark source, default handler, original frontend/SDK pin, Rust reference,
math algorithm or application validation was changed. Matched account bytes,
metadata, errors, CPIs and transaction rollback pass all **166 cases × three**.
Actual Token consumed CU and non-budget log content remain identical; the
remaining budget supplied to Token changes with caller savings.

## Exact scope and semantics

The compiler recognizes a void function with three arguments: a byte slice,
uint64 offset, and uint32 or uint64 value. Its entire body must be this loop,
using four iterations for uint32 and eight for uint64:

```go
func Write64(b []byte, offset, value uint64) {
    for j := uint64(0); j < 8; j++ {
        b[offset+j] = byte(value >> (8 * j))
    }
}
```

Matching uses typed variable identities and constant values. Different strides,
start values, extra statements, shadowed values, calls and mutated arguments
retain their ordinary translation. Unsupported constructs remain rejected.
The optimization is enabled only for explicit SDK 2; SDK-1 and legacy output
remain unchanged. There is no new Go syntax, SDK API or unsafe application cast.

When the complete range is within the slice length, emitted C uses builtin
memcpy from the scalar value. This permits defined unaligned/alias-safe stores
and allows LLVM to optimize the encoding. The guard checks offset before
subtracting it from length, preserving unsigned-overflow safety. On insufficient
space, the original byte loop still executes: bytes written before an eventual
panic remain observable in native execution. SBF failures roll the transaction
back. Unknown/non-little-endian host compilation also retains the original loop.
The optimized LLVM IR contains a full-range guard and an alignment-1 scalar
store on the valid branch; this is not an assertion about instruction-level
attribution of the complete handler savings.

Implementation: [packed.go](../internal/compiler/packed.go). Independent
encoding/binary-based native/C tests check 12,000 vectors in both fast-path and
forced-fallback builds under undefined-behavior sanitization, covering all eight
byte alignments, nil/empty/short buffers, full-width offsets, canaries and partial
writes. Recognition tests reject similar loops with different semantics.

The [direct SBF proof](../results/compiler/2026-10-06-packed-store-probe/README.md)
passes **422 cases × three**: 64 successes and 358 failures. It checks packed
32/64-bit output, unaligned offsets, exact aborts, extreme offsets, short/empty
buffers and rollback both after partial writes and after successful writes
followed by an explicit application error.

## Controlled alternatives and regressions

The [isolated controls](../results/compiler/2026-10-06-packed-codegen-controls-final/README.md)
preserve the baseline ELF exactly and test each variant on 12,000 native
encoding/bounds vectors and the full 166-case SBF corpus three times:

| Variant | Median paired ratio | ELF bytes | Decision |
| --- | ---: | ---: | --- |
| Compact baseline | 1.737883 | 84,192 | Preserved |
| Packed reads | 1.742357 | 85,352 | Rejected; 61 successful paths regress |
| Packed writes | 1.702520 | 84,208 | Implemented |
| Key comparison pointer parameters | 1.737883 | 84,192 | Exact baseline ELF; no gain |
| Combined | 1.706915 | 85,368 | Rejected; read-related regressions remain |

Root `make test` and `go vet ./...` pass. The separate checked-token (117 cases),
managed swap (29/36 transactions), full swap/service (108/110) and escrow
(32/42) regressions each pass three SBF repetitions plus their native/scaffold
and stack checks. All four preserve prior ELF hashes; their encoding helpers
do not match this exact optimization shape. This does not establish a performance
improvement on arbitrary Go code. See the [proof README](../results/compiler/2026-10-06-packed-store-canonical-final/README.md)
for regression links and build observations.

Initial setup/harness attempts are retained separately: the first analysis used
an unwritable default Go cache; the first control driver omitted its boolean
typedef; the next control comparison failed to normalize remaining Token budget
in logs; the first canonical setup tested a module before copying its dependency.
No failed attempt is counted as validation. Corrected runs use new directories.

## Benchmark adoption and next work

Use the recorded new frontend and complete matching SDK snapshot in a separate
benchmark pin. The SDK helper/API contents are unchanged from the canonical
compact signer snapshot, but the frontend binary/cache identity changes.
Build the existing compact handler; no application encoding rewrite is required.
Preserve the original/compact baselines and compare complete case distributions.
The artifact is experimental and for the currently tested macOS arm64/SBFv3
toolchain, not a public release or a cross-platform verification claim.

The optimistic target remains **1.33×**, with **1.5×** as the intermediate
checkpoint. This change makes measurable progress but does not meet either.
Validated tick-view reuse remains application work; remaining arithmetic and
packed reads need fresh isolated evidence. The simple key-comparison parameter
change has been ruled out as a useful optimization on this artifact.
Independent developer acceptance remains the sole milestone-2 closure gate.
