# math/bits on-chain

On-chain programs, and the user packages they import, may `import "math/bits"`.
It is the only standard-library package available on-chain. Calls compile to
small C helpers (`internal/compiler/bits.c.txt`) and produce the same results as
the Go standard library, including where Go panics. Native tests call the real
`math/bits`, so the SBF program and the native test agree.

```go
import "math/bits"

// floor(a*b/d), or 0 when d == 0 or the quotient does not fit in 64 bits.
func mulDiv(a, b, d uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if d == 0 || hi >= d {
		return 0
	}
	q, _ := bits.Div64(hi, lo, d)
	return q
}
```

A plain or renamed import (`import wide "math/bits"`) works in a single file
(no `go.mod` needed), in a module program and in shared user packages. Dot and
blank imports are rejected, as they are for every on-chain import. Other
standard-library packages are still rejected.

## Supported

| Group | Functions |
| --- | --- |
| Carry arithmetic | `Add64`, `Sub64`, `Add32`, `Sub32` |
| Full-width multiply | `Mul64`, `Mul32` |
| Full-width divide | `Div64`, `Rem64`, `Div32`, `Rem32` |
| Counts (int result) | `LeadingZeros64/32/16/8`, `TrailingZeros64/32/16/8`, `OnesCount64/32/16/8`, `Len64/32/16/8` |
| Permutations | `Reverse64/32/16/8`, `ReverseBytes64/32/16`, `RotateLeft64/32/16/8` |
| `uint` variants | `Add`, `Sub`, `Mul`, `Div`, `Rem`, `LeadingZeros`, `TrailingZeros`, `OnesCount`, `Len`, `Reverse`, `ReverseBytes`, `RotateLeft` |
| Constant | `UintSize` (64, untyped) |

The compiler type-checks calls against a copy of the Go 1.22 `math/bits` API.
`TestBitsDeclarationsMatchStandardLibrary` checks that every name, signature
(including parameter and result names) and constant value matches the host
toolchain's `math/bits` source. The 16-bit helpers are in
`internal/compiler/bits16.c.txt`, emitted only for programs that also use
`uint16`, so other programs' C is unchanged. `uint` is 64 bits on-chain
([integers](GO_INTEGERS.md)), so each `uint` variant calls its 64-bit helper
in `bits.c.txt` and gives the 64-bit result and traps. `bits.Div` traps where
`Div64` does, and `bits.Rem` where `Rem64` does.

## Not supported

- Function values such as `f := bits.Len64`, as for every function.

## Count results are `int`

The count functions return `int`, an ordinary on-chain type
([integers](GO_INTEGERS.md)). You can store a count, pass it, do arithmetic
with it, or use it as a shift or rotation count:
`n := bits.Len64(x) - 1`.

## RotateLeft counts

The rotation count `k` of `RotateLeft`, `RotateLeft64/32/16/8` is an `int`,
and any `int` expression is allowed, including negative values and values
wider than the operand. Go's rule applies: the rotation is `uint(k) mod width`
bits to the left, so a negative `k` rotates right. A constant `k`
(`bits.RotateLeft64(x, -3)`) is reduced at compile time, which keeps the C of
earlier programs unchanged. Any other `k` is reduced at run time, as
`(u64)k & (width-1)`.

## Semantics and traps

The helpers follow `$GOROOT/src/math/bits/bits.go` (Go 1.22) operation by
operation. They use 64-bit integer operations only, with no 128-bit types, no
compiler library calls, no lookup tables and no syscalls other than `abort`. So
fogo-v0 (v0) and solana-v3 (v3) ELFs use the same code, and import validation is
unchanged on both.

- **Traps.** Each of these calls the VM `abort`, like integer division by zero.
  The transaction fails with no custom error code and rolls back.
  - `Div64(hi, lo, y)` and `Div32` trap if `y == 0` or `y <= hi`. Go panics with
    a divide error or an overflow error in the same cases.
  - `Rem64(hi, lo, y)` and `Rem32` trap only if `y == 0`. Rem64 first reduces
    `hi` modulo `y`, as Go does, so it never overflows.
- **Mul64** uses Go's four 32×32-bit partial products. It does not use
  `unsigned __int128`, so it never calls the `__multi3` helper.
- **Div64** uses Go's algorithm (Hacker's Delight `divlu`): it normalizes the
  divisor, then divides in two 64-by-32-bit steps. No 128/64 division helper
  (`__udivti3`) is needed. When `hi == 0` it uses one native 64-bit divide. Go
  defines a 64-bit shift as 0, but C leaves it undefined, so the helper handles
  that one case (`lo >> (64 - s)` with `s == 0`) explicitly.
- **Add64/Sub64 carry inputs other than 0 and 1.** Go documents only 0 and 1.
  On amd64 and arm64, the Go compiler replaces `Add64`/`Sub64` with an
  add-with-carry instruction that treats any nonzero carry or borrow as 1. On
  those hosts this differs from the package's source formula (`x + y + carry`).
  goSVM matches the compiled behaviour, because that is what native tests on
  amd64 and arm64 run. On architectures that do not replace these calls (for
  example riscv64 and wasm), Go uses the source formula, and a native test there
  could differ for a carry of 2 or more. `Add32`/`Sub32` are never replaced by
  the compiler, so goSVM follows their source formula for every carry value. For
  example, `Add32(x, y, carry)` can return a carry-out of 2.
- The count functions return values 0..64 (as `int`). Results are bit-for-bit
  identical to Go for every input.

## Evidence

- `TestBitsDifferentialAgainstGo` (`internal/compiler/bits_diff_test.go`)
  compiles one program that calls every supported function, including the
  `uint` variants, every `RotateLeft` width with 29 constant counts and with a
  run-time count, and count results stored and used in arithmetic. It compiles
  the program to C and runs 1,125,806 edge and random vectors (167 operations,
  39,833 traps) through the host binary. Each result, and each trap, is
  compared with the host's real `math/bits`. Edge vectors include 0, 1, max,
  every power of two ±1, alternating bit patterns, `hi = y-1` (largest valid),
  `hi = y` and `hi = y+1` (overflow), `y = 0` and `y = 1`, and carries 0, 1, 2,
  3, max and 2^63.
- `TestBitsSBF` (`internal/compiler/bits_sbf_test.go`) builds
  `testdata/bitsprobe` for fogo-v0 and solana-v3 with platform-tools v1.57. It
  runs 4,261 Mul64/Div64/Add64/LeadingZeros64/Sub64/Rem64/32-bit vectors and 11
  trap transactions (with rollback assertions) through the pinned 0.5.0 and
  0.6.0 runners. It skips when the backend or runner is missing.
- [CU comparison and results](../results/compiler/2026-10-14-math-bits/README.md):
  full-swap's hand-written `mulDiv` against the same contract written with
  `bits.Mul64`/`bits.Div64`.
- Check-level tests: `internal/compiler/bits_test.go`.

An alternative backend sees these calls as ordinary typed calls into package
`math/bits` and needs its own lowering
([backend interface](BACKEND_INTERFACE.md)). The traps are part of the
[error contract](ERROR_CONTRACT.md).
