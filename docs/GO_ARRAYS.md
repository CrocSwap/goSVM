# Experimental fixed-size arrays

On-chain Go code can use fixed-size arrays of any storable element type:
[integers](GO_INTEGERS.md) of every width, `bool`, [strings](GO_STRINGS.md),
[interface values](GO_INTERFACES.md), [maps](GO_MAPS.md),
[function values](GO_FUNCTIONS.md), and (since the October 14
[value-types](GO_TYPES.md) increment) **composite elements**: named value
structs, arrays (`[3][4]uint8`, `[N]uint256.Int` where `Int` is `[4]uint64`),
pointers (`[2]*Node`, including pointers to the array's own element type),
slices and `struct{}`. Named array types such as `type PublicKey [32]byte`
are supported. This is compiler support for in-memory Go values: the
schema-1 project generator still accepts only flat unsigned wire fields, and
[schema-2 wire layouts](SCHEMA2.md) still accept only bounded scalar arrays
(arrays of structs and nested arrays in account layouts are rejected there).

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

type Int [4]uint64 // holiman/uint256 style

func digits(x Int) int {
    pows := [...]Int{{1}, {10}, {100}, {1000}}
    for j := len(pows) - 1; j >= 0; j-- {
        if x[0] >= pows[j][0] {
            return j + 1
        }
    }
    return 0
}
```

Arrays preserve Go value semantics: assignment, struct fields, function arguments
and returned values copy their elements (an array of structs or arrays is copied
deeply; an array of pointers, slices or maps copies the references). Indexed
writes mutate the selected local array or array field. Literal values evaluate in
source order, including keyed literals; omitted elements are zero. Positional,
keyed and inferred-length `[...]` literals are supported, with elided element
literals (`[2]Pair{{K: 1}, {}}`, `[2][2]P{{{1, 2}}}`) and elided `&T{...}`
elements of pointer arrays (allocated on the heap). Array equality/inequality
compare elements in index order and stop at the first difference, as Go does;
elements that are structs compare field by field ([struct equality](GO_TYPES.md#struct-equality)),
and arrays of slices, maps or function values are not comparable (go/types
rejects `==` on them). `len` and `cap` retain Go's operand-evaluation rules.
Conversion between arrays with identical underlying types is supported,
including named/unnamed array conversions.

<a id="pointer-literals"></a>**Pointer literals.** `&T{...}` with an array
`T` (named or not) is an addressable composite literal, like a struct
literal: it moves to the heap only when it escapes. In a slice, array or map
literal whose element (or key) type is `*T` with an array `T`, an element may
be written `{...}` for `&T{...}`, as Go allows: holiman/uint256 and
KyberNetwork/int256 declare `var multipliers = [5]*Int{nil, {10000000000000000000, 0, 0, 0}, ...}`
with `type Int [4]uint64`. Check rewrites each such elided element into the
explicit `&T{...}` (`internal/compiler/elided.go`), so evaluation order,
escape analysis and the backend view are those of the explicit form; elided
struct elements keep their own lowering (always on the heap), and programs
without elided array elements are unchanged. Elided elements of other
pointer types (`[]*map[K]V{{...}}`) keep the rejection `elided &T{...}
literals require a struct type; write &T{...}`.

Composite elements are addressable storage: `a[i].F = v`, `a[i].F++`,
`a[i].F += v`, `a[i][j] = v`, `&a[i]`, `&a[i].F`, pointer-receiver method
calls `a[i].M()`, slicing `a[i][:]` and `a[lo:hi]`, and the same through a
pointer to an array (`p[i].F`, with the nil check). Reading `a[i].F` or
`a[i][j]` reads in place, without copying the element. `range` over an array
of composite elements copies the array when a value variable is used, as Go
does; an index-only `range` over an element of an array of arrays
(`for j := range g[i]`) evaluates nothing when the operand has no calls
(Go's constant-length rule, so an out-of-range `i` does not trap). Arrays of
composite elements work in locals, fields, parameters, results, slices
(`[][2]Pair`), map keys and values (`map[[2]Pair]uint8`), interface values and
generic code (`[4]T` instantiated with a struct). Storing a pointer to a local
into an array element or array literal moves the local to the heap, as Go's
escape analysis would ([allocation](GO_ALLOCATION.md)).

Indexes are checked using their full unsigned width; a negative `int` index is
out of range. An out-of-range access
aborts; there is no on-chain recovery. The SVM corpus checks transaction rollback
for failed accesses, including a successful first instruction followed by a
bounds failure. Zero-length arrays have no accessible elements.

Each array value is limited to **1,024 bytes** (Go size; for example
`[32]uint256.Int` or `[3][4]uint64` fits, `[33]uint256.Int` is rejected with
`array storage exceeds experimental 1024-byte per-value limit`). The limit
applies to every array type, including nested element arrays; arrays of
`struct{}` have size zero. SBF frames are 4 KiB, and the limit does not
guarantee that all copies, locals and calls in a function fit its frame:
LLVM/VM stack limits still apply, so keep large tables in few locals (or on
the heap through a pointer or slice) and pass them by pointer. One exception:
a package-level table whose values are never copied (only indexed, passed
to `len`/`cap` or ranged over by index) may exceed the limit, since it is
never in a stack frame ([large tables](GO_GLOBALS.md#large-tables)).
Arrays of `solana.Context` are rejected (`arrays of Context unsupported`).

<a id="slice-to-array-conversions"></a>**Slice-to-array conversions.**
`(*[N]T)(s)` (Go 1.17) and `[N]T(s)` (Go 1.20) follow Go:

```go
func firstWord(data []byte) uint64 {
    k := (*[8]byte)(data) // aliases data[0:8]
    return binary.LittleEndian.Uint64(k[:])
}

key := [32]byte(acct[8:40]) // a copy
```

- `len(s) < N` aborts with the bounds trap
  ([error contract](ERROR_CONTRACT.md#traps)); Go panics.
- `(*[N]T)(s)` is the address of `s`'s first element: writes through the
  pointer and through `s` (or any slice sharing its backing array, including
  account data) see each other. A nil slice gives a nil pointer (only
  `N == 0` gets there); a non-nil empty slice gives a non-nil pointer.
- `[N]T(s)` copies the first `N` elements; `[0]T(s)` reads nothing, so a nil
  `s` is fine.
- Both are plain operations in gc's [evaluation order](ERROR_CONTRACT.md#evaluation-order):
  `s` (and, for `[N]T(s)`, its elements) is read, and the length checked,
  after the calls in the same statement, so in `(*[8]byte)(b)[0] + f()` a
  panic in `f` comes first and a new `b` assigned by `f` is the one converted.
- `len((*[N]T)(s))` is a constant and evaluates nothing, as in Go. An
  index-only `range` over a conversion keeps the rejection `index-only range
  over an array conversion unsupported; range over the operand`.
- The borrow analysis treats the pointer as borrowing what `s` borrows, so a
  pointer into a local array that escapes moves the array to the heap.
  Package-level initializers that convert a slice run at run time rather
  than at [compile time](GO_GLOBALS.md#compile-time-evaluation).

Element types are any storable slice element: scalars, structs, arrays,
pointers and type parameters (`func First[T any](s []T) *[4]T`).
Tests: `TestSliceArray*` ([slicearray_test.go](../internal/compiler/slicearray_test.go),
[evidence](../results/compiler/2026-10-19-slice-array-conversions/README.md)).

[`range`](GO_CONTROL.md#range-loops) iterates over arrays (over a copy
when a value variable is used) and pointers to arrays. [Constrained array pointers](GO_POINTERS.md), indexed
increment/decrement and [borrowed byte views](GO_VIEWS.md) are now supported. Diagnostics identify Go source
locations. Wire bytes must use explicit codecs rather than native Go/C memory layout.

The C backend keeps arrays of scalar, string, interface, map and
function-value elements exactly as before (their programs' C is unchanged);
arrays of composite elements are tagged C structs defined after their element
type, with an element-wise equality function when comparable.
`gosvm features` reports `uses.composite_arrays` for array types written with
struct, array, pointer or slice elements.

## Evidence and reproduction

The [value-types fixture](../examples/value-types/program.go) exercises arrays
of structs, arrays, pointers, slices, maps and strings (zero values, literals,
updates, copies, methods, ranges, slicing, map keys, equality and bounds
failures) natively and through goSVM, on the host and on SBF for both targets
([evidence](../results/compiler/2026-10-14-value-types/README.md)).

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
