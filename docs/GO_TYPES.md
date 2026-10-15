# Struct comparison, type aliases and panic on-chain

Three gaps that kept existing pure-Go libraries off-chain are closed, with
Go's semantics:
[the October 14 library survey](../results/compiler/2026-10-14-library-shakeout/README.md)
found them in `lukechampine.com/uint128` (`u == Zero`, `panic("overflow")`)
and the Uniswap-v3 utilities (`type Uint160 = uint256.Int`):

- `==` and `!=` on structs, and `switch` on a struct tag;
- type alias declarations, `type A = T`;
- `panic(v)`, which aborts the transaction and logs one line.

[Arrays of composite elements](GO_ARRAYS.md) (`[N]uint256.Int`, tables of
`[...]Int{...}`) were the fourth gap; they are documented with arrays.

```go
type Uint128 struct{ Lo, Hi uint64 }

type U = Uint128 // an alias: the same type

func (u Uint128) IsZero() bool { return u == Uint128{} }

func (u Uint128) Add(v Uint128) Uint128 {
	lo, carry := bits.Add64(u.Lo, v.Lo, 0)
	hi, carry := bits.Add64(u.Hi, v.Hi, carry)
	if carry != 0 {
		panic("overflow") // the transaction fails; logs "panic: overflow"
	}
	return Uint128{lo, hi}
}
```

Programs that use none of these compile to byte-identical C and ELFs: struct
comparison and aliases need no runtime, and only programs that call `panic`
get its runtime and the log syscall.

## Struct equality

`x == y` and `x != y` compare two values of a comparable struct type (go/types
rejects uncomparable ones: structs with slice, map or function fields). As Go
specifies, the non-blank fields are compared in declaration order and the
comparison stops at the first difference:

| Field kind | Compared as |
| --- | --- |
| Integers, `bool`, pointers | By value (pointers by address) |
| Strings | Bytewise |
| Arrays | Element by element, in index order, stopping at the first difference |
| Structs (named, or `struct{}`) | Recursively; `struct{}` and zero-length arrays are always equal |
| Interfaces | By dynamic type and value; an uncomparable identical dynamic type (a slice, map or function value) aborts, as Go panics `comparing uncomparable type` |
| `_` (blank) | Ignored, as Go ignores blank fields |

Because comparison stops at the first difference, an interface field holding
an uncomparable value only aborts when every earlier field is equal and both
interfaces hold the same uncomparable type, exactly when Go panics.

The same comparison serves `switch` on a struct tag (cases are tested in
source order and stop at the first match), comparisons of interface values
holding structs (already supported with [interfaces](GO_INTERFACES.md)),
arrays of structs, and map keys. Map keys and interface comparisons now also
ignore blank fields, as Go does (before this change, two struct keys differing
only in a blank field were distinct keys).

Comparing structs allocates nothing and cannot fail except through an
interface field. Its compute cost is that of comparing each field in turn;
the C backend inlines the field comparisons.

## Type aliases

`type A = T` declares a second name for `T`, as in Go. go/types (Go 1.22)
resolves every use of `A` to `T`, so an alias has no representation of its
own and the rules of `T` apply wherever `A` is used:

- aliases of scalars, strings, structs, arrays, slices, maps, pointers,
  interfaces (`type E = error`), function types and other aliases;
- aliases of types in other packages (`type Uint160 = uint256.Int`) and of
  instantiated generic types (`type IntBox = Box[uint64]`);
- methods declared through an alias of a local type (`func (p Pair2) M()`
  declares `Pair.M`), and `type Named Alias`, a new defined type whose
  underlying type is the alias's;
- aliases used in map keys and values, interface values, conversions,
  composite literals, `switch` cases and type switches.

An unused alias of a named type is never checked by itself (the named type is
checked at its declaration); an alias of an unnamed type must be a storable
type (`type F = float64` is rejected like a `float64` variable). Generic alias
declarations (`type A[T any] = ...`) are not valid Go 1.22. The compiler
relies on go/types' default alias resolution: a binary run with
`GODEBUG=gotypesalias=1` rejects alias declarations with a diagnostic rather
than miscompiling.

`gosvm features` reports `uses.type_aliases`.

## Panic

`panic(v)` evaluates `v`, writes one log line with the VM's `sol_log_` syscall
and calls the `abort` syscall, like every other [trap](ERROR_CONTRACT.md#panic).
The instruction fails with `ProgramFailedToComplete` and the transaction rolls
back, so a panic is observable on chain as the failure plus its log line,
`Program log: panic: ...`. The exact line format, including truncation and
UTF-8 handling, is contracted in
[ERROR_CONTRACT.md](ERROR_CONTRACT.md#panic).

| Argument | Line (as Go 1.22 prints it) |
| --- | --- |
| A string (constant or not) | `panic: ` + the string |
| An integer or `bool` | `panic: -5`, `panic: true` |
| A named string, integer or `bool` type | `panic: pkg.Label("x")`, `panic: pkg.Code(5)` |
| `nil`, or a nil interface value | `panic: panic called with nil argument` |
| A non-nil interface value (including `error`) | `panic: (error)`: the static type, where Go prints the dynamic value's `Error()` or `String()` result (a deviation) |
| A type with an `Error` or `String` method, or any other type | `panic: (pkg.T)`, where Go prints the method's result or `(pkg.T) 0xADDRESS` (a deviation) |

The line is at most 512 bytes; a longer line is truncated. `sol_log_` fails on
invalid UTF-8, so each byte that does not start a valid UTF-8 sequence
(including one cut by the 512-byte limit) is logged as `?`. Constant lines are
computed at compile time; `pkg` is the package name, as Go's runtime spells
type names.

`panic` works anywhere a call statement does: in functions, methods,
closures, instances of generic code and deferred calls (a deferred function
that panics runs when the surrounding function returns, then aborts). It may
be a function's last statement, as Go allows. `defer panic(v)` is rejected
(defer a function literal that panics instead).

**Deferred calls and recovery.** Go runs pending deferred calls while a panic
unwinds and lets them `recover`. On-chain, a panic aborts the whole
transaction and every effect rolls back, so deferred calls are **not run**
after a panic (as after any other trap) and `recover` is rejected:
`recover unsupported: a panic aborts the transaction and rolls back its
effects, so there is nothing to recover`. The transaction fails either way;
the only difference from Go is that a deferred call's own log lines and
compute are absent. A custom error code is not possible: the SVM cannot
unwind to the entrypoint, so the failure is the abort trap's.

Panic allocates nothing (the argument is never converted to an interface, so a
program using only `panic` does not get the interface runtime). Its compute
cost, on the failure path only, is the log syscall plus copying and checking
the line: `panic("overflow")` cost 379 CU and a 64-byte string 1,789 CU more
than returning normally in the
[measured probe](../results/compiler/2026-10-14-value-types/README.md).

`gosvm features` reports `uses.panic`.

## Backends and reports

`CheckedProgram.Panics` maps each `panic` call (by its `*ast.CallExpr`; calls
inside instances of generic code are keyed by the instance `Decl`'s nodes) to
a `PanicSite`: the complete line for constants, `nil` and opaque values
(`PanicText`), or a prefix and suffix around the formatted run-time value
(`PanicString`, `PanicSigned`, `PanicUnsigned`, `PanicBool`), or an interface
line with the nil case (`PanicInterface`). Struct equality and aliases add no
field. See [the backend interface](BACKEND_INTERFACE.md#value-types).

## Evidence

[Value-types evidence](../results/compiler/2026-10-14-value-types/README.md):
[`examples/value-types`](../examples/value-types/program.go) runs natively and
through goSVM; the [host differential](../internal/compiler/valuetypes_diff_test.go)
compares output, status and each panic's log line for 10,200 vectors, and the
[SBF test](../internal/compiler/valuetypes_sbf_test.go) runs 400 vectors on
fogo-v0 and solana-v3, checking rollback and each panic's log line in the
runner's logs.
