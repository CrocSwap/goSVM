# Standard-library packages on-chain

On-chain programs, and the user packages they import, can import these
standard-library packages, with Go 1.22.0's semantics:

| Package | On chain |
| --- | --- |
| `errors` | everything: `New`, `Is`, `As`, `Unwrap`, `Join`, `ErrUnsupported` |
| `math` | the integer limits (`MaxInt`, `MinInt`, `MaxInt8` … `MaxUint64`); no floating-point constant or function |
| `encoding/binary` | `ByteOrder`, `AppendByteOrder`, `LittleEndian`, `BigEndian`, `NativeEndian` (little-endian, as SBF), `Uvarint`, `Varint`, `PutUvarint`, `PutVarint`, `AppendUvarint`, `AppendVarint`, `MaxVarintLen16/32/64` |
| `unicode/utf8` | everything |
| `unicode` | the case mappings (`To`, `ToUpper`, `ToLower`, `ToTitle`, `CaseRanges`, `CaseRange`, `UpperCase`, `LowerCase`, `TitleCase`, `MaxCase`, `UpperLower`), `SimpleFold`, `IsSpace`, the `Range16`/`Range32`/`RangeTable` types, `MaxRune`, `ReplacementChar`, `MaxASCII`, `MaxLatin1`, `Version` |
| `strings` | everything but `Reader`/`NewReader`, `Replacer`/`NewReplacer`, `Title` and `To*Special`: `Builder` (all methods), `Clone`, `Compare`, `Contains`, `ContainsAny`, `ContainsFunc`, `ContainsRune`, `Count`, `Cut`, `CutPrefix`, `CutSuffix`, `EqualFold`, `Fields`, `FieldsFunc`, `HasPrefix`, `HasSuffix`, `Index`, `IndexAny`, `IndexByte`, `IndexFunc`, `IndexRune`, `Join`, `LastIndex`, `LastIndexAny`, `LastIndexByte`, `LastIndexFunc`, `Map`, `Repeat`, `Replace`, `ReplaceAll`, `Split`, `SplitAfter`, `SplitAfterN`, `SplitN`, `ToLower`, `ToTitle`, `ToUpper`, `ToValidUTF8`, `Trim`, `TrimFunc`, `TrimLeft`, `TrimLeftFunc`, `TrimPrefix`, `TrimRight`, `TrimRightFunc`, `TrimSpace`, `TrimSuffix` |
| `bytes` | the same functions for `[]byte`, with `Equal` and `Runes`; not `Buffer`, `Reader` (and their constructors, `MinRead`, `ErrTooLarge`), `Title` or `To*Special` |
| `strconv` | everything but floats and complex numbers: `Atoi`, `Itoa`, `ParseInt`, `ParseUint`, `ParseBool`, `FormatInt`, `FormatUint`, `FormatBool`, `AppendInt`, `AppendUint`, `AppendBool`, `NumError`, `ErrRange`, `ErrSyntax`, `IntSize` (64), `Quote`, `QuoteToASCII`, `QuoteToGraphic`, `QuoteRune`, `QuoteRuneToASCII`, `QuoteRuneToGraphic`, the `Append*` forms of each, `CanBackquote`, `Unquote`, `UnquoteChar`, `QuotedPrefix`, `IsPrint`, `IsGraphic` |
| `math/bits` | as before, a compiler intrinsic ([math/bits](GO_BITS.md)) |

```go
import (
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
)

var ErrBadAmount = errors.New("bad amount")

type AmountError struct{ Field string }

func (e *AmountError) Error() string { return "bad " + e.Field }

func amount(text string) (uint64, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if errors.Is(err, strconv.ErrRange) {
		return 0, &AmountError{"amount"}
	}
	if err != nil {
		return 0, ErrBadAmount
	}
	return v, nil
}

func Process(s, i []byte) uint64 {
	v, err := amount(string(i))
	var ae *AmountError
	switch {
	case errors.As(err, &ae):
		return 2
	case err != nil:
		return 1
	}
	binary.BigEndian.PutUint64(s, v)
	return 0
}
```

Imports work in a single file (no `go.mod` needed), in module programs and in
shared packages, plain or renamed. Native tests and clients compile the same
source against Go's own standard library.

Any other standard-library package (`fmt`, `io`, `math/big`, `sort`,
`slices`, `reflect`, `encoding/json`, …) is still rejected at the import:
`could not import fmt (unsupported import "fmt"; standard-library packages on
chain: bytes, encoding/binary, errors, math, strconv, strings, unicode,
unicode/utf8 and math/bits (docs/GO_STDLIB.md))`.

## Symbols that are not available

A Go 1.22 symbol of one of these packages that is not available on chain is
reported by name, at the use, with the reason, instead of `undefined`:

```text
p.go:3:55: math.Sqrt is not available on chain: floats are not supported on chain (docs/GO_STDLIB.md)
p.go:3:57: encoding/binary.Size is not available on chain: it uses reflection (docs/GO_STDLIB.md)
p.go:3:51: encoding/binary.ReadUvarint is not available on chain: it reads from an io.ByteReader; package io is not available on chain (docs/GO_STDLIB.md)
p.go:3:48: bytes.Buffer is not available on chain: it implements io interfaces; package io is not available on chain (docs/GO_STDLIB.md)
p.go:3:47: unicode.IsUpper is not available on chain: goSVM's on-chain unicode has only the case mappings, SimpleFold and IsSpace, not the property tables (docs/GO_STDLIB.md)
```

| Left out | Reason |
| --- | --- |
| every `math` float constant and function, `strconv.ParseFloat`, `FormatFloat`, `AppendFloat`, `ParseComplex`, `FormatComplex` | floats are not supported on chain (a project decision) |
| `encoding/binary.Read`, `Write`, `Size` | reflection |
| `encoding/binary.ReadUvarint`, `ReadVarint`; `bytes.Buffer`, `bytes.Reader`, `strings.Reader`, `strings.Replacer` and their constructors | `io` |
| `strings.Title`, `bytes.Title`; `unicode`'s property tables and predicates (`IsUpper`, `IsLetter`, `In`, `Is`, …) | Unicode tables beyond case mapping and white space; they would be read-only data, but are not written yet |
| `To{Upper,Lower,Title}Special`, `unicode.SpecialCase`, `TurkishCase`, `AzeriCase` | named slice types (`SpecialCase`) are not in the subset |

A name Go does not declare keeps go/types' error (`undefined: strings.Nope`).
A symbol that only exists as a method (none of the overlay's types leave out
methods) is not rewritten.

## How it works

Each package is an **overlay**: Go 1.22.0's own source files, embedded in the
compiler (`internal/compiler/_stdlib/`, with Go's `LICENSE`), curated so that
every function satisfies the on-chain subset. A program that imports one gets
it as an ordinary package of the program: Check type-checks it in the same
universe, monomorphizes, borrow-checks and lowers it, and the C backend emits
it like user code (unused functions are dropped by LLVM, so the ELF only
grows by what the program reaches). Positions inside the overlay are spelled
`gosvm-stdlib/go1.22.0/<package>/<file>.go`.

The overlay also contains two internal packages, which user code cannot
import (Go's `internal` rule): `internal/bytealg` (Go's portable versions:
SBF has no assembly, so `bytealg.MaxLen` is 0 and `strings`/`bytes` take the
same generic paths as Go on such platforms) and `internal/errtext` (goSVM's,
below).

Files are copied verbatim where possible. Every change is marked in the source
with a `goSVM:` comment, and each file starts with a summary. The changes:

| File | Change |
| --- | --- |
| `errors/wrap.go` | `Is` asks the compiler whether target's dynamic type is comparable (intrinsic) instead of `internal/reflectlite`; `is` and `As` walk an `Unwrap() []error` tree with an explicit stack instead of recursion, in the same depth-first order; `As` is generic (below) |
| `errors/join.go`, `strconv/atoi.go` | `(*joinError).Error` and `(*NumError).Error` build their text with `internal/errtext` instead of calling the wrapped errors' `Error` (recursion through the error interface); `cloneString` copies once |
| `strings/builder.go`, `strings/clone.go`, `errors/join.go` | `unsafe.String` and the runtime's `noescape` are compiler intrinsics |
| `strings/strings.go`, `bytes/bytes.go` | `EqualFold` continues in a helper instead of a `goto`; `FieldsFunc`'s local `span` type is at package level; `bytes.Equal` compares in a loop instead of `string(a) == string(b)` (which gc does without copying); the `*Special` and `Title` functions are left out |
| `internal/bytealg/bytealg.go`, `equal_generic.go` | the same comparison loop for the Rabin-Karp candidates and `Equal`; `MakeNoZero` is `make` (same length and capacity) |
| `internal/bytealg/compare_generic.go` | a `goto` becomes a skipped loop |
| `unicode/utf8/utf8.go`, `strconv/quote.go` | `fallthrough` becomes a repeated case body |
| `strconv/quote.go`, `unicode/letter.go`, `unicode/graphic.go` | lookups index the read-only tables directly instead of passing them to `bsearch16`/`is16`/`to` (passing a table would make it writable, so it would be built on every invocation) |
| `unicode/tables.go` | the case, fold and white-space tables, extracted verbatim from Go's `tables.go` by `TestStdlibUnicodeTables` |
| `encoding/binary/native_endian_little.go` | `nativeEndian` writes out `littleEndian`'s promoted methods (struct embedding is not in the subset) |
| `encoding/binary/binary.go`, `varint.go`, `math/const.go` | extracts: the reflection, `io` and float parts are left out |

Go's own `panic` calls are kept ([panic](ERROR_CONTRACT.md#panic)): where Go
panics, the transaction fails and logs `panic: <Go's message>` (for example
`panic: strings: negative Repeat count`); runtime errors (an index out of
range in `PutUvarint`'s buffer, a nil pointer) are traps without a line, as
everywhere.

### Provenance

`_stdlib/PROVENANCE.json` records, for every overlay file, its own SHA-256,
how it relates to Go (`verbatim`, `verbatim apart from the build constraint`,
`modified`, `extract`, goSVM helpers) and the SHA-256 of each Go 1.22.0 file it
comes from. `TestStdlibProvenance` regenerates it from the host toolchain
(which must be Go 1.22.0) and checks it, that verbatim files differ only by
their goSVM header, and that `_stdlib/LICENSE` is Go's. `_stdlib/api.txt` is
Go 1.22.0's exported API of these packages (from `$GOROOT/api`, checked by
`TestStdlibAPI`); it drives the "not available on chain" diagnostics.
`GOSVM_UPDATE_STDLIB=1 go test -run 'TestStdlib(API|Provenance|UnicodeTables)'
./internal/compiler` regenerates all three.

**Identity.** `compiler.StdlibIdentity(paths)` hashes the overlay files of the
packages a program reaches (`compiler.ProgramStdlib`) with the Go version.
Build receipts and dependency keys include it (`stdlib:go1.22.0`, the package
list and the hash) for programs that import an overlay package; other
programs keep their keys.

## errors

`Is`, `Unwrap` and `Join` have Go's signatures and results, including
`Unwrap() []error` trees, `Is(error) bool` methods, uncomparable targets
(`errors.Is(err, listErr{...})` skips `==`, as Go does) and `ErrUnsupported`.

**`As` is generic.** Go declares `As(err error, target any) bool` and uses
reflection to find `*target`'s type. goSVM has no run-time type information,
so the overlay declares `As[T any](err error, target *T) bool`, and the type
of `*target` is the type argument. The usual call, `errors.As(err, &target)`,
compiles unchanged and behaves as in Go, for interface, pointer and value
targets, including `As(any) bool` methods and the panics for a nil pointer
(`errors: target must be a non-nil pointer`) or a type that is neither an
interface nor an error (`errors: *target must be interface or implement
error`). A target whose static type is not a pointer cannot be passed:

```text
p.go:3:50: errors.As on chain needs a target of static pointer type, as in errors.As(err, &target): goSVM has no reflection to find the type of an any target (docs/GO_STDLIB.md#errors); cannot infer T (gosvm-stdlib/go1.22.0/errors/wrap.go:117:9)
```

`errors.As` cannot be used as a function value either (generic functions need
instantiation, as in Go). Pointer targets (`var p *MyErr; errors.As(err, &p)`)
are `**MyErr` values: pointers to pointers are supported for this
([pointers](GO_POINTERS.md#pointers-to-pointers)), and `errors.Join` needed
[variadic functions](GO_FUNCTIONS.md#variadic-functions).

**Recursion.** The subset rejects recursion, including through interfaces
([recursion](GO_FUNCTIONS.md#recursion)): a call of `err.Error()` inside an
`Error` method is a call to every `Error` method. Go's `(*joinError).Error`
and `(*NumError).Error` are exactly that, so the overlay builds their text in
`internal/errtext` without recursion: each describes its text as literal parts
and wrapped errors (`ErrorParts`, an extra method that cannot be called or
implemented outside the standard library: its parameter type is internal), and
`errtext.Error` expands nested ones with an explicit stack, calling the
other errors' `Error` methods in Go's order through a dispatch that excludes
the overlay's own wrappers (below). The text is Go's. User error types are
under the same rule: an `Error` method that calls another error's `Error`
is rejected as recursion, as is an `Is` method that calls `errors.Is`.

## Intrinsics

Four overlay functions have Go bodies that type-check and pass the subset rules
but that the C backend replaces (`CheckedProgram.Functions[i].Intrinsic`);
an alternative backend must implement them
([backend interface](BACKEND_INTERFACE.md#standard-library-packages)):

| Intrinsic | Function | Meaning |
| --- | --- | --- |
| `comparable` | `errors.comparableType(x any) bool` | whether x's dynamic type is comparable: false for the program's uncomparable dynamic types (`DynamicType.Comparable`) |
| `unsafeString` | `unsafeString(b []byte) string` in `strings`, `strconv`, `internal/errtext` | a string sharing b's bytes (`unsafe.String`), so `Builder.String` and friends do not copy, as in Go |
| `noescape` | `strings.noescape(p *Builder) *Builder` | p itself; its Go body returns nil so that the borrow analysis does not move every `strings.Builder` to the heap for its self-pointer, which is only compared |

And one directive: dynamic calls in a function marked `//gosvm:dispatch-except
internal/errtext.Node` use a method slot that excludes the implementers of
`errtext.Node` (`MethodSlot.ExceptImplementers` and `Except`): their
dispatcher aborts for those types, which the overlay never passes, and the
call graph stays acyclic. Directives are honoured only in overlay files.

## Aggregate parameters on SBF

Clang passes C struct parameters (strings, slices, interfaces, structs,
arrays) `byval`: the IR promises the callee its own copy, so a caller may pass
its own variable's address, and the SBF backend lowers `byval` as that
pointer without copying. A function that writes such a parameter, and keeps
it in memory (typically to pass it on to another call, as `strings.Count`
does with `s = s[i+len(substr):]` and `Index(s, substr)`), then changes its
caller's variable. Host-compiled C is correct (small structs travel in
registers), so only SBF executions differ; this was found by
`TestStdlibSBF`, where `strings.Replace` aborted on chain but not on the host.

**Resolution (October 15, user decision: fix it for all code).** Every
function, method and literal, in the overlay and in program code, receives
such a parameter under another name and copies it into a local at entry
(`internal/compiler/byval.go`), so its body never touches the caller's
memory:

- a parameter the body writes (assigned, also through a field or array
  element; incremented; ranged into; addressed, also by a pointer-receiver
  method call; an array parameter sliced) is copied plainly;
- a parameter the body may read after a write that can change its caller's
  variable is copied through an opaque pointer, so the copy is taken at
  entry. This is the second form of the same bug, found while auditing:
  LLVM treats byval memory as private, so after `gp.a = 1` (or a write
  through a pointer, or a call that writes) it still reads the parameter, or
  passes its address on, from the caller's variable `gp`. A write counts
  unless it provably reaches only the call's own locals (a pointer-receiver
  method called on a local) or only values of types the variable cannot
  hold (a byte written through a `[]byte` never changes a string, slice or
  struct without byte arrays: those headers are not their elements);
  calls are summarized by what their callees write and through which
  parameters;
- copies larger than 128 bytes stay whole in memory (otherwise LLVM rebuilds
  the struct from scalars at every return of it, which grew the full swap's
  ELF by 19–29%).

Functions with neither kind of parameter keep their C. append's growth helper
(`gosvm_grow`) wrote its slice parameter's length in place, so `t :=
append(s, x)` with spare capacity could lengthen a package-level or heap `s`
on SBF; it now returns a new header. The other C runtime helpers never write
their aggregate parameters, and library calls pass only pointers and lengths.
Paths audited on SBF ([evidence](../results/compiler/2026-10-15-byval-rebuild/README.md)):
package variables, heap fields, address-taken locals, value receivers,
interface boxes (a value-receiver method called through an interface wrote
the box), method and function values, deferred calls, closures, slice, array
and map elements, a parameter passed on to a writer, results replacing the
argument's variable (sret: safe, LLVM keeps the result separate), and append.
The cost is what the copies execute: +599/+603 CU for the full swap's wide
swap (its `Swap` writes its 700-byte accounts parameter), +717–807 CU for pool
creation, 0 on the paths that return before the copies; the ELFs shrink by
152–224 bytes.

## Cost

Package initialization runs on every invocation
([package-level variables](GO_GLOBALS.md#cost)): `errors.ErrUnsupported`,
`strconv.ErrRange` and `strconv.ErrSyntax` are boxed errors built on each
invocation; the tables (`unicode`, `utf8`, `strconv`'s `isPrint`) are
read-only data at zero cost. Measured on the pinned runners (identical on
fogo-v0 and solana-v3 unless two numbers are given; a probe's cost minus the
same ELF with the call skipped; [results](../results/compiler/2026-10-14-stdlib/README.md)):

| Call | CU |
| --- | --- |
| Importing `errors` or `strconv` (initialization, per invocation) | 70–84 |
| Importing `strings`, `bytes`, `unicode`, `unicode/utf8`, `encoding/binary` | 1–19 |
| `binary.BigEndian.Uint64`, `LittleEndian.PutUint64` | 2–3 |
| `binary.Uvarint` (3 bytes) | 25 |
| `errors.Is` (match at depth 1 / no match through 2 wraps) | 22 / 89 |
| `errors.As` (through 2 wraps, pointer target) | 56 |
| `strconv.Itoa` (2 digits) | 7 |
| `strconv.ParseUint` (8 / 20 digits, from `[]byte` via `string(b)`) | 123 / 480 |
| `strconv.FormatUint`, `AppendUint` (20 digits) | 423 / 414–430 |
| `strconv.Atoi` failing, then `err.Error()` (`NumError` text with `Quote`) | 2,257–2,843 |
| `strconv.Quote` (12 bytes) | 861–993 |
| `strings.Index` (32 bytes, 3-byte needle) / `bytes.Index` | 511 / 260 |
| `strings.Split` (4 fields) | 332 |
| `strings.ToUpper` (12 ASCII bytes) | 811 |
| `strings.EqualFold` (non-ASCII) | 544 |
| `strings.Builder` (4 writes and `String`) | 388 |
| `utf8.DecodeRuneInString`, `RuneCount` (64 bytes) | 56, 742 |
| `unicode.ToUpper` (non-ASCII) | 116 |

The byte-searching functions are Go's portable versions (a byte loop and
Rabin-Karp), so they cost CU proportional to the text: there is no
vectorized `IndexByte` on SBF.

## Heap

The overlay allocates as its Go code does under goSVM's
[allocation rules](GO_ALLOCATION.md): slices and strings grow as Go 1.22's
`append`, a `strings.Builder` grows as Go's (`2*cap + n`) and `String` shares
its bytes. Where gc allocates on the stack instead (non-escaping temporaries)
goSVM uses the heap, and the explicit stacks of `errors.Is`/`As` and
`errtext.Error` allocate where Go's recursion used the stack, so heap use can
be higher than gc's; results never differ.

## Deviations from Go

- `errors.As` takes `*T`, not `any` (above).
- `strings.Builder` and the error texts are exact; the text of a
  `*NumError` or `Join` error is built without recursion, and the heap it
  uses differs.
- Recursive user error methods are rejected (above).
- `unicode.CaseRanges` is a read-only table unless a program writes it or
  passes it as a value; then it is built on every invocation (Go's
  semantics, at a cost).
- `strconv.IntSize` is 64 and `binary.NativeEndian` is little-endian, as on
  SBF.
- Panics log goSVM's line ([panic](ERROR_CONTRACT.md#panic)), not Go's stack
  trace.

## Evidence

- [`examples/stdlib-values`](../examples/stdlib-values/program.go) calls every
  supported function of every package (and the variadic and
  pointer-to-pointer forms) on inputs from the vector, and writes every
  result, slice length and capacity, error text and `errors.Is`/`As` answer.
  `TestStdlibDifferential` runs 30,906 vectors (4,048 Go panics, each an abort
  at the same point with the same log line) natively against Go 1.22.0's
  standard library and as host-compiled C with the default 32 KiB heap.
- `TestStdlibSBF` runs 306 of them on fogo-v0 and solana-v3 through the pinned
  runners (aborts roll back; panics log their line) and measures the probes
  above.
- `TestStdlibRejections`, `TestStdlibImportForms`, `TestStdlibIntrinsics`,
  `TestStdlibIdentity`, `TestStdlibConstantsOnly` (importing `math` for
  constants emits the same C as writing them out), `TestStdlibPackagesList`,
  `TestStdlibAPI`, `TestStdlibProvenance`, `TestStdlibUnicodeTables`
  ([stdlib_test.go](../internal/compiler/stdlib_test.go)), and
  `TestStdlibUses` for `gosvm features`.
- Programs that import none of these packages emit byte-identical C (the
  frozen corpus) and ELFs (full swap fogo-v0 `4697a9f5…`, solana-v3
  `2b5f21d9…`). Since the [aggregate-parameter fix](#aggregate-parameters-on-sbf)
  the full-swap reference is fogo-v0 `a66c5a97…`, solana-v3 `b419245b…`
  ([results](../results/compiler/2026-10-15-byval-rebuild/README.md)).

The local runners are not a public network; see
[target profiles](TARGET_PROFILES.md).
