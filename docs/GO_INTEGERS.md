# Integer types on-chain

On-chain programs can use every fixed-size Go integer type, plus `int` and
`uint`, with exactly Go's semantics:

| Unsigned | Signed |
| --- | --- |
| `uint8` / `byte`, `uint16`, `uint32`, `uint64`, `uint` (64 bits) | `int8`, `int16`, `int32` / `rune`, `int64`, `int` (64 bits) |

Named versions (`type Tick int16`) work too. Every integer type is allowed
anywhere an unsigned scalar was before: locals, parameters, results, struct
fields, arrays, pointers, constants, conversions, comparisons, `switch` tags,
`range` counts and `++`/`--`. `len` and `cap` results, `range` keys and the
`math/bits` count results are ordinary `int` values. You can store them, do
arithmetic with them, negate them and pass them around (`n := len(s) - 1`).

```go
type Tick int16

func step(price int64, delta int32, tick Tick) (int64, Tick) {
	next := price + int64(delta)*int64(tick)
	if next < 0 {
		return 0, tick - 1
	}
	return next / 2, tick + 1
}
```

Native tests run this code with real Go, so results and traps must match it
bit for bit. They do, on the host and on SBF (see [Evidence](#evidence)).

## Semantics

These follow the Go specification.

- **Wrapping.** `+`, `-`, `*`, unary `-` and `++`/`--` wrap modulo 2^width
  (two's complement for signed types). `^x` is bitwise complement.
- **Division** truncates toward zero, and the remainder has the dividend's
  sign: `-7 / 2 == -3`, `-7 % 2 == -1`, `7 % -2 == 1`. Division or remainder by
  zero traps. `MinInt / -1 == MinInt` and `MinInt % -1 == 0` for each signed
  width. These do not trap, because Go does not panic.
- **Shifts.** The count can be any integer type, signed or unsigned. A negative
  count traps, because Go panics. A shift by the operand's width or more gives
  0, except a signed `>>`, which fills with the sign bit (0 or -1). `>>` on a
  signed value is arithmetic.
- **Conversions** between integer types truncate, or sign- or zero-extend, by
  the source's signedness: `uint64(int8(-1)) == 1<<64 - 1`,
  `int8(uint64(200)) == -56`.
- **Comparisons** are signed for signed types.
- **`range n`** over a signed `n` runs `max(n, 0)` times. The key has `n`'s
  type.
- **Constants** are checked by `go/types`. Out-of-range constants
  (`var x int8 = 128`), constant negative shift counts (`x << -1`) and constant
  division by zero are compile errors with the `go/types` message.

Still rejected: `uintptr`, floating-point and complex types, strings, slices
of anything but bytes (`[]int8`), and builtins other than `len` and `cap`
(including `min` and `max`). Compound assignment (`x += 1`) is still rejected,
for every type.

Schema 2 wire layouts accept `int8`, `int16`, `int32` and `int64` fields and
seeds, as little-endian two's complement. `int` and `uint` are rejected in wire
layouts because their size depends on the platform. See [schema 2](SCHEMA2.md).

## C lowering

This section is for backend authors and auditors. The C backend (with Clang)
lowers the types as follows.

- **Types.** `int64` and `int` are the prelude's `i64`, and `uint` is `u64`.
  `int8`, `int16` and `int32` are `i8`, `i16` and `i32`. Their typedefs are
  emitted only for programs that use them.
- **Arithmetic** `+ - * & | ^ &^`, unary `-` and `^` are computed in `u64`, the
  same lowering as for unsigned types. There is no C signed overflow.
  Assigning to the typed temporary truncates to the width. Clang defines the
  conversion of an out-of-range value to a signed type as modulo 2^width, which
  is Go's result. `++`/`--` on a signed target is lowered to
  `x = (T)((u64)x ± 1ULL)`, never C `++`.
- **Ordered comparisons** compare the sign-extended `i64` values. When both
  operands are known non-negative (a non-negative constant, or a `len`, `cap`
  or `math/bits` count result), the earlier unsigned `u64` comparison is kept.
  It is exact for those values, and it keeps the emitted C of earlier
  programs unchanged. Equality uses `u64`, which is exact for values of the
  same type.
- **Division.** SBF has no signed divide instruction, so LLVM would call
  `__divdi3`/`__moddi3`, which the runtime does not provide. After the existing
  zero-divisor check (`if (y == 0) abort();`), `gosvm_quo64`/`gosvm_rem64`
  divide the magnitudes as `u64` and restore the sign. The quotient is negated
  when the operand signs differ, and the remainder takes the dividend's sign.
  `|MinInt64|` is 2^63 as a `u64`, so `MinInt64 / -1` yields 2^63, which
  converts back to `MinInt64`, and `MinInt64 % -1` yields 0, with no special
  case. Narrower types are divided as sign-extended `i64` values, and the typed
  store wraps `MinInt8 / -1` and the like. The helpers are emitted only for
  programs with a signed `/` or `%`.
- **Shifts.** A count that could be negative is checked first:
  `if (n < 0) abort();`. Unsigned counts, non-negative constants and
  `len`/`cap`/`math/bits` count results are never negative, so they get no
  check. `<<` (and unsigned `>>`) keep the existing lowering:
  `n >= width ? 0 : (u64)x << n`. Signed `>>` is `(i64)x >> (n >= 63 ? 63 : n)`,
  an arithmetic shift of the sign-extended value. A count of 63 or more gives 0
  or -1 for every width. Clang lowers signed right shift arithmetically, and
  SBF has `arsh`.
- **Range.** A count that could be negative is clamped:
  `(n < 0 ? 0 : (u64)n)`.
- **Constants.** Negative constants are spelled `(-5LL)`, and `MinInt64` is
  `(-9223372036854775807LL-1)`. Non-negative constants keep the `%dULL`
  spelling.

The generated C was also run under Clang UBSan (`-fsanitize=undefined,integer`,
excluding only the intended unsigned wrap and implementation-defined
narrowing). It ran 2.29 million fixture vectors with no report.

Programs that use no signed type and no `uint` emit byte-identical C. Of the
frozen corpus programs, only those rejected before for using `int` changed
(`corpusChanges` in `internal/compiler/check_corpus_test.go`). The full-swap
ELFs are unchanged: fogo-v0 `4697a9f5…` and solana-v3 `2b5f21d9…`.

## Traps

Two runtime failures call the VM `abort`, like every trap in the
[error contract](ERROR_CONTRACT.md). The transaction fails with no custom
error code and rolls back.

- Division or remainder by zero, for any integer type (an existing trap).
- A negative shift count (new). Go panics with
  `runtime error: negative shift amount`.

## Evidence

- Check tables: `TestIntegerChecksAccept` and `TestIntegerChecksReject`
  (exact messages), in `internal/compiler/signed_test.go`, and the updated
  range, `math/bits`, array and value tables.
- [`examples/int-values`](../examples/int-values) is the ordinary-Go oracle.
  `ops.go` is generated by `TestIntValuesFixture`. It covers 13 types (every
  integer type, plus `I16 int16`, `N int` and `W uint`) and 62 operations per
  type: every binary and unary operator, comparisons, `++`/`--`, `<<` and `>>`
  with each of the 10 count types, conversions to all 13 types, `switch` tags,
  `range` counts, pointer updates, typed constants (including `MinInt`/`MaxInt`),
  struct and array storage, mixed widths, a multiple-result function and
  constant shifts.
- `TestIntValuesDifferential` compiles the fixture to C with the host Clang. It
  compares 2,492,496 vectors with ordinary Go, including 3,660 division-by-zero
  and 87,390 negative-shift traps that abort exactly where Go panics. Vectors
  are edge × edge for each type (0, ±1, min, max and their neighbours, powers
  of two, raw 64-bit patterns that conversions truncate). Shift vectors are
  edge × count, with counts within, at and beyond every width, and negative.
  Each operation also gets 300 random vectors. `MinInt / -1` and
  `MinInt % -1` are checked for every signed type.
- `TestIntValuesSBF` builds `internal/compiler/testdata/intprobe` with the
  fixture for fogo-v0 and solana-v3 (platform-tools v1.57). It runs 59,930
  sampled vectors, `MinInt / -1` for every signed type, and 183 trap
  transactions through the pinned 0.5.0 and 0.6.0 runners. The traps are
  division by zero, and negative counts of every signed count type, for every
  type. One trap transaction checks that earlier successful records are rolled
  back. The test skips when the backend or a runner is missing.
- `TestSchema2SignedFieldsAndSeeds` (`internal/project/signed_test.go`) covers
  signed state and argument fields, a named `int16` array, signed PDA seeds,
  codec round trips at the extremes, and the handler's wrapping arithmetic
  (including `MinInt32 / -1`), natively.
- The `math/bits` `uint` variants and non-constant `RotateLeft` counts are in
  `TestBitsDifferentialAgainstGo` ([math/bits](GO_BITS.md)).

The native oracle needs a correct Go compiler. Go 1.22.0 on darwin/arm64
miscompiles `uint64(v*3) ^ uint64(v)<<16` for a `uint32` `v` when optimization
is on: `v = 0xffffffff` gives `0x0000fffd0000fffd` instead of
`0x0000ffff0000fffd`. `-gcflags=-N -l` and amd64 builds are correct. The
fixture avoids that pattern. The goSVM C output was correct.
