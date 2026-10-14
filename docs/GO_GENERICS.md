# Generics on-chain

On-chain packages may declare and use Go 1.22 generics. Generic code is
monomorphized at compile time: every distinct instantiation becomes its own
ordinary function or struct, checked by the same subset rules, borrow checker and
recursion ban as hand-written code, and lowered to its own C function. There is
no runtime dictionary, boxing or dynamic dispatch, so an instance costs exactly
what a hand-written copy for its type arguments would.

```go
type Unsigned interface{ ~uint8 | ~uint16 | ~uint32 | ~uint64 }

func Min[T Unsigned](a, b T) T {
	if a < b {
		return a
	}
	return b
}

// A fixed-capacity stack: the storage is an array of T.
type Stack[T Unsigned] struct {
	items [4]T
	n     uint64
}

func (s *Stack[T]) Push(v T) { s.items[s.n] = v; s.n++ }

func Process(s, i []byte) uint64 {
	var st Stack[uint16]
	st.Push(Min(uint16(s[0]), 300)) // Min[uint16], Stack[uint16].Push
	return uint64(Min[uint8](s[1], s[2]))
}
```

## Supported

- Type parameters on functions and on named types: structs, arrays
  (`type Vec[T Wide] [4]T`) and other named types whose instances satisfy the
  usual type rules.
- Methods on generic types, with value or pointer receivers, called directly or
  through a pointer to an instance.
- Explicit (`Min[uint8](a, b)`, `pkg.Min[uint8]`) and inferred (`Min(a, b)`)
  instantiation, with any number of type parameters.
- Constraints: interfaces with type sets (`~uint64 | ~uint32`, unions),
  `comparable`, `any`, and method constraints. Operations on type-parameter
  values are exactly those go/types permits for the type set (arithmetic,
  shifts, ordering, `==`, conversions between type parameters, untyped
  constants representable in every member). A call `x.M()` on a type parameter
  becomes a direct call to the type argument's method.
- Generic types as struct fields, behind pointers, as type arguments
  (`Box[Pair[uint8, Box[uint64]]]`) and as results of generic methods
  (`func (p Pair[K, V]) Flip() Pair[V, K]`).
- Generic calls inside generic code, including across packages: a library
  package's `Stack[T]` can be instantiated with the importing package's types.
- Function values in generic code ([function values](GO_FUNCTIONS.md)):
  generic functions take and return function values
  (`Map[T, U any](xs []T, f func(T) U) []U`), generic instances and methods of
  generic types are values (`Max[int8]`, inferred `var f func(int64, int64)
  int64 = Max`, `b.Get` on a `Box[int16]`, `Box[uint8].Get`), function types
  are type arguments, and each instance has its own closures. Recursion
  through a value of an instance's own signature is rejected like any other.
- Allocation in generic code: `make([]T, n)`, `append` on `[]T`, `new(T)`,
  `[]T{...}`, and generic structs with pointer or slice fields. Escape analysis
  runs per instance, so each instance moves exactly its own escaping locals to
  the heap ([allocation](GO_ALLOCATION.md#with-generics-and-signed-integers)).

Every type argument must be a type the subset can store: an integer scalar
(signed or unsigned), `bool`, a [string](GO_STRINGS.md) or named string, a
named scalar, a value struct (including one with pointer, slice, string or
interface fields), a scalar array of at most 1,024 bytes, a slice of an
admitted element, a pointer to an admitted element, or an interface type
(including `error`). Each instance's body must
then satisfy every subset rule with the arguments substituted; for example
`Eq[T comparable]` instantiated with a struct is rejected because struct
comparison is unsupported, while `Eq[[4]byte]` is accepted.

## Interfaces

Interfaces are constraints and, since interface values became supported
([GO_INTERFACES.md](GO_INTERFACES.md)), value types too: interfaces are valid
type arguments (`Box[Shape]`, `Same[any]`), generic functions take and return
interface values, `any(v)` inside an instance converts the instance's concrete
type, and generic interfaces (`Getter[T]`) can be instantiated as value types.
Each instance of a generic type is its own dynamic type (`Box[int8]` and
`Box[uint8]` are different, `Box[byte]` and `Box[uint8]` the same), and the
methods of an instance that an interface needs are instantiated even when only
dynamic calls reach them. Interfaces with type sets (`~uint64 | ~uint32`)
remain constraints only, as in Go.

## Lowering

Check discovers instantiations from the non-generic code, in file and source
order, then from each new instance body in discovery order. For each distinct
instantiation it builds a synthetic declaration: a deep copy of the generic
declaration without its type parameter list, with every expression type,
declared object, field, method and scope substituted and recorded in the shared
`types.Info`. The copy replaces its generic declaration in the program, so the
rest of the compiler sees only non-generic code. Instances of one declaration
appear at its position, sorted by key. Uninstantiated generic declarations emit
nothing; they are type-checked by Go but not certified by the subset rules,
because they have no code.

Instances are keyed by the generic declaration and the type arguments, written
with full package paths:

| Instance | Key | C symbol |
| --- | --- | --- |
| function | `example.org/app.Min[uint64]` | `gosvm_fn_example_Dorg_Sapp_DMin_Luint64_R` |
| method | `example.org/app.Pair[uint8, uint16].Flip` | `gosvm_fn_..._DPair_Luint8_C_Wuint16_R_DFlip` |
| type | `example.org/app.Stack[uint16]` | `gosvm_ty_example_Dorg_Sapp_DStack_Luint16_R` |

The C name is `gosvm_fn_` or `gosvm_ty_` and the key, encoded injectively:
letters and digits stay, `_` becomes `__`, `.` `_D`, `/` `_S`, `[` `_L`, `]` `_R`,
`,` `_C`, `*` `_P`, space `_W`, and any other byte `_xHH`. `byte` and `uint8`
(`rune` and `int32`) are one type and give one key: Go's `Box[byte]` and
`Box[uint8]` are one C struct and share every method instance. An unnamed
interface type argument is spelled by its method set (`any` and `interface{}`
are both `interface{}`: `Box[interface{}]`). Fields of an
instantiated struct keep the names of the declaration's fields.

Naming guarantee: a key and its C symbol depend only on the generic
declaration's package path and name and on the type arguments. They do not
depend on declaration order, file order, discovery order or map iteration, and
rebuilding a program emits identical C. Reordering declarations keeps every
instance and type-instance symbol; as for all code, local temporaries
(`vN`, `tN`) are numbered in emission order and may change.

## Limits and rejections

- **Instantiation limit.** A generic function, a method of a generic type and a
  generic type may each have at most `MaxInstantiations` (64) distinct
  instances, to keep code size predictable. The 65th is rejected at its site:
  `too many instantiations of Id (limit 64)` (methods: `B.M`).
- **Type arguments.** A disallowed type argument is rejected at the
  instantiation site, naming it:
  `g.go:3:40: instantiation Id[complex128]: type argument complex128 for T: unsupported type complex128`.
  [Strings](GO_STRINGS.md) and `error` are valid type arguments.
  An untyped constant argument infers `int`, which is rejected as storage
  (`Id[int]`); convert the argument (`Id(uint64(1))`).
- **Instance bodies.** A subset or borrow violation that occurs only for some
  type arguments is reported at the instantiation site, followed by the
  position in the generic body:
  `g.go:4:39: instantiation Eq[P]: g.go:3:45: struct comparisons unsupported; compare fields explicitly`.
- **Recursion.** Calls are followed to generic declarations, so a generic
  function calling itself with any type arguments, or generic functions calling
  each other, is `recursion is not supported`. Cycles that appear only after
  instantiation, through a method constraint, are rejected the same way.
- **Constraint violations** are Go type errors, reported as go/types writes
  them (`string does not satisfy Unsigned (string missing in ~uint8 | ~uint16 | ~uint32 | ~uint64)`).
- **Borrows** are summarized per instance: `Pick[[]byte]`'s result may alias its
  argument while `Pick[uint8]`'s aliases nothing.

## Not supported

- Function values and method values of generic functions and methods
  (`f := Min[uint64]`), as for all functions.
- Generic types in schema-2 wire layouts. An uninstantiated layout type is a Go
  error in the generated adapter (`cannot use generic type model.Boxed[T any]
  without instantiation`); a field of an instantiated generic type is
  `wire field Inner has generic type ...; generic types are unsupported in wire layouts`.
- Everything the subset rejects for ordinary code (arrays of structs or
  pointers, pointers to slices, Context fields, slices of zero-size elements)
  is rejected in instances too.

No trap or error code was added; instances fail exactly as the equivalent
hand-written code does ([error contract](ERROR_CONTRACT.md)).

## Backends and reports

`CheckedProgram` lists each instance in `Functions` (with `Function.Instance`)
and in `Instances`, instantiated types in `TypeInstances`, and the instance
each generic call runs in `Instantiated`; see
[the backend interface](BACKEND_INTERFACE.md#generics). `gosvm features` reports
generic declarations and their instantiations (`generics`) and the
`generic_functions`, `generic_methods`, `generic_types`, `type_parameters`,
`explicit_instantiations`, `inferred_instantiations` and `type_instantiations`
counters ([feature report](FEATURES_REPORT.md)).

## Evidence

- [`examples/generic-values`](../examples/generic-values/program.go) is an
  ordinary-Go oracle with 56 function and method instances and 9 type
  instances: Min/Max/Clamp and sums over arrays and byte views at every unsigned
  width, division and shifts, a fixed-capacity `Stack[T]`, a two-parameter
  `Pair[K, V]`, a generic named array with a method, generic struct fields,
  pointers to instances, `comparable`, `any` and a method constraint.
  `TestGenericDifferential` runs 4,672 vectors natively and as host-compiled C
  and compares results, panics and state; `TestGenericSBF` runs 705 of them
  (496 custom errors) as SBF programs for fogo-v0 and solana-v3.
- `TestGenericMatchesHandwrittenSBF` builds generic helpers instantiated for
  two widths and a hand-written per-type copy: their `.text` sections are
  byte-identical (800 bytes) on both targets, so size and compute units match.
- `TestGenericAcceptedForms`, `TestGenericRejections` (exact messages),
  `TestGenericCrossPackage`, `TestCheckedProgramInstances`,
  `TestGenericAliasSpellingsShareInstances`, `TestGenericNamesAreDeterministic`
  and `TestMangleIsInjective` cover the forms, diagnostics, backend view and
  naming.
- Programs without generics emit byte-identical C: the frozen corpus
  (`TestCorpusMatchesPreCheckCompiler`) still matches except for the two
  recorded generic programs, now accepted, and three interface programs whose
  outcome interface values changed later. The full-swap ELFs are unchanged (fogo-v0
  `4697a9f5…`, solana-v3 `2b5f21d9…`).
