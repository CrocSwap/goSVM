# Experimental fixed-size arrays

The first milestone 2 compiler increment supports fixed-size arrays of `byte`,
`uint32`, `uint64`, `bool`, and named versions of those scalar types. Named array
types such as `type PublicKey [32]byte` are supported. This is compiler support;
the schema-1 project generator still accepts only flat unsigned wire fields.
[Shared-package imports](GO_PACKAGES.md) and [schema-2 scalar-array codecs](SCHEMA2.md)
are now implemented; richer array elements remain open.

```go
type PublicKey [32]byte
type Pool struct {
    Authority PublicKey
    Counter   uint64
}

func ReplaceFirst(key PublicKey, value byte) PublicKey {
    copy := key
    copy[0] = value
    return copy
}
```

Arrays preserve Go value semantics: assignment, struct fields, function arguments
and returned values copy their elements. Indexed writes mutate the selected local
array or array field. Literal values evaluate in source order, including keyed
literals; omitted elements are zero. Positional, keyed and inferred-length `[...]`
literals are supported. Array equality/inequality compare elements, and `len` and
`cap` retain Go's operand-evaluation rules. Conversion between arrays with identical
underlying types is supported, including named/unnamed array conversions.

Indexes are checked using their full unsigned width. An out-of-range access
aborts; there is no on-chain recovery. The SVM corpus checks transaction rollback
for failed accesses, including a successful first instruction followed by a
bounds failure. Zero-length arrays have no accessible elements.

This increment limits each array value to **1,024 bytes**. Arrays of structs,
nested arrays, signed/pointer elements, slice-to-array conversions and `range`
remain unsupported. [Constrained array pointers](GO_POINTERS.md), indexed
increment/decrement and [borrowed byte views](GO_VIEWS.md) are now supported. Diagnostics identify Go source
locations. The per-value limit does not guarantee that all copies, locals and
calls in a function fit its SBF stack frame; LLVM/VM stack limits still apply.
Wire bytes must use explicit codecs rather than native Go/C memory layout.

## Evidence and reproduction

The [array fixture](../examples/array-values/program.go) exercises keys, named
elements, boolean and wide arrays, literals, copies, function calls, struct fields,
integer wrapping, evaluation order and bounds failures. It is ordinary Go and
can be called by the native driver without SBF tools or runtime emulation.

- [Differential tests](../internal/compiler/arrays_test.go): 1,000 deterministic
  randomized native-Go/generated-C comparisons plus 15 edge/failure vectors,
  56 additional 1 KiB copy/zeroing/bounds comparisons across all scalar kinds,
  deterministic multi-file output, explicit unsupported cases and size boundaries.
- [Compiled-SBF evidence](../results/compiler/2026-10-05-arrays-supported/README.md):
  147 scenarios, three repetitions, native-Go state expectations, exact failures
  and atomic rollback. Clang reports static frames of 320 bytes for `Process` and
  64 bytes for the entry adapter in the exact tested ELF.
- [1 KiB capacity evidence](../results/compiler/2026-10-05-array-capacity-final/README.md):
  60 scenarios across byte/uint32/uint64/bool arrays, three repetitions each.
  Zeroing, copies, mutation, integer wrapping and bounds rollback agree with native
  Go. Each variant uses a 2,112-byte handler frame and 64-byte entry adapter.
- [Existing-workload regression](../results/svm/2026-10-05-m2-arrays-regression-final/README.md):
  all starter/token/lifecycle/sysvar cases and prior ELF hashes are retained.

```sh
GOCACHE="$PWD/build/lifecycle-go-cache" make test
GOCACHE="$PWD/build/lifecycle-go-cache" python3 scripts/array_verify.py \
  --output results/compiler/new-array-experiment
python3 scripts/array_capacity_verify.py \
  --output results/compiler/new-capacity-experiment
```

The SBF experiment needs installed v1.51 LLVM and the pinned local runner package.
Use `--llvm /absolute/path/to/v1.51/llvm` if needed. Each output directory must be
new. Saved native-driver source is text; executable build inputs remain in ignored
staging, so results do not become root Go packages. No fresh validator is used.
