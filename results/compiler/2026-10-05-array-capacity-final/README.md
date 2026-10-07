# Scalar-array capacity validation

October 5, 2026, macOS arm64. Sixty actual-SBF scenarios passed in three repetitions
using runner 0.5.0 and platform-tools v1.51/SBFv3. Four independently compiled entries
exercise the full 1,024-byte per-value limit. Fourteen ordinary-Go vectors per kind
supply expected account bytes and panic outcomes; each suite adds a two-instruction
transaction to check rollback after an earlier successful copy.

| Kind | Elements | ELF bytes | Handler frame bytes |
| --- | ---: | ---: | ---: |
| byte | 1,024 | 2,144 | 2,112 |
| uint32 | 256 | 2,232 | 2,112 |
| uint64 | 128 | 2,360 | 2,112 |
| bool | 1,024 | 1,928 | 2,112 |

Zeroing, whole-array copies, mutations of the original, integer wrapping, first/last
and middle indexes, untouched-element reads, and failed reads/writes match native
Go. Five expected failures per variant retain all initial account bytes. Exact
case results, CU and logs repeat. All entry adapters use 64-byte static frames;
hidden memory helpers use zero-byte frames. Each instrumented object links to a
byte-identical tested ELF.

The [shared fixture source](../../../examples/array-capacity/program.go) has a
native `Check` dispatch; saved `entry.go.txt` files select one SBF handler at a time.
Combining all four 1 KiB workloads into one inlined function exceeds the SBF frame
budget. The per-array limit is not a guarantee for every function, call graph or
collection of locals. These freestanding memory loops establish correctness;
they are not tuned CU implementations or application benchmark results.

- [Summary and source hashes](summary.json).
- Variant fixtures, ELFs, native expectations, generated C, stack usage and three
  runtime reports: [byte](byte/fixtures.json), [uint32](uint32/fixtures.json),
  [uint64](uint64/fixtures.json), [bool](bool/fixtures.json).
- [Small-array semantics corpus and root/nested checks](../2026-10-05-arrays-supported/README.md).
- [Existing-workload regression](../../svm/2026-10-05-m2-arrays-regression-final/README.md).

Reproduce with `python3 scripts/array_capacity_verify.py --output results/compiler/new-capacity`.
The output must be new. Ordinary Go and the shared checked-in driver generate
expectations independently of generated C and VM output. Native-Go/C tests also
exercise all 56 vectors. Earlier evidence directories remain intact. No fresh
validator is used; [array support and limits](../../../docs/GO_ARRAYS.md) apply.
