# Borrowed byte view verification

October 5, 2026. Native Go, generated C and actual SDK/SBF execution agree on
bounded byte views, capacities and shared backing-storage updates in this corpus.
This is a compiler/SDK fixture, distinct from the bounded starter, full token
swap and dependency-heavy protocol benchmark.

| Check | Result |
| --- | --- |
| Native Go and generated C | 1,000 random comparisons and 149 edge/boundary comparisons pass |
| Additional compiler checks | SDK capacities, nil contexts, 24-result descriptor forwarding, 1 KiB reslicing and source-located lifetime/type rejections pass |
| Native SDK reference | 262 vectors using actual Go packages; supplied buffers match serialized lengths/capacities |
| Compiled SBF | 264 scenarios in each of three repetitions; exact account bytes/errors and deterministic CU/logs |
| Failure scenarios | 75, including bounds, nil pointer, custom status and multi-instruction rollback |
| SDK syscall | Real SHA-256 of nil input into a local array view matches Go crypto/sha256 |
| ELF | 6,280 bytes, SHA-256 `8aa0a05f5d8f6dc2da9c19b9f93273d61846313f7f019e7f53d4721f1ab0a471` |
| Static frames | Entry 1,024 bytes, library handler 1,088 bytes, memory helpers 0; instrumented ELF hash is identical |

The entrypoint verifies actual SDK capacities and forwards into a shared Go
package. The model returns caller-borrowing views through methods and multiple
results. Successes exercise aliases, independent array copies, conversion,
reslicing beyond length within capacity, nil/non-nil empty slices and the 1 KiB
array boundary. Explicit bound calls and element writes have native-Go/generated-C
pre-panic comparisons. Actual SBF errors must roll account state back; a prior
successful instruction is rolled back by both nil failure and custom status.

[summary.json](summary.json) binds source hashes, exact commands, native reference,
fixture/ELF hashes, runner identity and frame measurements. The saved native driver
is text; build staging remains outside root Go test discovery. Its Context uses
standard Go SHA-256 and buffers explicitly capped at serialized byte lengths.
Sources and linked artifacts were rechecked after the sequence.

The [initial attempt](../2026-10-05-slice-views/README.md) stopped because decoded
native input buffers had extra capacity. The subsequent successful experiment is
preserved separately; this snapshot also verifies aggregate copy support for
borrowed descriptors without array declarations. The frame bounds apply to this
fixture, not every program. The [language reference](../../../docs/GO_VIEWS.md)
states the supported byte/lifetime subset and remaining restrictions.

This uses pinned runner 0.5.0 and v1.51 LLVM on macOS arm64; it is not a fresh
validator or clean-host check. Schema-2/application authoring remains open.

```sh
python3 scripts/slice_verify.py --output results/compiler/new-views
```
