# Interface values on-chain

On-chain Go code can use interface values with Go's semantics: method sets,
implicit and explicit conversions, dynamic dispatch through value and pointer
receivers, type assertions, type switches and comparisons. Converting a
non-pointer value to an interface copies it to the invocation heap
([allocation](GO_ALLOCATION.md)), so the conversion costs a fixed, documented
number of heap bytes. `error` is not supported: its `Error()` method returns a
string, and strings are not in the subset.

```go
type Shape interface {
	Area() uint64
	Perimeter() uint64
}

type Rect struct{ W, H uint64 }

func (r Rect) Area() uint64      { return r.W * r.H }
func (r Rect) Perimeter() uint64 { return 2 * (r.W + r.H) }

func Total(xs []Shape) uint64 {
	t := uint64(0)
	for _, x := range xs {
		t = t + x.Area() // dynamic dispatch
	}
	return t
}

func Process(s, i []byte) uint64 {
	shapes := []Shape{Rect{2, 3}, &Rect{1, uint64(i[0])}}
	if r, ok := shapes[0].(Rect); ok {
		s[0] = byte(r.W)
	}
	return Total(shapes)
}
```

Programs that use no interface value compile to byte-identical C and ELFs: the
interface runtime is emitted only when a program has interface values.
Interfaces used only as type-parameter constraints change nothing.

## Supported

| Form | Semantics |
| --- | --- |
| Interface types | Method sets, embedded interfaces (overlapping identical methods allowed), `any` and `interface{}`, named and literal interface types, generic interface types (`Getter[T]`). Interfaces with type sets remain constraints only, as in Go |
| Conversions | Implicit (assignment, `var` with a type, call argument, return value, struct/array/slice literal element, `append` value, range assignment to an interface variable, multiple-value assignment, return and argument from a call) and explicit (`I(x)`, `any(x)`). Interface-to-interface conversions copy the two words |
| Method calls | `x.M(args)` on any interface value, including results of calls, fields, elements, dereferenced `*I` pointers and type assertions. The receiver is evaluated, then the arguments, then the call; a nil interface aborts after the arguments are evaluated, as Go panics |
| Method sets | A value of type `T` in an interface has `T`'s value-receiver methods; `*T` has both. A value-receiver method called through a `*T` gets a copy of `*p`; a nil `*T` there aborts, as Go panics (`value method called using nil pointer`) |
| Type assertions | `x.(T)` and `x.(I)`; the comma-ok forms `v, ok := x.(T)`, `v, ok = ...` and `var v, ok = ...` (v is `T`'s zero value when ok is false). A failed single-result assertion aborts |
| Type switches | `switch x.(type)` and `switch v := x.(type)`, with an optional init statement; several types per case, `case nil`, interface cases, `default` in any position (it runs only when no case matches). The bound variable has the case's type when the case lists exactly one non-interface type, and the guard's interface type otherwise. `break` leaves the switch; `continue` continues the enclosing loop |
| Comparisons | `==` and `!=` between interface values, with a concrete value, and with `nil`; expression `switch` on an interface tag or with interface cases. Values compare equal when their dynamic types are identical and the values are equal. Comparing two interfaces whose (identical) dynamic type is not comparable (a slice, or a struct or array containing one) aborts, as Go panics; different dynamic types compare unequal without aborting |
| Storage | Interface variables, parameters, results, struct fields, slice elements, array elements (`[4]Shape`, with `==` comparing elements in order), pointers to interfaces (`*any`, `&x`, `&s.Field`, `&a[i]`), `new(any)`, `make([]any, n)` |
| Generics | Instances of generic types are dynamic types like any other, and each instance is distinct (`Box[int8]` and `Box[uint8]` are different types; `Box[byte]` and `Box[uint8]` are the same). Methods of a generic type that an interface needs are instantiated even when nothing calls them directly. Interfaces are valid type arguments (`Box[Shape]`, `Same[any]`); generic functions take and return interface values; `any(v)` inside a generic function converts the instance's concrete type; `comparable` admits interface types |
| Signed values | Every [integer type](GO_INTEGERS.md) can be boxed, asserted, compared and switched on; untyped constants convert with Go's default types (`var a any = -1` holds an `int`) |

Values that can be stored in an interface are exactly the subset's storable
values: integers, `bool`, named scalars, value structs, scalar arrays and arrays
of interfaces, pointers, slices and interface values. A pointer to a local
stored in an interface moves that local to the heap
([escape analysis](#escape-analysis)).

## Representation

An interface value is two 64-bit words, `{type, data}` (C:
`typedef struct { u64 type; u8 *data; } gosvm_iface;`):

- `type` is the dynamic type's ID; 0 is the nil interface. IDs are 1-based and
  assigned per program in the sorted order of the dynamic types' canonical keys
  (`typeKey`: package-path-qualified, `byte` spelled `uint8`, interfaces spelled
  by method set), so they depend only on the set of dynamic types, never on
  declaration or discovery order.
- `data` is the value itself for pointer types (`*T`; a typed nil pointer is a
  non-nil interface with `data == 0`), and otherwise points at the boxed copy.

The dynamic types of a program are every type converted to an interface, every
concrete type a type assertion or a type switch case names, and every concrete
operand of a comparison with an interface value. They are static: no runtime
type information, hashing or allocation is needed to identify them.

### Boxing cost

Converting a value of type `T` to an interface:

| `T` | Heap bytes | Data word |
| --- | --- | --- |
| pointer type `*E` | 0 | the pointer |
| zero-size type (empty struct, `[0]E`, structs of those) | 0 | the heap base address (never read) |
| any other storable type | its Go size rounded up to 8 (`(size+7)&^7`) | a new allocation holding a copy |

Examples: `uint8`, `int16`, `uint32`, `uint64`, `bool`, `int` and `[5]byte`
cost 8 bytes; `[3]uint64` 24; a `struct{ W, H uint64 }` 16; a `[]byte` (its
24-byte header; the elements are shared, not copied) 24; an interface-to-
interface conversion, a comparison with a concrete value, an assertion, a type
switch and a method call allocate nothing. Each conversion allocates afresh,
including conversions of constants. Allocation follows the
[allocator contract](GO_ALLOCATION.md#allocator): with the default 32 KiB heap,
4,095 conversions of a `uint64` fit and the 4,096th aborts; 1,365 of a
`[3]uint64` fit and the 1,366th aborts.

Types that contain a zero-length array or an empty struct but have a nonzero
size cannot be converted (their Go and C layouts can differ), as for slice
elements.

### Copy semantics

Boxing copies the value: later changes to the original variable are not visible
through the interface, and a value-receiver method called through the interface
works on a copy of the box (the box itself is never modified). Copying an
interface value copies the two words, so both copies refer to the same box,
which is immutable. A pointer stored in an interface aliases its target, so
changes through the pointer are visible. A boxed slice shares its elements, as
in Go.

### Dispatch

Each distinct interface method that the program calls dynamically, or that an
asserted interface requires, is a *method slot* (name, package for unexported
names, signature). For each called slot the compiler emits one dispatcher that
compares the type ID with each dynamic type implementing the slot, in ID order,
and calls that type's method:

```c
static u64 gosvm_call0(gosvm_iface x) { /* Area() (uint64) */
if (x.type == 1ULL) return v11(*(v4 *)gosvm_nonnull(x.data)); /* *Rect, value receiver */
if (x.type == 3ULL) return v11(*(v4 *)x.data);                 /* Rect */
abort();                                                       /* nil interface */
}
```

There are no function pointers, method tables in data or jump tables: dispatch
is plain compare-and-branch code, which needs no data relocations on either
target. The cost of a dynamic call grows with the number of dynamic types that
implement the method; LLVM inlines small dispatchers and can hoist the type
test out of loops when the interface value is loop-invariant. Assertions to an
interface type test the ID against the statically known implementers
(`gosvm_implN`); equality calls a per-type equality function.

## Escape analysis

An interface value can be copied anywhere, so converting a pointer or slice to
an interface is an escape, like storing it into heap data
([escape rules](GO_ALLOCATION.md#escape-rules)): a local whose address (or a
local array whose view) is converted moves to the heap, and a parameter's
storage becomes the caller's obligation (`BorrowSummary.Escapes`). Pointers and
slices obtained from an interface (by assertion or as a type switch variable)
are heap or SDK storage. A dynamic call may run any implementation, so every
pointer or slice argument escapes and a pointer or slice result is heap or SDK
storage. Converting a non-reference value never moves anything: the box is a
copy.

## Traps

Interfaces add three aborts to the [failure contract](ERROR_CONTRACT.md); each
is where Go panics:

- **Failed type assertion**: a single-result `x.(T)` or `x.(I)` whose dynamic
  type is not `T` or does not implement `I`, including a nil `x`.
- **Uncomparable comparison**: `==`, `!=` or a `switch` comparison of two
  interface values (or an interface and an array or struct containing one)
  whose identical dynamic type is not comparable.
- **Nil interface method call**: calling a method on a nil interface value
  (Go's nil pointer dereference), after the receiver and the arguments are
  evaluated.

A value-receiver method called through a nil `*T` in an interface uses the
existing nil-pointer trap. Boxing that does not fit the heap uses the existing
out-of-memory trap.

## Limits and rejections

- `error` values are rejected wherever a value of type `error` appears:
  `error values unsupported; error's Error() returns a string, and strings are
  not in the on-chain subset`.
- An interface used as a value must have storable method parameters and
  results: `interface method String: unsupported type string`; variadic
  methods: `interface method M: variadics are unsupported`.
- `solana.Context` cannot be converted: `Context values cannot be converted to
  interfaces`.
- `interface values must not hold T: it contains zero-length arrays or empty
  structs` (see above).
- Method values (`f := x.M`) and method expressions (`I.M`, `I.M(x)`) of
  interface types are rejected (`method values of interface types
  unsupported; use a function literal that calls the method`); method values
  of concrete types are supported ([function values](GO_FUNCTIONS.md)). Maps
  (and so interfaces as map keys) and embedding an interface in a struct
  (struct embedding is rejected) remain unsupported.
- [Function values](GO_FUNCTIONS.md) can be stored in interfaces (8-byte
  box), asserted and switched on; function types are not comparable, so
  comparing two interfaces holding the same function type aborts.
- Recursion is rejected through dynamic calls too: a dynamic call may reach any
  method that fills its slot, so a cycle through one is `recursion is not
  supported`.
- Interface values are not part of schema-2 wire layouts.

## Backends and reports

`CheckedProgram` lists the conversions (`Conversions`), the dynamic types with
their IDs, sizes, comparability and method tables (`DynamicTypes`), the method
slots (`MethodSlots`), each dynamic call's slot (`DynamicCalls`), and the type
assertions and switches (`TypeAssertions`, `TypeSwitches`);
`Implementers(I)` lists the dynamic types an interface can hold. See
[the backend interface](BACKEND_INTERFACE.md#interfaces). `gosvm features`
reports `interface_conversions`, `dynamic_calls`, `type_assertions` and
`type_switch` ([feature report](FEATURES_REPORT.md)).

## Evidence

- [`examples/iface-values`](../examples/iface-values/program.go) is an
  ordinary-Go oracle: dispatch through value and pointer receivers, embedded
  interfaces and multiple results; boxing copy semantics and aliasing; single
  and comma-ok assertions to 12 dynamic types (including a typed nil pointer, a
  slice, a struct with a slice, an array, an empty struct and a generic
  instance); type switches with nil, multi-type, interface and generic cases,
  shadowing, heap-moved clause variables, `break` and `continue`; comparisons
  including uncomparable types, typed nil pointers, arrays of interfaces and a
  struct with an interface field; nil-interface calls after argument side
  effects; interfaces in fields, slices, arrays and behind pointers; generic
  dynamic types and generic functions over interfaces; and a boxing loop.
  `TestInterfaceDifferential` compares 14,324 vectors (3,195 Go panics, each an
  abort at the same point) natively and as host-compiled C with the default
  32 KiB heap.
- `TestInterfaceBoxingHeap`: the exact out-of-memory point for each boxing
  kind (4,095 `uint64`, `int8` or `Sq` conversions; 1,365 `[3]uint64`; no limit
  for pointers and empty structs), against computed expectations.
- `TestInterfaceSBF` ([results](../results/compiler/2026-10-14-interfaces/README.md))
  runs 530 cases on fogo-v0 and solana-v3 through the pinned runners: 520
  native-Go vectors (180 aborts with rollback) and 10 boxing boundaries (6 fit,
  4 run out of memory and roll back), and measures dispatch: a dynamic call
  over an `[]I` costs about
  5 CU more than the same direct call over `[]T` (43.15 vs 38.14 CU per
  iteration, on both targets).
- `TestInterfaceAcceptedForms`, `TestInterfaceRejections` (exact messages),
  `TestCheckedProgramInterfaces`, `TestInterfaceHeapMoves`,
  `TestInterfaceCrossPackage` (package-qualified unexported methods) and
  `TestInterfaceRuntimeOnlyWhenUsed` cover forms, diagnostics, the backend view
  and determinism.
- Programs without interface values emit byte-identical C: the frozen corpus
  (`TestCorpusMatchesPreCheckCompiler`) matches except for three recorded
  interface programs (two now compile; `error` keeps its position with the new
  message), and the full-swap ELFs are unchanged (fogo-v0 `4697a9f5…`,
  solana-v3 `2b5f21d9…`).

The local runners are not a public network; see
[target profiles](TARGET_PROFILES.md).
