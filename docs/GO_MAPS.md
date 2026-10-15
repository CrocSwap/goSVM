# Maps on-chain

On-chain Go code can use maps with Go's results for every operation except
iteration: `make`, literals, `m[k]`, `v, ok := m[k]`, assignment, `++`/`--`
and `+=` on elements, `delete`, `len`, `clear`, nil maps, and maps stored
anywhere a value can be stored. `range` over a map is rejected, because Go
randomizes its order (below). The table lives on the invocation heap and its
heap use is a documented function of the live-key counts, so out-of-memory
points are predictable.

```go
type Ledger struct {
	balances map[[32]byte]uint64
	seen     map[string]struct{}
}

func (l *Ledger) credit(owner [32]byte, amount uint64) {
	if l.balances == nil {
		l.balances = make(map[[32]byte]uint64)
	}
	l.balances[owner] = l.balances[owner] + amount
}

func Process(s, i []byte) uint64 {
	var l Ledger
	var owner [32]byte
	copy(owner[:], i)
	l.credit(owner, 5)
	l.credit(owner, 7)
	l.seen = map[string]struct{}{"deposit": {}}
	if _, ok := l.seen[string(i[:7])]; ok {
		s[0] = 1
	}
	// Keep the keys in a slice and range over that, never over the map.
	keys := [][32]byte{owner}
	for _, k := range keys {
		s[1] = byte(l.balances[k])
	}
	delete(l.balances, owner)
	return uint64(len(l.balances))
}
```

Programs that use no map value compile to byte-identical C and ELFs: the map
runtime is emitted only when a program has a map type.

## Supported

| Form | Semantics |
| --- | --- |
| Types | `map[K]V` for any comparable `K` the subset admits: every integer type, `bool`, strings, arrays, structs of comparable fields, pointers, interfaces (and named versions of these). `V` is any storable type: scalars, strings, structs, arrays, slices, pointers, interfaces, maps, function values and `struct{}`. Named map types (`type Set map[string]struct{}`), with methods, and generic ones (`type Set[T comparable] map[T]struct{}`) |
| `make(map[K]V)`, `make(map[K]V, hint)` | A new empty map. The hint may have any integer type; as in Go, a negative hint, or one whose table would exceed 2^48 bytes, is treated as 0 and never panics. A positive hint only presizes the table ([heap](#heap-accounting)) |
| `map[K]V{k: v, ...}` | Keys and values evaluated in source order, each pair inserted after it is evaluated; a computed duplicate key keeps the last value. Elided element and key types (`map[P][]P{{1, 2}: {{3, 4}}}`), nested map literals, elided `&T{...}` values |
| `nil` | The zero value: `m == nil`, `m != nil` (maps compare only with nil). Reading, `len`, `delete` and `clear` work on a nil map; writing to one aborts, as Go panics (`assignment to entry in nil map`) |
| `m[k]` | A copy of the element, or `V`'s zero value when `k` is absent. The map is evaluated, then the key |
| `v, ok := m[k]` | Also `v, ok = m[k]`, `var v, ok = m[k]`, `_, ok := m[k]`, in `if` and `switch` init statements, and `v` of an interface type receiving a concrete element |
| `m[k] = v` | Inserts or overwrites. The map and key operands are evaluated, then `v`, then the element is stored (Go's order: in `m[k] = f()` where `f` writes `m[k]`, the assignment wins). In tuple assignments (`m[a], m[b] = x, y`, swaps through a map) all operands are evaluated first and stores happen left to right |
| `m[k]++`, `m[k]--`, `m[k] op= v` | `++`/`--` and every [compound assignment](GO_INTEGERS.md#compound-assignment) (`+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `\|=`, `^=`, `&^=`, `<<=`, `>>=`) on integer elements (wrapping as Go does), and `+=` on string elements; an absent key starts from the zero value. The map and key are evaluated, then the right operand, then the key is looked up once (inserted when absent) and the element updated in place: the element is read after the right operand, as gc does |
| `delete(m, k)` | Removes `k`; a nil map or an absent key is a no-op |
| `len(m)` | The number of keys; 0 for a nil map |
| `clear(m)` | Removes every key (Go 1.21); a no-op on a nil map. `clear` of a slice remains unsupported |
| Map values | References: assignment, parameters, results, struct fields, slice and array elements, pointers (`*map[K]V`), interface values and type conversions (`Set(m)`, `map[string]struct{}(s)`) copy the reference, and every copy sees every mutation, as in Go |
| Elements | `m[k][i] = x` for slice elements, `m[a][b] = x` for nested maps, `m[k].F` reads, `m[k].M()` value-receiver calls, `m[k] = append(m[k], x)`. As in Go, `&m[k]`, `m[k].F = v` and pointer-receiver calls on `m[k]` are compile errors |
| Interfaces | A map converts to an interface without allocating (its header pointer is the data word); `x.(map[K]V)`, `case map[K]V:` and methods of named map types through interfaces work. Comparing two interfaces holding maps aborts (uncomparable), as Go panics |
| Generics | Map types as type arguments and constraints (`M ~map[K]V`), `make(M)`, generic map types, generic structs with map fields, `comparable` keys including interfaces |
| `struct{}` | The unnamed empty struct is admitted as a zero-size value, for sets: `map[K]struct{}`, `struct{}{}`, `struct{}` fields and elements |

**NaN keys.** The subset has no floating-point types, so Go's NaN-key behaviour
(a NaN key never equals itself) cannot arise.

## With function values

Maps combine with [function values, closures and `defer`](GO_FUNCTIONS.md#with-maps)
with no extra rules: maps of function values (an absent key yields a nil
function value), function types over maps, closures capturing maps by
reference, deferred calls that read and change maps (a map operand saved at
the `defer` statement is a reference, so the call sees later writes), named
map results, and method values and method expressions of map-backed named
types, generic ones included. A function value used as an `any` key aborts
(unhashable), and a nil-map write in a closure or deferred call aborts where
Go panics. See [`examples/func-maps`](../examples/func-maps/program.go).

## No iteration

`for k, v := range m` and every other range form over a map (`for k := range
m`, `for range m`, `for k, v = range m`, over a named map, a field or a call,
and inside generic code instantiated with a map type) is rejected with:

```text
range over maps unsupported: Go randomizes map iteration order, so it cannot be deterministic on-chain; keep the keys in a slice and range over that
```

Go deliberately randomizes the order of map iteration, so a native test cannot
pin it, and an on-chain program must be deterministic across validators. Every
other map operation has a single correct result in Go, which goSVM reproduces
exactly. To visit the entries, keep the keys in a slice next to the map (append
a key when it is first inserted) and range over the slice.

## Representation

A map value is a pointer to a 24-byte header on the invocation heap (nil for a
nil map): the live-key count, the slot count (a power of two, or 0 before the
first slot array exists) and the slot array. A slot is an 8-byte hash word
(0 for an empty slot; occupied slots have bit 63 set), the key and the
element, each rounded up to 8 bytes; a zero-size key or element takes no
space. The slot size of `map[K]V` is

    8 + roundup8(sizeof(K)) + roundup8(sizeof(V))

with Go sizes: `map[uint64]uint64` 24 bytes, `map[string]struct{}` 24,
`map[[32]byte]uint64` 48, `map[any]bool` 32.

The table uses open addressing with linear probing from `hash & (slots - 1)`
and backward-shift deletion: removing an entry moves later entries of its
probe run back into the hole, so there are no tombstones and the table never
holds deleted entries. The hash word stores the full hash, so growing does not
rehash keys.

### Hashing

The hash is a fixed-seed 64-bit mix over the key's canonical value: integers
and `bool` by value, pointers by address, strings by length and then bytes (8
at a time), arrays element by element, structs field by field (padding and
blank `_` fields are never hashed or compared, as Go ignores blank fields),
interfaces by dynamic type ID and then the dynamic value (pointers by
address; zero-size values contribute nothing). Equal keys therefore always hash
equally, including equal strings in different storage and identical dynamic
types. There is no per-run random seed: every result except iteration order is
independent of the hash, so a fixed seed changes nothing Go programs can
observe, and it keeps execution deterministic.

Hashing an interface whose dynamic type is not comparable (a slice, a map, or
a struct or array containing one) aborts, as Go panics (`hash of unhashable
type`). As in Go this happens in every operation that hashes the key, including
lookups and deletes on nil and empty maps.

**Adversarial keys.** The hash is not keyed, so a program whose keys come from
untrusted input can be fed keys that collide in the low bits and make probe
runs long. The cost is deterministic (the same transaction always takes the
same compute) and bounded by the table size, but a lookup could degrade from a
few probes to scanning a full run, exhausting the transaction's compute budget
rather than corrupting state. Bound the number of attacker-chosen keys per
transaction, or derive keys from hashed or validated data, when that matters.

## Heap accounting

Map storage follows the [allocator contract](GO_ALLOCATION.md#allocator)
(8-byte rounding, abort when the budget is exhausted) and is part of the
[contracted runtime behaviour](ERROR_CONTRACT.md#contracted-runtime-behaviour).
With `S` the slot size above, `cap(n)` the smallest power of two of at least 8
that holds `n` keys at load factor 3/4 (`4n <= 3 cap`):

| Operation | Heap bytes |
| --- | --- |
| `make(map[K]V)`, `make(map[K]V, h)` with `h <= 0` (or oversized, as above), `map[K]V{}` | 24 (the header) |
| `make(map[K]V, h)`, `h > 0` | 24, then `cap(h) * S` (allocated at the `make`) |
| A literal with `n > 0` entries | 24, then `cap(n) * S`, before any entry is evaluated; the insertions never grow it |
| Inserting a new key when `4 * (len + 1) > 3 * slots` | `2 * slots * S` (8 slots when there are none); the old slot array is not freed |
| Any other insertion, overwrite, lookup, comma-ok lookup, `delete`, `len`, `clear`, `++`/`--` or `op=` on an existing key | 0 |
| Converting a map to an interface, copying or passing a map | 0 |

Growth depends only on the number of live keys: deletes make room for later
insertions but never shrink or free the slot array, and `clear` keeps it. The
bump allocator never reuses memory, so each growth leaves the previous array
unused until the invocation ends. Keys and elements are copied into the slots;
what they refer to (string bytes, slices, pointers) is not copied. Allocations
made while evaluating the key or value (for example a concatenated string key)
happen before the insertion's growth.

Examples with the default 32 KiB heap (32,760 usable bytes):

- `make(map[uint64]uint64)` then 384 distinct keys: 24 + (8 + 16 + ... + 512)
  slots of 24 bytes = 24,408 bytes fit; the 385th key needs 1,024 slots (24,576
  bytes) and aborts.
- `make(map[uint64]uint64, h)` with 385 <= h <= 768 allocates the 1,024 slots
  at once, so 768 keys fit (the 769th needs 2,048 slots); with `h = 769` the
  `make` itself runs out of memory.
- The insert-then-delete loop `m[k] = 1; delete(m, k)` never grows past 8 slots,
  however many times it runs.

`TestMapHeap` checks these and the other kinds of `examples/map-values` mode 6
against an implementation of this table (19 boundaries, including string keys
whose concatenation allocates, and a slice of 636 fresh maps).

## Compute cost

Measured on SBF with `TestMapSBF` (fogo-v0 / solana-v3, platform-tools v1.57,
`map[uint64]uint64` presized with a hint so no operation grows it). CU per
operation is the difference between 256 operations and none, divided by 256;
it includes the probe loop and key computation, which cost 8 CU per iteration
without a map:

| Operation | 8 keys | 64 keys | 384 keys (load 3/4) |
| --- | --- | --- | --- |
| Insert a new key | 68.8 / 69.8 | 71.2 / 72.2 | 62.6 / 63.6 |
| Lookup, key present | 57.0 / 57.0 | 60.1 / 60.1 | 60.4 / 60.4 |
| Lookup, key absent | 50.7 / 50.7 | 55.0 / 55.0 | 94.1 / 94.1 |
| `delete`, then reinsert the key | 149.9 / 150.9 | 187.3 / 188.3 | 309.2 / 310.2 |

A present-key lookup with string keys of 1 to 8 bytes (built before the loop)
costs 116.5 / 114.5 CU with 8 keys and 196.1 / 194.1 CU with 48 keys: the key
is hashed bytewise and compared on every hash match. A growth costs one pass
over the old slots. Long probe runs are the main variable: in a table just
below 3/4 full, misses and deletes (which shift the run back) cost more, as
the 384-key column shows.

## Traps

Maps add two aborts to the [failure contract](ERROR_CONTRACT.md):

- **Assignment to an entry in a nil map**: `m[k] = v`, `m[k]++`, `m[k]--` or
  `m[k] op= v` on a nil map, after the operands are evaluated (in a tuple
  assignment, after the stores to its left).
- **Unhashable map key**: hashing an interface key (or an interface inside a
  struct or array key) whose dynamic type is not comparable, in any operation,
  where Go panics `hash of unhashable type`.

`make` with a negative or oversized hint does not trap (Go treats it as 0). An
insertion whose growth does not fit the heap uses the existing out-of-memory
trap.

## Limits and rejections

- Range over maps, as above.
- `clear` of a slice: `clear supports only maps`.
- Keys and elements must not hold zero-length arrays or empty structs unless
  they have size zero: `map keys and values must not contain zero-length arrays
  or empty structs` (their Go and C sizes could differ). `struct{}` itself and
  `[0]T` are fine.
- Maps of `solana.Context`: `maps of Context unsupported`.
- Go errors stay Go errors: `&m[k]`, `m[k].F = v`, `m1 == m2`, `cap(m)`,
  uncomparable key types.
- Maps are not part of schema-2 wire layouts: `wire field Name: map[K]V is a
  map; wire layouts are fixed-size, use a [N] array of key/value structs`
  ([schema 2](SCHEMA2.md)). Handlers may use maps internally.

## Backends and reports

`CheckedProgram.Maps` lists every map type the program uses, sorted by key,
with the key and element sizes, the slot size and whether hashing a key can
abort (`KeyInterfaces`); see [the backend interface](BACKEND_INTERFACE.md#maps).
A backend may use any table layout and hash: only the results above, the two
traps and the heap accounting (which depends only on the slot size and the
live-key counts) are observable. `gosvm features` reports `map_values`,
`map_literals`, `map_makes`, `map_lookups`, `map_comma_ok_lookups`,
`map_assignments`, `map_deletes` and `map_clears`, and `uses.maps`
([feature report](FEATURES_REPORT.md)).

## Evidence

- [`examples/map-values`](../examples/map-values/program.go) is an
  ordinary-Go oracle: random operation scripts (insert, overwrite, lookup,
  comma-ok lookup, delete, `++`, `len`, `clear`) over 20 key kinds (all ten
  integer types, `bool`, strings including `""`, a named string, concatenated
  string keys, byte and string arrays, a struct with string and array fields,
  pointers, mixed interface keys with `nil`, named interface keys with value
  and pointer dynamic types, a struct with an interface field) and 12 element
  kinds (structs with slices, slices, strings, interfaces, arrays, pointers
  mutated through, nested maps, `bool`, `struct{}`, appends, `+=` and `++`/`--`
  with wrapping, nested `m[a][b]` updates); aliasing through assignment,
  fields, value receivers, slices, arrays, pointers and interfaces; nil maps
  (reads, `len`, `delete`, `clear`, and six writes that panic); unhashable
  interface keys in every operation (including nil and empty maps) and keys
  that are hashable; generic sets, counters and helpers over named and unnamed
  map types; large maps that grow; Go's evaluation order. Every observation is
  independent of iteration order. `TestMapDifferential` compares 5,670 vectors
  (787 Go panics, each an abort at the same point) natively and as host-compiled
  C with the default 32 KiB heap, and 37 large-map vectors (up to 3,072 keys
  and 19,200-operation scripts) with a 256 KiB heap.
- `TestMapHeap`: 19 exact out-of-memory boundaries against the accounting
  above.
- `TestMapSBF` ([results](../results/compiler/2026-10-14-maps/README.md))
  runs 318 cases on fogo-v0 and solana-v3 through the pinned runners: 270
  native-Go vectors (37 aborts with rollback), 18 heap-boundary cases (9
  out-of-memory aborts with rollback) and 30 compute probes.
- `TestMapAcceptedForms`, `TestMapRejections` (exact messages, including
  range in generic instances), `TestCheckedProgramMaps`, `TestMapHeapMoves`,
  `TestMapRuntimeOnlyWhenUsed`, `TestMapDocumentationExample`,
  `TestSchema2RejectsMaps` and `TestMapCounters` cover forms, diagnostics, the
  backend view, escape analysis, determinism and reports.
- Programs without maps emit byte-identical C: the frozen corpus matches
  except for its one map program (`var m map[uint64]uint64` now compiles), and
  the full-swap ELFs are unchanged (fogo-v0 `4697a9f5…`, solana-v3
  `2b5f21d9…`).

The local runners are not a public network; see
[target profiles](TARGET_PROFILES.md).
