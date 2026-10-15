# Language and SDK feature report

`gosvm features` writes a JSON report of which Go constructs and which SDK calls
a program uses. It exists to size downstream IRs (for example a verified
backend). It describes the source; it does not certify that the program is in
the supported on-chain subset — `gosvm check` and the compiler still decide that.

```sh
gosvm features [-dir project] [-generate] [-o out.json]   # project form
gosvm features [-o out.json] <package-directory|file.go>  # low-level form
```

The project form finds the project like other commands, reads `gosvm.json` for
the module and target profiles, and reports the package as it is on disk,
including the existing generated adapter (`zz_gosvm.go`). `-generate` runs
`gosvm generate` first. The low-level form type-checks a package directly and
takes the module from the nearest `go.mod`; it reports no targets. Output goes
to stdout unless `-o` is given. The Go API is
`features.Report(*compiler.TypedProgram) (*features.Usage, error)` in
`internal/features`, with `features.Marshal` for the canonical bytes.

## Schema `gosvm-features-v1`

Output is deterministic: arrays are sorted, maps are emitted with sorted keys,
paths are relative to the common directory of the reported files, and every
construct counter is present even when zero.

| Field | Meaning |
| --- | --- |
| `schema` | `"gosvm-features-v1"` |
| `program.module`, `program.entry_package` | Go module and the checked entry package path |
| `program.targets` | `{network, profile}` per configured target (project form only) |
| `program.packages` | non-SDK packages in the import graph |
| `program.sdk_packages` | `gosvm/solana` and `gosvm/sdk/...` packages imported |
| `files[]` | `path`, `package`, `kind` (`handwritten`, `generated`, `library_stub`), `sha256`, `lines` |
| `lines` | total lines, `handwritten` vs `generated` |
| `language.handwritten`, `language.generated` | per-kind usage (below) |
| `sdk_calls[]` | calls to `gosvm/solana` or `gosvm/sdk/...` functions and methods |
| `library_calls[]` | calls into generated Bend/Rust library bindings |
| `stdlib_calls[]` | calls into `math/bits`, the one standard-library package available on-chain (added October 14, 2026; additive, schema unchanged) |
| `generics[]` | every generic function, method and type declared in program code: `name` (`pkg/path.F`, `pkg/path.T.M`, `pkg/path.T`), `package`, `kind` (`function`, `method`, `type`), `count` and the sorted canonical `instances` the compiler would monomorphize (added October 14, 2026; additive, schema unchanged; see [generics](GO_GENERICS.md)) |
| `uses` | per-program feature summary (added October 14, 2026; additive): booleans `allocation`, `interfaces`, `signed_integers`, `math_bits`, `generics`, `uint16`, `range`, `strings` (added with [strings](GO_STRINGS.md); additive), `maps` (added with [maps](GO_MAPS.md); additive: a non-nil map value, a map literal or `make` of a map; a map literal also sets `allocation`); `checked` and `check_error` from `compiler.Check`; `heap_moved_locals` and `heap_moved_literals`, the escaping locals and `&T{}` literals Check moves to the heap. Derived from the counters (including `signed_operations`, `interface_values` and `string_values`; programs with interface values or strings, including `error` values, pass Check, so `checked` is true for them unless another rule rejects them): `int` values used only as `len`/`cap`/count results do not make `signed_integers` true, and heap moves make `allocation` true. Interface conversions alone do not set `allocation`, but boxing allocates, so a backend that supports `interfaces` needs the allocator too ([interfaces](GO_INTERFACES.md#boxing-cost)); likewise string concatenations and conversions allocate without setting `allocation` ([string allocation](GO_STRINGS.md#allocation)). String constants folded into constant expressions do not set `strings`. Lets a backend that supports a subset reject a program with a named reason. Also (added October 14, 2026; additive): `closures` (function literals, function values, method values or expressions, or calls through function values; separate from `interfaces`), `defer` (`defer` statements), `struct_equality` (`==`/`!=` on structs or a
struct `switch` tag; [struct equality](GO_TYPES.md#struct-equality)),
`type_aliases` (alias declarations; [aliases](GO_TYPES.md#type-aliases)),
`composite_arrays` (array types written with struct, array, pointer or slice
elements; [arrays](GO_ARRAYS.md); arrays of a type parameter instantiated with
a struct are not counted), `panic` (calls of the `panic` builtin;
[panic](GO_TYPES.md#panic), which never sets `interfaces` or `allocation`),
and `recursion_check_inconclusive` with `recursion_check_reasons` (omitted when empty): set when a call through a function value has possible targets that are not resolved to a set whose call graph is acyclic. Check resolves every such call to all targets of its signature (string-, `error`- and map-typed signatures included) and rejects any cycle, so every possibly-recursive program is rejected outright and the flag is always false ([recursion](GO_FUNCTIONS.md#recursion)). Closures move captured variables to the heap (`heap_moved_locals`), so a program with captures also has `allocation` |

A file is `generated` when it has the standard `// Code generated ... DO NOT
EDIT.` line before its package clause. SDK package sources are not counted as
program code; tests (`_test.go`) are not part of the on-chain package and are
excluded. Library stub files are listed with no hash and are not counted.

Each `language` side has:

- `files`, `lines`, `max_loop_nesting`, `imports` (sorted paths).
- `constructs`: counts for declarations (`functions`, `methods_declared`,
  `pointer_receivers`, `value_receivers`, `params`, `pointer_params`,
  `results`, `pointer_results`, `multi_result_functions`,
  `struct_type_decls`, `array_type_decls`, `other_type_decls`,
  `type_aliases`, `function_literals`), values (`struct_literals`,
  `array_literals`, `slice_literals`, `keyed_literals`, `array_equality`,
  `struct_equality`, `slice_expressions`, `slice_expressions_of_arrays`,
  `index_expressions`, `field_selections`, `address_of`, `pointer_derefs`,
  `*_types` written in source), calls (`function_calls` in the same package,
  `cross_package_calls`, `method_calls`, `sdk_calls`, `library_calls`,
  `builtin_calls`, `indirect_calls`, `conversions`), statements
  (`assignments`, `compound_assignments`, `multi_value_assignments`,
  `short_var_decls`, `var_decls`, `returns`, `multi_value_returns`, `if`,
  `else`, `else_if`, `switch`, `switch_tagless`, `switch_cases`,
  `type_switch`, `fallthrough`, `for_three_clause`, `for_condition_only`,
  `for_infinite`, `for_range` with `for_range_array|slice|int|other`, `break`,
  `continue`, `labels`, `goto`, `defer`, `go`, `select`), constants
  (`const_typed`, `const_untyped`), literals (`int_literals`,
  `char_literals`, `string_literals`, excluding import paths), signed
  integer operations whose lowering differs from the unsigned one
  (`signed_division`: non-constant `/` and `%` on signed operands;
  `signed_shift_counts`: shifts by a non-constant signed count, which trap
  when negative), generics (`generic_functions`, `generic_methods`
  declared on generic types, `generic_types`, `type_parameters` declared, and
  generic function names written with type arguments,
  `explicit_instantiations`, or without, `inferred_instantiations`, plus
  instantiated generic type expressions, `type_instantiations`) and
  allocation builtins (`new_calls`, `make_calls`, `append_calls`,
  `append_spreads` for `append(s, t...)`, `copy_calls`; they also count in
  `builtin_calls` and `builtins`) and interface values
  (`interface_conversions`: implicit or explicit conversions of a
  non-interface value to an interface type, including a type parameter
  converted in a generic body, but not nil or interface-to-interface
  conversions; `dynamic_calls`: method calls on interface values;
  `type_assertions`: `x.(T)` expressions, not type switch guards, which
  `type_switch` counts), strings (`string_values`: non-constant
  expressions of a string type; `string_concatenations`: non-constant string
  `+` operators and `+=` on strings; `string_conversions`: non-constant
  conversions between strings and byte or rune slices, or from integers, but
  not string-to-string; `string_ranges`: range over a string, which also
  counts as `for_range` and `for_range_other`), function values
  (`function_values`: declared functions or generic instances used as values
  rather than called; `method_values` and `method_expressions` not called;
  `named_results`: functions and literals with named results; literals are
  `function_literals`, calls through values `indirect_calls`, and `defer`
  statements `defer`) and maps (`map_values`:
  non-constant, non-nil expressions of a map type; `map_literals`: map
  composite literals; `map_makes`: `make` of a map, which also counts as
  `make_calls`; `map_lookups`: single-value `m[k]` reads; `map_comma_ok_lookups`:
  `v, ok := m[k]` reads; `map_assignments`: `m[k]` as an assignment, `++`/`--`
  or compound-assignment target; `map_deletes`: `delete` calls; `map_clears`: `clear` of a
  map; `map_types` counts map type expressions as before) and value types
  (`struct_switch_cases`: case values compared with a struct `switch` tag;
  `composite_array_types`: array type expressions whose element type is a
  struct, array, pointer or slice; `struct_equality` and `type_aliases` count
  `==`/`!=` on structs and alias declarations as before); all added within
  gosvm-features-v1 on October 14, 2026, schema unchanged. An SDK method call counts as both
  `method_calls` and `sdk_calls`. A call `F[T](x)` counts as a call of `F`,
  and `F[T]` is not an `index_expressions` entry.
- `operators`: binary, unary (`!`, `unary-`, `unary^`, `unary+`), compound
  assignment (`+=`, …) and `++`/`--` occurrences; `&x` is `address_of`, not an
  operator. `operator_classes` groups them as `arithmetic`, `shift`,
  `bitwise`, `comparison`, `logical`, `incdec`, `other`.
- `builtins`: builtin calls by name (for example `len`).
- `integer_types`: predeclared integer type names written in source
  (`byte` and `uint8` kept distinct).
- `integer_value_types`: the underlying integer kind of every typed expression
  (`uint8` reported as `byte`; untyped constants excluded), including the
  signed types, `int` and `uint`. `int` also comes from `len` and constant
  indices.
- `conversions`: conversion target types (fully qualified).
- `arrays`: fixed array type expressions written in source, with `type`,
  `elem`, `length`, `count`.

A `math/bits` call also counts as `cross_package_calls` in `constructs`. Each
`sdk_calls`/`library_calls`/`stdlib_calls` entry has `name` (`types.Func.FullName`, for
example `gosvm/sdk/token.Transfer` or `(*gosvm/sdk/pda.Seeds).AddKey`),
`package`, `method`, `count`, `handwritten`, `generated`, and `positions`
(`file:line`, one per call).

Evidence: [full-swap report](../results/compiler/2026-10-14-features-report/README.md).
