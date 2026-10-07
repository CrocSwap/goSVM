# Pointer and method verification

October 5, 2026. The shared-package fixture preserves native-Go behavior for
caller-owned pointer updates, receiver copies and nil/bounds failures in actual
SBF execution. This is a compiler fixture, distinct from the bounded starter,
full token swap and dependency-heavy protocol workload.

| Check | Result |
| --- | --- |
| Native Go and generated C | 1,000 random vectors and 60 edge/failure vectors pass |
| Additional compiler checks | Imported methods and result-borrow identity, target evaluation, pointer-array len/cap, local/block/loop escapes and unsupported forms pass |
| Ordinary Go reference | 194 vectors; pre-panic state retained in the native report |
| Compiled SBF | 196 scenarios in each of three repetitions; exact account bytes and failures |
| Failure scenarios | 44, including nil, bounds, custom status and multi-instruction rollback |
| ELF | 3,600 bytes, SHA-256 `b4b53ed0faf6e3be0e326973aaef406fb57735d786bd682174726c82f0ee5975` |
| Static frames | Handler 64 bytes, entry 64 bytes, memory helpers 0 bytes; instrumented ELF hash is identical |

The program imports value/pointer receiver types and methods from a separate Go
package. Success cases check mutable aliases, copied structs, nested field and
array/byte-slice element addresses, returned caller borrows through multiple
results, explicit method expressions, nil-handling pointer receivers, composite
literal storage and captured assignment targets. Failure cases compare native-Go
and generated-C effects before panic; actual SBF must revert account state. Both
a nil failure and custom error after an earlier successful instruction roll back
the whole transaction.

[summary.json](summary.json) binds source hashes, tool commands, native reference,
fixture and ELF hashes, runner identity and measured frames. The native driver is
saved as text to keep it outside root Go test discovery. Logs and three runtime
reports accompany the summary. Source/artifact hashes were rechecked after the
verification sequence.

This uses the pinned runner 0.5.0 and installed v1.51 backend on macOS arm64. It is
not a fresh-validator comparison or a clean-host check. There is no general
pointer escape/heap implementation: the supported lexical borrowing subset is
specified in [the language reference](../../../docs/GO_POINTERS.md). Safe slice
views and schema-2/application authoring remain open.

Reproduce without overwriting this evidence:

```sh
python3 scripts/pointer_verify.py --output results/compiler/new-pointers
```
