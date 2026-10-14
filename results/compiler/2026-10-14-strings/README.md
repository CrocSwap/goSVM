# Strings and `error` on SBF: fogo-v0 and solana-v3

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_STRINGS.md](../../../docs/GO_STRINGS.md).

## Question

Do strings (literals, `len`, indexing, slicing, concatenation and `+=`,
comparisons, conversions to and from bytes, runes and rune slices, `range`
with UTF-8 decoding, `switch`, strings in fields, arrays, slices, pointers,
interfaces and generic code) and `error` values behave on SBF exactly as in
native Go, do bounds failures abort and roll back, and does every allocating
string operation consume exactly the documented heap bytes?

## Method

`TestStringSBF` (`internal/compiler/strings_sbf_test.go`) compiles
[examples/string-values](../../../examples/string-values/program.go) with a
Context entrypoint (account 0 is the 1 KiB output, instruction data is the
input) for each target with platform-tools v1.57 and the default 32 KiB heap,
and runs general fixtures through the pinned runners (fogo-v0:
gosvm-svm-runner 0.5.0 / LiteSVM 0.8.2; solana-v3: 0.6.0 / LiteSVM 0.13.0):

- 488 vectors from `StringVectorsForTest` covering all nine modes (basics,
  concatenation and conversions, comparisons, rune conversions, range,
  containers and switch, errors and interfaces, generics, heap loops),
  including inputs over a small alphabet (shared prefixes) and inputs built
  from 32 UTF-8 edge sequences (overlong forms, surrogates, U+10FFFF and
  beyond, truncated sequences, stray continuation bytes), each also ranged
  over and converted to `[]rune` alone and doubled. Expected account bytes come
  from running the same `Run` natively; the 86 vectors that panic in Go
  (string index and slice bounds, a failed `x.(string)`, `Error()` through a
  nil pointer, an empty generic `s[0]`, an out-of-range `+=` target) must fail
  with `ProgramFailedToComplete`, log `SBF program panicked` once and roll back.
- Heap boundaries for eight kinds of loop body, each with operands of 5 and 3
  bytes and of 32 and 32 bytes: concatenation `a + b`, `string(b)`,
  `[]byte(s)`, `string(rune)`, `[]rune(s)`, `a + ""` (allocates nothing),
  boxing a string, and `[]byte` of a constant. The predicted number of
  iterations fits; one more aborts with rollback (14 out-of-memory cases);
  `a + ""` completes 6,000 iterations. These transactions set a 1,400,000 CU
  limit (`SetComputeUnitLimit`); thousands of allocations exceed the default.

```sh
GOSVM_STRING_RESULTS=/tmp/string-results go test -ldflags=-linkmode=external \
  -run TestStringSBF ./internal/compiler/
```

(macOS needs the external linker flag; `make test` runs the test when the v1.57
backend and runners are installed and skips it otherwise.) Fixtures, ELFs and
runner reports stay local; [metrics.json](metrics.json) keeps the summaries.

Host-side, `TestStringDifferential` compares 14,116 vectors (3,341 Go panics)
natively and as host-compiled C, `TestStringHeap` checks 105 heap boundaries
(seven operand sizes), and `TestStringIntegerConversions` checks `string(x)`
for all ten integer types and 226 values against Go's conversion.

## Results

All 518 cases pass on both targets (100 aborts with rollback: 86 Go panics and
14 out-of-memory boundaries).

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Probe ELF SHA-256 | `998161cc…` | `70174787…` |
| Highest vector CU | 97,102 | 97,035 |
| CU per iteration, `a + b` (5 + 3 bytes; 4,093 fit) | 214.1 | 210.1 |
| CU per iteration, `a + b` (32 + 32 bytes; 510 fit) | 216.9 | 212.9 |
| CU per iteration, `a + ""` (no allocation) | 81.1 | 78.1 |
| CU per iteration, `string(b)` (5 bytes) | 105.1 | 76.1 |
| CU per iteration, `[]byte(s)` (5 bytes) | 116.1 | 117.1 |
| CU per iteration, `string(rune)` (4,093 fit) | 71.1 | 64.1 |
| CU per iteration, `[]rune(s)` (5 runes) | 315.4 | 316.3 |
| CU per iteration, boxing a string (2,046 fit) | 38.2 | 35.2 |
| CU per iteration, `[]byte("constant!")` | 138.2 | 135.2 |

Per-iteration figures include the loop, the `switch` on the kind, two output
stores and `len` of the result; the boxing row (no copy of bytes) approximates
that overhead. Concatenation is dominated by the general `gosvm_concat` helper
(operand array, counting pass, allocation, copy pass), not by the bytes copied.

ELF hashes are snapshots of this commit's example and runtime. Programs without
strings are unchanged: the fresh-rebuild check
(`scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json`)
reproduces full-swap fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…` and clock2
on both targets; `TestInterfaceSBF` reproduces the interface probe ELFs
(`c7daf643…`, `8e88eb9e…`); `TestMixedSBF` and `TestAllocationSBF` pass; and
the frozen corpus keeps every entry except three recorded changes.
