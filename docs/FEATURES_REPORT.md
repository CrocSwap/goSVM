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
  `char_literals`, `string_literals`, excluding import paths) and allocation
  builtins (`new_calls`, `make_calls`, `append_calls`, `append_spreads` for
  `append(s, t...)`, `copy_calls`; added October 14, 2026, additive, schema
  unchanged; they also count in `builtin_calls` and `builtins`). An SDK method
  call counts as both `method_calls` and `sdk_calls`.
- `operators`: binary, unary (`!`, `unary-`, `unary^`, `unary+`), compound
  assignment (`+=`, …) and `++`/`--` occurrences; `&x` is `address_of`, not an
  operator. `operator_classes` groups them as `arithmetic`, `shift`,
  `bitwise`, `comparison`, `logical`, `incdec`, `other`.
- `builtins`: builtin calls by name (for example `len`).
- `integer_types`: predeclared integer type names written in source
  (`byte` and `uint8` kept distinct).
- `integer_value_types`: the underlying integer kind of every typed expression
  (`uint8` reported as `byte`; untyped constants excluded). `int` here usually
  comes from `len` and constant indices.
- `conversions`: conversion target types (fully qualified).
- `arrays`: fixed array type expressions written in source, with `type`,
  `elem`, `length`, `count`.

A `math/bits` call also counts as `cross_package_calls` in `constructs`. Each
`sdk_calls`/`library_calls`/`stdlib_calls` entry has `name` (`types.Func.FullName`, for
example `gosvm/sdk/token.Transfer` or `(*gosvm/sdk/pda.Seeds).AddKey`),
`package`, `method`, `count`, `handwritten`, `generated`, and `positions`
(`file:line`, one per call).

Evidence: [full-swap report](../results/compiler/2026-10-14-features-report/README.md).
