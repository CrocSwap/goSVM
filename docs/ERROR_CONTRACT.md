# Error and failure contract

Stable from October 14, 2026, at the request of the Qushta verified backend,
which reproduces this behaviour byte for byte. Everything listed here is a
contract. To change any of it, announce the change to Qushta before landing it
and record it in the changelog at the end. Adding a new code in an unused number
does not change the contract; changing the meaning, number or check order of an
existing code does, and so does turning a trap into a code or a code into a trap.

## Traps

These failures call the VM `abort` syscall: a static syscall hash on v3, the
legacy `abort` import on v0 (`internal/compiler/compiler.go`). The instruction
fails with no custom error code and the transaction rolls back.

- Division or modulo by zero, for every integer type, signed or unsigned.
- A negative shift count (a signed count below zero), where Go panics
  ([integers](GO_INTEGERS.md)).
- Array index, slice index or slice-expression bounds out of range, including
  a string index or string slice bounds ([strings](GO_STRINGS.md)).
- Nil pointer dereference, field access or method receiver, including a
  value-receiver method called through a nil `*T` held in an interface.
- Account index out of range in SDK account accessors.
- `math/bits.Div64(hi, lo, y)`, `Div32` and `Div` with `y == 0` or `y <= hi`,
  and `Rem64`/`Rem32`/`Rem` with `y == 0`, where Go panics
  ([math/bits](GO_BITS.md)).
- Out of memory: an allocation (`new`, `make`, a growing `append`, a slice
  literal, an escaping `&T{...}`, a local moved to the heap, a conversion to
  an interface, or a string concatenation or conversion) needs more than the
  remaining invocation heap budget ([allocation](GO_ALLOCATION.md)).
- `make` length or capacity out of range: `len > cap`, a negative length or
  capacity of any signed integer type, or a capacity larger than 2^48 bytes,
  where Go panics `makeslice: len out of range` or `cap out of range`.
- A failed single-result type assertion `x.(T)` or `x.(I)` (the dynamic type
  is not `T`, does not implement `I`, or `x` is nil), where Go panics
  ([interfaces](GO_INTERFACES.md#traps)).
- Comparing two interface values (with `==`, `!=` or a `switch`, directly or
  as elements of arrays or fields of structs) whose identical dynamic type is
  not comparable, where Go panics `comparing uncomparable type`.
- Calling a method on a nil interface value, after the receiver and the
  arguments are evaluated (Go's nil pointer dereference).
- Calling a nil function value, after the function value and the arguments
  are evaluated; a nil deferred function value aborts when the deferring
  function returns, after its body, where Go panics
  ([function values](GO_FUNCTIONS.md#traps)). A nil interface, or a value
  method through a nil pointer, in a `defer` statement aborts at the statement
  after its arguments are evaluated (the existing nil-interface and
  nil-pointer traps), as Go panics there.

A trap aborts without running pending deferred calls (there is no `recover`);
the instruction's effects roll back, so this is indistinguishable on chain
from Go's unwinding.

A program built with a heap budget above 32 KiB whose transaction did not
request a large enough heap frame fails at its first allocation with a VM
access violation (also `ProgramFailedToComplete`, consuming the remaining
compute), not with the abort syscall.

These are **not** traps:

- `+`, `-`, `*`, unary `-` and `++`/`--` wrap modulo 2^width, two's complement
  for signed types.
- Signed division truncates toward zero, and the remainder takes the
  dividend's sign. `MinInt / -1` is `MinInt` and `MinInt % -1` is 0, for each
  signed width.
- A shift by the operand's width or more yields 0, or -1 for a signed `>>` of a
  negative value.
- Conversions truncate, or sign- or zero-extend by the source's signedness.
- Other `math/bits` calls never fail. [GO_BITS.md](GO_BITS.md) specifies their
  exact results, including `Add64`/`Sub64` carry inputs other than 0 and 1.

## Contracted runtime behaviour

Besides traps and codes, these observable results are part of the contract,
because fixtures compare them byte for byte:

- **Append growth.** `append` grows capacity exactly as Go 1.22's
  `runtime.growslice`: `nextslicecap`, then `roundupsize` with Go 1.22's malloc
  size classes, including the 8-byte malloc header for pointer-holding element
  types above 512 bytes. `cap()` after an append, whether an append reuses the
  backing array, and the heap bytes a growth consumes all follow from it. The
  full rule is in [GO_ALLOCATION.md](GO_ALLOCATION.md#append-growth).
- **Heap accounting.** The allocator is a bump allocator over the heap region
  with no free. The first 8 bytes of the budget hold the in-use count, each
  allocation consumes its size rounded up to 8 bytes, and the out-of-memory
  trap fires at the first allocation that does not fit (32 KiB default, so
  32,760 usable bytes; `heap_bytes` up to 256 KiB). Locals moved to the heap by
  escape analysis consume budget the same way; `gosvm features` reports them.
  Converting a value to an interface allocates its Go size rounded up to 8
  bytes, except pointers and zero-size values, which allocate nothing
  ([interfaces](GO_INTERFACES.md#boxing-cost)).
  String literals and constant expressions allocate nothing; a concatenation
  allocates its total length rounded up to 8 bytes, or nothing when at most one
  operand is non-empty; `string(b)` allocates `len(b)` rounded up (nothing when
  empty), `string(rune)` 8 bytes and `string([]rune)` its encoded length
  rounded up; `[]byte(s)` and `[]rune(s)` allocate Go 1.22's `rawbyteslice`
  and `rawruneslice` sizes, which are also their capacities, and `[]byte` of a
  constant exactly its length (capacity `len`). The full table is in
  [GO_STRINGS.md](GO_STRINGS.md#allocation).
  Evaluating a function literal
  that captures `n ≥ 1` variables allocates `8 + 8n` bytes (nothing without
  captures, and nothing for a declared function, instance or method
  expression used as a value); a method value allocates 16 bytes for a
  pointer receiver and `8 + ((size+7)&^7)` for a value receiver of Go size
  `size`; captured variables are always moved to the heap (a captured
  three-clause loop variable is also copied to a fresh allocation before each
  post statement); a `defer` statement inside a loop allocates a 16-byte header
  plus its saved values as a C struct, rounded up to 8, per execution, and one
  outside loops allocates nothing ([function values](GO_FUNCTIONS.md#heap-cost)).
  See [GO_ALLOCATION.md](GO_ALLOCATION.md#allocator).

## Error codes

A nonzero `Process` result is returned unchanged as the transaction's custom
error. Generated entrypoints, adapters and SDK helpers use these numbers;
application handlers use `Err*` constants in 1..5999.

### Entrypoint (generated C)

| Code | Meaning |
| --- | --- |
| 1001 | More accounts than the program's maximum |
| 1002 | Malformed duplicate-account marker |
| 1003 | Account data longer than 10 MiB |
| 1004 | Instruction data longer than 10,240 bytes |

SDK 1 single-account programs use 10–14 for their input checks
(`internal/compiler/abi.c.txt`).

### Runtime boundary (`solana` package)

| Code | Meaning |
| --- | --- |
| 2001 | Program account not executable, or not the classic Token program where required |
| 2002 | SDK 1 signed-transfer seed longer than 32 bytes |
| 2003 | SHA-256 input or output range out of bounds |
| 2004 | SHA-256 result not 32 bytes |
| 2005 | Account index out of range |
| 2006 | Malformed CPI account metas |
| 2007 | Malformed signer seeds or PDA input |
| 2008 | CPI instruction data longer than 10,240 bytes |
| 2009 | Native callback missing (native tests only) |
| 2010 | Resize or original length beyond limits |
| 2011 | Account not writable, or executable |
| 2012 | Account not owned by the program |
| 2013 | Refund account is the source, or malformed native key |
| 2014 | Refund lamport overflow |
| 2015 | Rent output not 17 bytes |
| 2016 | Clock output not 40 bytes |
| 2017 | Return data longer than 1,024 bytes |

### SDK 2 helpers

| Code | Meaning |
| --- | --- |
| 3001 | Seeds: more than 16, or one longer than 32 bytes, or nil where required |
| 3002 | CPI metas: more than 16, or nil |
| 3003 | CPI signers: more than 2 groups or 1,024 bytes |
| 3010 | Token account invalid (owner, executable, 165-byte layout, state, native) |
| 3011 | Token account identity |
| 3012 | Token mints differ |
| 3013 | Token authority or close authority mismatch, or missing signature |
| 3014 | Token account not writable |
| 3020 | Rent: size over 10 MiB, overflow or invalid threshold |
| 3021 | System program account invalid |
| 3022 | System create: account, alias, layout or privilege |
| 3023 | System create: payer signature, or target signer or PDA |
| 3024 | System create: size over 10,240 bytes |

Once a CPI's invoke syscall runs, a failure inside the callee fails the whole
transaction; the codes above come only from checks made before invoking.

### Schema 2 adapter

Checks run in this order; within each phase, accounts follow declaration order
(`internal/project/generate2.go`, [schema 2](SCHEMA2.md)):

1. Instruction discriminator and length: 6004.
2. Account count: 6000.
3. Writable, signer and executable flags: 6001.
4. Program ID length (6002, only when an account declares `owner:
   "program_id"`), then per account: key length and fixed address (6005),
   owner (6002).
5. Duplicate keys not declared as aliases: 6006.
6. Initialization target and payer empty: 6009.
7. Owned state length and discriminator: 6003.
8. Checked token layouts: SDK codes 3010–3014.
9. Relations, including token mint and authority: 6007.
10. Close authority: 6007.
11. PDA re-derivation: 6008.
12. Initialization (`system.Create` codes), in declaration order.
13. The handler; a nonzero result returns unchanged.
14. Post-handler close of declared `close` accounts (`CloseAccount` codes).
15. Write-back of writable, non-closed state, in declaration order. For
    instructions with init or close declarations, each such account is first
    re-read and re-checked: length (6003), owner (6002), then the
    discriminator (6003), or, for a newly initialized account, the
    discriminator is written. Read-only state, token accounts and other
    references are never written back.

Handler results propagate unchanged.

## Changelog

- 2026-10-14: contract established at `da6bf04` plus this document.
- 2026-10-14: steps 4 and 12–15 spelled out to match existing generated code
  (no behaviour change).
- 2026-10-14: addition (new functions), no existing behaviour changed:
  `math/bits` traps for `Div64`/`Div32` (`y == 0` or `y <= hi`) and
  `Rem64`/`Rem32` (`y == 0`).
- 2026-10-14: addition (signed integers), no existing behaviour changed: the
  division trap covers signed division, a negative shift count traps, and the
  wrapping, `MinInt / -1` and conversion rules above. The `math/bits` `uint`
  variants share the 64-bit traps (`Div`, `Rem`).
- 2026-10-14: addition (allocation), no existing behaviour changed: out-of-memory
  and `make` length/capacity-out-of-range traps; the heap-frame access
  violation for budgets above 32 KiB.
- 2026-10-14: signed integers, generics and allocation combined, no trap added
  or changed: negative signed `make` lengths and capacities, indexes and slice
  bounds use the traps above, and instances of generic code allocate and trap
  exactly as the equivalent hand-written code.
- 2026-10-14: append growth and heap accounting recorded as contracted
  behaviour at Qushta's request (documentation only, no behaviour change).
- 2026-10-14: addition (interface values), no existing behaviour changed: a
  failed single-result type assertion, comparing interfaces whose identical
  dynamic type is not comparable, and calling a method on a nil interface
  abort; boxing allocates as recorded under heap accounting; a value-receiver
  method through a nil `*T` in an interface and boxing beyond the budget use
  the existing nil-pointer and out-of-memory traps.
- 2026-10-14: addition (strings and `error`), no trap added and no existing
  behaviour changed: string index and slice bounds use the bounds trap, string
  concatenations and conversions allocate as recorded under heap accounting
  and use the out-of-memory trap, and `[]byte(s)`/`[]rune(s)` capacities are
  contracted with it; `error` values use the interface traps.
- 2026-10-14: addition (function values, closures, defer, named results), no
  existing behaviour changed: calling a nil function value aborts (a nil
  deferred value when the function returns); the heap cost of closure
  environments, method values, captured variables and deferred calls in loops
  is recorded under heap accounting; deferred calls do not run when a trap
  aborts. A nil interface or nil pointer in a `defer` statement, closures
  beyond the budget and comparisons of interfaces holding function values use
  the existing traps.
