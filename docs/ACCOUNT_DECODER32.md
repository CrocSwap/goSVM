# SDK-2 incoming account decoder

October 6, 2026. Phoenix accepts extra incoming account metas, but the old Go
frontend returned decoder error 1001 for 17 accounts before calling its handler.
The standard four-account Phoenix benchmark was unaffected.

The SDK-2 frontend now accepts **up to 32 incoming descriptors**, including
duplicates. The account table and decoder guard use the same SDK-selected bound.
SDK 1 remains at 16. Outgoing CPI metas and schema-2 declared accounts retain
their separate 16-account limits; the schema diagnostic now names its own bound.
Custom handlers can accept extras; generated handlers still check their declared
account count. This is an experimental goSVM bound, not Solana's maximum.

The decoder retains aliasing, key/owner/data references, privilege flags, original
length metadata, data/instruction size guards and duplicate-reference checks.
The change adds 896 bytes of account-descriptor storage to SDK-2 contexts. Tested
static frames remain below 4,096 bytes: Phoenix 2,432, Whirlpools 2,368, checked
token 3,648, escrow 2,880 and generated/managed swap 3,968. Programs with other
inlining/frame behavior still need their own SBF build and frame check.

## Verification

The [decoder/Phoenix proof](../results/compiler/2026-10-06-account-decoder32-final/summary.json)
contains the native decoder checks, emitted C, ELFs, static frames, fixtures,
source/artifact pins, logs and actual SBF reports. The native loader suite checks
384 SDK-1/SDK-2 vectors with undefined-behavior sanitization, including 32 unique
accounts, malformed self/forward references, alias chains, flags, write effects
and handler count checks. Actual SBF runs cover counts 0/1/4/16/17/31/32/33,
readonly rejection, alias writes and rollback, signed System transfers using
indices beyond 15, failed CPIs and transaction rollback. Counts above 32 still
return decoder error 1001.

The 31/32-account SBF fixtures use repeated keys to fit valid legacy transaction
packets; the native serialized tests separately exercise 32 unique descriptors.
No oversized legacy transaction is presented as a successful SBF test.
SDK-1 no-op compilation reproduces the old ELF exactly, and its >16 rejection
continues to pass. Compiling Phoenix with the frozen old frontend reproduces
its accepted baseline ELF exactly before comparison with the new frontend.

Phoenix's real 16/17-account header-update reproduction passes Go, scoped Rust
and upstream Rust in three runs each. The standard four-account corpus also
passes **430 cases × three runs** for all three implementations. Every standard
case retains its exact baseline CU, error and committed state assertions; the
[baseline audit](../results/compiler/2026-10-06-account-decoder32-final/baseline-identity-audit.json)
also checks normalized logs. Phoenix's median paired successful-case CU ratio
remains **1.3701157742402315×**. The new Go ELF stays 25,760 bytes.

The [optimized Whirlpools regression](../results/compiler/2026-10-06-whirlpool-decoder32/summary.json)
passes all 166 fixed-fee classic-token, event-omitting handler cases × three
against unchanged scoped/full Rust, with exact errors, state assertions, CPI logs
and rollback. CU is unchanged on every case: median paired successful-case ratio
**1.2808531366340767×**. Its new ELF remains 83,424 bytes, with SHA-256
`f4b7cdf0c18addc7de5d22ad3aa7c8cbdcc25ca5de1b9499a20a1e60173c7c34`.
This is a new frontend artifact, separate from the earlier optimization snapshot.

Existing SDK-2 applications pass full three-run SBF regressions:

| Corpus | Scenarios | Evidence |
|---|---:|---|
| Checked token/CPI | 117 | [results](../results/compiler/2026-10-06-checked-token-decoder32/summary.json) |
| Escrow lifecycle | 32 / 42 transactions | [results](../results/compiler/2026-10-06-escrow-decoder32/summary.json) |
| Managed swap lifecycle | 29 / 36 transactions | [results](../results/compiler/2026-10-06-managed-swap-decoder32/summary.json) |
| Generated swap/service | 108 / 110 transactions | [results](../results/compiler/2026-10-06-framework-swap-decoder32-final/summary.json) |

The generated swap's historical 17-account failure changes from decoder 1001 to
handler account-count error 6000. Its proof records that specific mapping and
validates it natively too; state and rollback expectations remain intact.
Root `make test` and `go vet ./...` pass; their logs are in the decoder proof.
The separately generated modules were tested by the application drivers above.

Three unsuccessful setup attempts are retained and labeled: the initial legacy
packet-limit fixture, the fee-payer authority balance fixture, and the old
17-account generated-handler expectation. They are not counted as passing proof.

## Benchmark-owner handoff

Rebuild using a **new complete frontend/SDK pin**. A SDK Go API hash alone does
not identify this change: the incoming account table lives in the compiler's C
runtime templates. Preserve the original benchmark pin and outputs. The new
snapshot is local macOS arm64 tooling; it does not change O2 or runtime pins.

The [complete frontend archive](../results/compiler/2026-10-06-account-decoder32-final/frontend.tar.gz)
includes the executable CLI, matching SDK, full frontend source and file manifest.
Its SHA-256 is
`1da9e93a68915a25dd7b361dc2391e6c7e817ac439c9ef0b3769676f22a72909`;
the CLI SHA-256 is
`ba8d8dcb38e0acafad25c44ad5ad812e83cc7434a32bda6b5cb31bb965d6bbd1`.
The [Phoenix source snapshot](../results/compiler/2026-10-06-account-decoder32-final/phoenix-source.tar.gz)
preserves the unchanged application with the matching local SDK. The extracted
frontend archive's manifest and executable mode were verified, and rebuilding
Phoenix with its CLI reproduces the measured new ELF exactly; see the
[delivery check](../results/compiler/2026-10-06-account-decoder32-final/delivery-verify.json).
Rebuilding the CLI from the archived source also reproduces the exact Phoenix
ELF. The rebuilt native CLI has a different binary hash; the archive ships the
exact verified CLI and its manifest rather than claiming native CLI byte reproducibility.

Reproduce the owner-side comparison without writing to the sibling benchmark:

```sh
python3 scripts/account_decoder_verify.py \
  --output results/compiler/<new-decoder-run>
python3 scripts/account_decoder_whirlpool_verify.py \
  --cli build/account-decoder/<new-decoder-run>/gosvm \
  --output results/compiler/<new-whirlpool-run>
```

The scripts require the recorded sibling source/reference artifacts to remain
present and match their accepted hashes. All writes stay in goSVM. Standard
Phoenix measurements and extended-account correctness are separate evidence.
