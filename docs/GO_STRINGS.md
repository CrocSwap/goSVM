# Strings and `error` on-chain

On-chain Go code can use `string` with Go's semantics: literals, `len`,
indexing, slicing, concatenation and `+=`, bytewise comparisons, conversions
to and from `[]byte`, `[]rune` and runes, `range` with Go's UTF-8 decoding,
`switch` on strings, and strings in every place a value can be stored. With
strings in the subset, the predeclared `error` interface is usable too: user
types implement `Error() string`, and errors are returned, compared with `nil`,
asserted and switched on like any interface value.

```go
type ParseError struct{ At int }

func (e *ParseError) Error() string { return "bad byte at " + string(rune('0'+e.At%10)) }

func command(b []byte) (string, error) {
	s := string(b) // copies b to the heap
	for i, r := range s {
		if r == utf8RuneError {
			return "", &ParseError{At: i}
		}
	}
	switch s {
	case "deposit", "withdraw":
		return s, nil
	}
	return "", &ParseError{At: 0}
}

const utf8RuneError = '�'

func Process(s, i []byte) uint64 {
	name, err := command(i)
	if err != nil {
		return 1
	}
	copy(s, "ok: "+name)
	return 0
}
```

Programs that use no string value compile to byte-identical C and ELFs: the
string runtime is emitted only when a value has a string type, and the heap
runtime only when a string operation allocates. String constants used only in
constant expressions (`const k = "abc"; len(k)`) are folded and add nothing.

## Supported

| Form | Semantics |
| --- | --- |
| Types | `string`, named string types (`type Name string`, with methods), untyped and typed string constants |
| Storage | Locals, parameters, results (including multiple results), struct fields, array elements (`[4]string`, with `==` comparing elements in order), slice elements (`[]string`, growing as Go 1.22 does for a 16-byte pointer-holding element), pointers (`*string`, `new(string)`, `&r.Name`), interface values, type arguments |
| Literals | Interpreted and raw literals with every Go escape (`\n`, `\x00`, `é`, `\U0001F600`, octal), and constant expressions (`"ab" + "c"`, `k[0:2]` of a constant is not constant, as in Go) |
| `len(s)` | The byte length; constant for a constant `s`, as in Go |
| `s[i]` | The byte at `i`, for an index of any integer type; out of range aborts (Go panics). Not addressable or assignable, as in Go |
| `s[lo:hi]` | Shares `s`'s bytes (no allocation); bounds as in Go (`0 <= lo <= hi <= len(s)`, defaults `0` and `len(s)`), out of range aborts. Three-index slices of strings are a Go error |
| `a + b`, `s += t` | Concatenation; see [allocation](#allocation). `+=` works on any assignable target: variables, fields, elements, `*p` |
| `==` `!=` `<` `<=` `>` `>=` | Bytewise, as Go compares strings (a proper prefix is less) |
| `switch s` | Cases compared with `==` in source order; case values may be non-constant |
| `string(b)` | `b` any `[]byte`-like slice (element type with underlying `byte`): a copy of its bytes |
| `[]byte(s)` | A copy, capacity as Go's runtime (below); `[]B(s)` for a named byte element `B` too |
| `string(x)` | `x` of any integer type: the UTF-8 encoding of rune `x`; negative values, surrogates (U+D800–U+DFFF) and values above U+10FFFF (including values that only truncate into range) become `"�"` |
| `[]rune(s)`, `string(rs)` | Decoding and encoding as Go does; invalid runes in `rs` encode as U+FFFD |
| `string(s)`, `Name(s)` | Conversions between string types copy the two words |
| `for i, r := range s` | Go's UTF-8 decoding: `i` is the byte index of each rune start, `r` the rune; an invalid, overlong, surrogate or truncated encoding yields U+FFFD with width 1. The string is evaluated once; `for i := range s` steps by runes too; key and value may be assigned (`for i, r = range s`) or converted to interfaces |
| `append(b, s...)`, `copy(b, s)` | Append or copy a string's bytes into a byte slice, as Go allows |
| Interfaces | A string boxes like any value (a 16-byte copy of the header); `==` between interfaces compares dynamic types, then bytes; `x.(string)`, `case string:`; `string` and a named string type are different dynamic types |
| Generics | `string` and named strings as type arguments; constraints with `~string` (`interface{ ~string }`, `interface{ ~string \| ~uint64 }`), `comparable`; every string operation inside generic code |
| `error` | The predeclared interface: values, results, `nil`, comparisons, assertions (`err.(*MyErr)`, `a.(error)`), type switches, `Error()` calls through the interface; any type with an `Error() string` method implements it |

## Representation

A string is two 64-bit words, a pointer to its bytes and its length (C:
`typedef struct { u8 *ptr; i64 len; } gosvm_string;`). The bytes are
immutable and live in:

- read-only data, for literals and constant expressions (C string literals in
  `.rodata`; `""` has a null pointer and length 0);
- the invocation heap, for concatenations and conversions;
- the same storage as the string they were sliced from.

String bytes are never on the stack and never SDK storage (`string(b)` of
account data copies it), so strings need no borrow tracking: a string can be
stored, returned or converted to an interface without moving anything to the
heap. Only `[]byte(s)` and `[]rune(s)` produce references, always to new heap
storage.

## Allocation

Every allocation follows the [allocator contract](GO_ALLOCATION.md#allocator)
(8-byte rounding, abort when the budget is exhausted). The heap bytes are part
of the [contracted runtime behaviour](ERROR_CONTRACT.md#contracted-runtime-behaviour):

| Operation | Heap bytes |
| --- | --- |
| Literals, constant expressions, `len`, `s[i]`, `s[lo:hi]`, comparisons, `switch`, `range`, string-to-string conversions, assertions | 0 |
| Concatenation `x1 + x2 + ... + xn` | 0 when the total length is 0 (the result is `""`) or exactly one operand is non-empty (the result is that operand); otherwise the total length rounded up to 8 |
| `string(b)` (`[]byte`) | 0 for an empty slice; otherwise `len(b)` rounded up to 8 |
| `string(x)` (integer) | 8 (the encoding is 1 to 4 bytes) |
| `string(rs)` (`[]rune`) | 0 when the encoding is empty; otherwise its length (1 to 4 bytes per rune, 3 for an invalid rune) rounded up to 8 |
| `[]byte(s)`, `s` not constant | 0 for `""` (the result is non-nil, capacity 0); otherwise Go 1.22's `rawbyteslice`: capacity `roundupsize(len(s))` (Go's malloc size classes, [append growth](GO_ALLOCATION.md#append-growth)) and that many bytes |
| `[]byte(c)`, `c` constant | 0 for `""`; otherwise capacity `len(c)` (Go copies a constant into a `[len(c)]byte`) and `len(c)` rounded up to 8 bytes |
| `[]rune(s)` | 0 when `s` has no runes (non-nil, capacity 0); otherwise Go 1.22's `rawruneslice`: `roundupsize(4n)` bytes for `n` runes, capacity `roundupsize(4n)/4` |
| Converting a string to an interface | 16 (the header; the bytes are shared) |
| `append(b, s...)`, `[]string` growth | As for any slice ([append growth](GO_ALLOCATION.md#append-growth)); `copy(b, s)` allocates nothing |

A *concatenation* is a maximal tree of non-constant string `+` operators, as
Go's compiler flattens them: `a + b + (c + d)` is one concatenation of four
operands, evaluated left to right, and `s += t + u` concatenates `s`, `t` and
`u` once. Operands are evaluated in order before anything is allocated; for
`x += y` the target's index and pointer operands are evaluated first, then
`y`, then `x` is read (as gc orders `x op= f()`).

Examples with the default 32 KiB heap (32,760 usable bytes): 4,095
`string(rune)` conversions fit and the 4,096th aborts; 2,047 strings boxed in
interfaces fit; after `a` (5 bytes) and `b` (3 bytes) are allocated (16 bytes),
4,093 concatenations `a + b` (8 bytes each) fit; 1,364 `[]rune` conversions of
a 5-rune string (24 bytes each) fit.

**Where gc differs.** Native Go has no fixed heap, and gc avoids some
allocations goSVM performs: it builds small non-escaping conversion results in
a 32-byte stack buffer, converts `string(b)` without copying for a comparison
or a concatenation operand, and ranges over `[]byte(s)` without
copying. Only one of these is observable in Go: a small `[]byte(s)` or
`[]rune(s)` that does not escape has capacity 32 in gc, while goSVM always
gives the heap capacity above (gc's capacity when the result escapes, for
example when it is returned or stored). Programs that do not depend on the
capacity of a non-escaping conversion behave identically; the differential
tests observe capacities only through escaping conversions.

## `error`

`error` is the predeclared interface `interface{ Error() string }` and follows
[interface semantics](GO_INTERFACES.md) exactly: a nil `error` is the nil
interface; a typed nil pointer in an `error` is non-nil; `err == nil`,
`err1 == err2` (dynamic types, then values; pointers by address), `err.(T)`,
`err.(type)` and `a.(error)` work as for any interface; calling `Error()` on a
nil `error` or through a nil pointer receiver that dereferences aborts, as Go
panics. Returning an error from `Process` is not possible (`Process` returns
the `uint64` custom error code); map errors to codes in the handler.

`errors`, `strconv`, `strings`, `bytes`, `unicode/utf8` and parts of
`unicode` are available on chain ([standard library](GO_STDLIB.md)); `fmt` is
not, and importing it is rejected with `could not import fmt (unsupported
import "fmt"; standard-library packages on chain: ...)`. Write error types by
hand (a struct or a named integer with an `Error() string` method) or use
`errors.New`. Logging strings through `sol_log` is
a separate SDK item: the pinned SDK is unchanged and has no string API.

## With function values

Strings work in every [function-value](GO_FUNCTIONS.md) form: parameters and
results of function types (including `error`), captured variables (a closure
captures the variable, so a later `s += t` through one closure is seen by the
others; the capture moves the 16-byte header to the heap, not the bytes),
deferred calls with string arguments (evaluated at the `defer` statement),
named `string` and `error` results changed by deferred closures, method values
of types with string methods, and function values in interfaces next to
`error` values. See [with strings and `error`](GO_FUNCTIONS.md#with-strings-and-error)
for the combined heap costs and evidence.

## Traps

Strings add no new trap. An out-of-range index or slice bound uses the existing
bounds abort; an allocation beyond the budget uses the out-of-memory abort; a
failed `x.(string)` and a nil-interface `Error()` call use the interface aborts
([failure contract](ERROR_CONTRACT.md)).

## Limits and rejections

- Strings are valid [map](GO_MAPS.md) keys and elements (`map[string]V`,
  hashed by length and bytes); range over a map is rejected.
- `+=` is the only compound assignment on strings, as in Go; integers
  accept every compound operator ([integers](GO_INTEGERS.md#compound-assignment)).
- Named slice types stay rejected, including byte slices converted from
  strings (`unsupported type program.B` for `type B []byte`).
- Standard-library packages other than `math/bits` and those of
  [GO_STDLIB.md](GO_STDLIB.md) (`fmt`, `io`, ...) are rejected as above.
- Strings are not part of schema-2 wire layouts: they have no fixed length.
  A string field is rejected with `wire field Name: string has a variable
  length; wire layouts are fixed-size, use a [N]byte array`
  ([schema 2](SCHEMA2.md)). Generated IDLs are unaffected.
- Go errors stay Go errors: assigning to `s[i]`, `s[lo:hi:max]`, and
  `string(x)` for floats are rejected by the type checker.

## Backends and reports

Strings are ordinary `types.Basic` values (`types.String`, or
`types.UntypedString` for constants) in the typed program; Check adds no new
fields. A backend lowers them with the representation and allocation rules
above, the bounds checks, and Go's UTF-8 decoding and encoding; see
[the backend interface](BACKEND_INTERFACE.md#strings). `gosvm features` reports
`string_values`, `string_concatenations`, `string_conversions` and
`string_ranges`, and `uses.strings` ([feature report](FEATURES_REPORT.md)).

## Evidence

- [`examples/string-values`](../examples/string-values/program.go) is an
  ordinary-Go oracle: literals with every escape, `len`, indexing and slicing
  with bounds panics (including signed indexes), concatenation trees, `+=` on
  variables, fields, elements and through pointers (including the order of a
  target read and a call operand that changes it), aliasing and immutability
  (input changed after `string(b)`, `[]byte(s)` changed after conversion,
  slices of a reassigned string), `[]byte`/`[]rune` capacities and append
  growth after a conversion, comparisons and an insertion sort, rune
  conversions of valid, surrogate, maximum and out-of-range values, `range`
  over random and edge-case UTF-8 (overlong forms, surrogates, U+10FFFF and
  beyond, truncated sequences, stray continuation bytes, `F8`/`FF`), `switch`
  with constant and computed cases, strings in structs, arrays, slices and
  pointers, error values through interfaces (nil, typed nil pointers, value
  and pointer receivers, comparisons, assertions, type switches, a nil-pointer
  `Error()` panic), strings in interfaces, and generic code over `~string`.
  `TestStringDifferential` compares 14,116 vectors (3,341 Go panics, each an
  abort at the same point) natively and as host-compiled C with the default
  32 KiB heap.
- `TestStringHeap`: the exact out-of-memory point of every allocating kind
  (concatenation, `string(b)`, `[]byte(s)`, `string(rune)`, `[]rune(s)`,
  boxing, `[]byte` of a constant, and a non-allocating concatenation) for seven
  operand sizes, against computed expectations (105 boundaries).
- `TestStringIntegerConversions`: `string(x)` for all ten integer types and
  226 values against Go's conversion (through `reflect`, which implements it;
  vet rejects `string(int64)` in Go source).
- `TestStringSBF` ([results](../results/compiler/2026-10-14-strings/README.md))
  runs 518 cases on fogo-v0 and solana-v3 through the pinned runners: 488
  native-Go vectors (86 aborts with rollback) and 30 heap-boundary cases
  (14 out-of-memory aborts with rollback, 16 completions).
- `TestStringAcceptedForms`, `TestStringRejections` (exact messages),
  `TestStringRuntimeOnlyWhenUsed`, `TestSchema2RejectsStrings` and
  `TestStringCounters` cover forms, diagnostics, determinism and reports.
- Programs without strings emit byte-identical C: the frozen corpus matches
  except for three recorded entries (the `error` result and `[]byte("abc")`
  now compile; the `fmt` import names `math/bits`), and the full-swap ELFs are
  unchanged (fogo-v0 `4697a9f5…`, solana-v3 `2b5f21d9…`).

The local runners are not a public network; see
[target profiles](TARGET_PROFILES.md).
