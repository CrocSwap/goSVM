# Value types: struct `==`, aliases, composite arrays and `panic`

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_TYPES.md](../../../docs/GO_TYPES.md),
[GO_ARRAYS.md](../../../docs/GO_ARRAYS.md),
[ERROR_CONTRACT.md](../../../docs/ERROR_CONTRACT.md#panic).

## Question

The [library survey](../2026-10-14-library-shakeout/README.md) found four
language gaps that block `lukechampine.com/uint128`, `holiman/uint256`,
`KyberNetwork/int256` and the Uniswap-v3 math: struct `==`, type aliases,
arrays of composite elements and `panic`. With Go-exact semantics, do they
behave on the host and on SBF (fogo-v0 and solana-v3) as native Go does, do
panics fail the transaction with rollback and a contracted log line, what do
they cost, and are programs that do not use them unchanged byte for byte?

## Method

- [`examples/value-types`](../../../examples/value-types/program.go) is
  ordinary Go with five modes: struct comparisons of every field kind (blank
  fields, interfaces, pointers, strings, arrays, nested structs, `struct{}`,
  zero-length arrays, `switch` on structs, interfaces holding structs and
  arrays, an uncomparable interface field that panics); arrays of structs,
  arrays, pointers (including to their own type), slices, maps and strings
  (literals, `[...]` and keyed, element and field updates with `=`, `op=` and
  `++`, copies, methods on elements, `&a[i]`, ranges over copies, pointers and
  index-only over an out-of-range row, slicing, slices of arrays, map keys and
  values, nil-pointer, nil-map and bounds failures); every kind of alias; 20
  ways to panic (constant, string, signed, unsigned, named, bool, `error`,
  struct, `nil`, nil `error`, a 600-byte string with a cut UTF-8 sequence, in
  closures, methods, generic code, deferred calls and nested calls); and a
  uint128/uint256-style core (`[...]Int` tables, `[4]uint64` arithmetic,
  struct `==`, overflow and underflow panics).
- `TestValueTypesDifferential` runs 10,200 vectors natively and as
  host-compiled C (default 32 KiB heap); output bytes, status and, for each
  panic, the log line (`ExpectedPanicLine` derives Go's line from the
  recovered value) must match.
- `TestValueTypesSBF` compiles the example with a Context entrypoint for each
  target (platform-tools v1.57; runners 0.5.0 for fogo-v0 and 0.6.0 for
  solana-v3, installed from the retained packages into a scratch
  `GOSVM_CACHE`) and runs 400 vectors (`ValueTypesVectorsForTest`) with a
  1,400,000 CU limit: completions must leave native Go's bytes; panics must
  fail with `ProgramFailedToComplete`, log `SBF program panicked` once, roll
  back, and (for `panic` calls) log `Program log: <line>` exactly once. It also
  runs a compute probe (`valueTypesCUSource`).
- `TestValueTypesAcceptedForms`, `TestValueTypesRejections`,
  `TestCrossPackageAliases` (aliases of another package's types,
  `type Uint160 = wide.Uint`, `type Named Uint160` with methods, run on the
  host), `TestCompositeArrayEscapes`, `TestPanicSites` and `TestPanicLogLine`
  cover Check, the backend view and the log line's truncation and UTF-8
  replacement; `internal/features` tests the new `uses` flags.
- `scripts/verify_rebuild.py` rebuilt the full token swap and `clock2` from
  two fresh directories with `-no-cache` and compared them with
  [the v1.57 reference](../2026-10-14-v157-rebuild/metrics.json).
- The library peel (`results/compiler/2026-10-14-library-shakeout/peel`) was
  rerun on the unchanged pass-2 workspace with the new compiler
  ([peel-pass2.jsonl](peel-pass2.jsonl)).

```sh
GOSVM_CACHE=<scratch cache with both runners> \
  go test -ldflags=-linkmode=external -run 'ValueTypes|PanicSites|PanicLogLine|CrossPackageAliases|CompositeArrayEscapes' -v ./internal/compiler/
GOSVM_CACHE=<scratch cache> make test
python3 scripts/verify_rebuild.py --output <new dir> \
  --reference results/compiler/2026-10-14-v157-rebuild/metrics.json
```

Fixtures, ELFs and runner reports stay local; [metrics.json](metrics.json)
keeps the summaries.

## Results

All pass. Host: 10,200 vectors match native Go, including 4,128 Go panics
(2,686 of them `panic` calls whose log line matches). SBF: 400 cases on each
target match native Go; 251 abort with rollback and 218 log the expected
panic line exactly once. No SBF test skipped: all 16 SBF/rebuild tests of
`internal/compiler` passed on both targets, and `make test` passed for every
root package.

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Example ELF SHA-256 | `c8e7dd33…` | `eae9703d…` |
| Highest completed vector CU | 8,451 | 8,486 |
| `u == v` on a two-word struct, per iteration | 24.6 | 24.6 |
| The same comparison spelled field by field | 25.6 | 25.6 |
| Update two fields of `[8]Pair` | 25.6 | 25.6 |
| Update the same values in two scalar arrays | 29.6 | 29.6 |
| `panic("overflow")` after an empty loop (completed run: 822) | 1,201 | 1,201 |
| `panic` with a 64-byte string after an empty loop | 2,611 | 2,611 |

Per-iteration CU is (CU of 128 iterations − CU of none) / 128 and includes the
loop and a `switch` (later cases pay one more test, which is why the
field-by-field form, the second case, costs one more CU). Struct `==` is
lowered as a chain of statements; an earlier `&&`-expression lowering cost
35.6 CU per iteration and was replaced. A panic costs the log syscall plus
copying and UTF-8-checking its line (about 21 CU per byte), on the failure path
only.

**Byte identity.** The full token swap rebuilds fresh to fogo-v0
`4697a9f5…` and solana-v3 `2b5f21d9…` (and `clock2` to `0f768370…` /
`54f34b65…`), matching the reference. The frozen check corpus emits identical C
for every accepted entry except the nine whose verdict changed
(`corpusChanges`), and every ELF recorded by the earlier October 14 results
(allocation, bits, compound, function values, function combinations,
interfaces, maps, strings) rebuilt to the same hash ([metrics](metrics.json)).

**Library survey.** On the unchanged pass-2 workspace the peel now needs 230
removals instead of 252; uint128 goes from 29 to 42 of 50 functions and the
Uniswap utilities from 13 to 14 of 40. No removal is caused by struct
comparison, aliases, panic or composite array elements any more. The remaining
root causes are package-level variables (sentinel errors, `binary.BigEndian`,
lookup tables, `MustFromHex` constants), the standard library, and one size
limit: uint256's `pows` table, `[60]Int`, is 1,920 bytes, over the 1,024-byte
per-value array limit (it is also a package-level variable).

## Deviations from Go

- A panic does not run pending deferred calls and cannot be recovered (the
  transaction fails and rolls back either way); there is no custom error code.
- Panic values of interface types (including `error`) and of types with
  `Error`/`String` methods log their static type, `(error)` or `(pkg.T)`,
  where Go prints the method's result; other non-printable values log
  `(pkg.T)` without Go's address. Lines are capped at 512 bytes and invalid
  UTF-8 bytes are logged as `?`.
- Arrays keep the 1,024-byte per-value limit.
