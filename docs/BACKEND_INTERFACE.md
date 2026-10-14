# Backend interface

goSVM separates the on-chain Go subset from code generation. An alternative
backend (for example Go typed AST → its own IR → SBF) consumes the same checked
values as the C/LLVM path and never re-implements the subset rules. The C/LLVM
path is unchanged: `CompileProgram` is now `Check` followed by C emission, and
the emitted C and ELFs are byte-identical to the pre-split compiler (see
[Equivalence evidence](#equivalence-evidence)).

```text
Program ──Check──▶ CheckedProgram ──emitC──▶ C ──Clang/LLD──▶ SBF ELF   (built in)
                                  └──your backend──▶ IR ──▶ SBF ELF       (alternative)
gosvm.json ──LoadSchema──▶ ResolvedSchema (layouts, instructions, PDAs, lifecycle)
```

All three APIs are in internal packages of module `gosvm`, so a backend lives in
this module or a fork of it.

## `compiler.Check`

`func Check(program *Program) (*CheckedProgram, error)` in
[`internal/compiler/check.go`](../internal/compiler/check.go). Build the
`Program` with `compiler.ReadProgram(dir)` (or `ReadProgramWithSources`). Check:

1. parses and type-checks the package graph with the pinned SDK in one
   `go/types` universe (dot/blank and illegal internal imports are rejected);
2. requires `Process([]byte, []byte) uint64` or `Process(solana.Context) uint64`;
3. requires bodies and rejects recursion (including through generic
   declarations);
4. monomorphizes generics: each instantiation becomes a synthetic non-generic
   declaration ([generics](#generics));
5. runs the borrow checker (no reference may outlive its storage);
6. applies every declaration, type, statement and expression rule of the
   subset ([`subset.go`](../internal/compiler/subset.go), with the type rules
   in [`rules.go`](../internal/compiler/rules.go)).

It returns the first violation as a `file:line:col: message` error. The order and
text are the same as `CompileProgram` has always reported. The emitter applies no
language rules. If it reaches a construct Check should have rejected, it reports
`internal error: construct was not certified by Check`.

## `compiler.CheckedProgram`

Every field is documented in `check.go`. In summary:

| Field | Meaning |
| --- | --- |
| `TypedProgram` (embedded) | `Entry` package, `Packages` by import path (SDK excluded), `Files` in dependency-first order, shared `Info` and `Fset` |
| `Program` | The input, including `Libraries` and `SDK` |
| `SDK`, `SDKPackage` | SDK API version (0/2 current, 1 legacy) and the checked `gosvm/solana` package, or nil if it is not imported |
| `Process`, `EntryKind` | Entry function; `EntryBytes` or `EntryContext` |
| `Functions` | Every function and method in declaration order, each with `Decl`, direct program `Callees`, a `Library` binding if the body is native, and its `Borrows`; a generic declaration is replaced by its instances (`Instance` set) |
| `CallOrder` | The same functions, callees before callers (acyclic) |
| `Libraries` | Pure native library bindings in the graph |
| `Instances`, `TypeInstances`, `Instantiated` | Generic instantiations ([generics](#generics)); empty without generics |

`BorrowSummary.Results[i]` gives the argument slots a returned pointer or slice
may alias. Slot 0 is the receiver for methods. `SDK` is true if the result may
alias SDK-owned storage such as account data. A result never aliases the
callee's own locals.

## Generics

Check lowers generics by monomorphization ([GO_GENERICS.md](GO_GENERICS.md)),
so a backend that lowers `Functions` needs no generic support of its own:

- `Instances` lists one `*Instance` per instantiation of a generic function or
  of a method of a generic type, in `Functions` order. `Origin` is the generic
  declaration's object, `TypeArgs` the concrete arguments (the receiver type's,
  for methods), `Object` the instance's own non-generic `*types.Func` (its name
  is `Name`, for example `Min[uint64]` or `Stack[uint8].Push`), and `Decl` a
  deep copy of the declaration without type parameters. `Key` is the
  package-path-qualified instantiation and `Symbol` the C name the built-in
  backend uses; both depend only on the declaration and the type arguments.
- Each instance is also a `Function` (with `Function.Instance` set) in place of
  its generic declaration, with its own `Callees` and its own `Borrows`. Generic
  declarations themselves are not listed and have no code.
- `Instantiated` maps every identifier that names a generic callee at a call,
  in `Files` and in instance bodies, to the instance the call runs: `F` in
  `F(x)` and in `F[T](x)` (strip the index expression), `pkg.F`'s `F`, and a
  method selector's `Sel`. `Info.Uses` still records the generic declaration
  (functions) or go/types' instantiated method there.
- `TypeInstances` lists every instantiated generic type checked code uses, by
  `Key`. Its `Type` is a `*types.Named` whose `Underlying()` is substituted.
  go/types can keep identical instances spelled with `byte` and `uint8` as
  distinct `*types.Named`; they share a key, and their fields share
  `Var.Origin()`, so key types and fields by those rather than by pointer.

Every node of an instance `Decl` has substituted `Info` entries: `Types` (with
the original value mode and constant), `Defs` (fresh parameter, result and
local objects; `Defs[Decl.Name]` is `Object`), `Uses` (mapped locals; fields and
methods looked up again in the substituted types, so a call through a method
constraint names the concrete method) and `Scopes` (a mirror of the generic
declaration's scopes rooted at the function scope). Three things differ from
go/types' own output: `Selections` for an instance's selectors hold the generic
declaration's selection (its `Kind`, `Index` and `Indirect` are right; use
`Uses` and `Types` for the substituted method or field and types), `Instances`
has no entries for instance bodies (use `Instantiated`), and type-parameter
identifiers keep their `TypeName` objects (use `Types`). Instance nodes share
the generic declaration's positions, so diagnostics point into its source.

Check does not lower SDK calls. Calls into `gosvm/solana` and the `gosvm/sdk/*`
packages are ordinary typed calls. The C backend maps the SDK intrinsics onto its
runtime templates (`*.c.txt`). Another backend needs its own lowering for them.

`math/bits` calls look the same: they are standard-library calls in the typed
program (`types.Func` with `Pkg().Path() == "math/bits"`), are not listed in
`Function.Callees`, and have no Go body in the checked program. Check has
already certified that each is a supported function. A `RotateLeft*` count is
any `int` expression; when it is constant, its `types.TypeAndValue.Value` holds
it. The `uint` variants (`bits.Add`, `bits.Len`, ...) are the 64-bit functions,
because `uint` is 64 bits. The C backend lowers them to `bits.c.txt`; another
backend needs its own lowering, with the results and traps in
[GO_BITS.md](GO_BITS.md). `UintSize` is an untyped constant (64).

Integer types are ordinary `types.Basic` kinds: every fixed-size integer, plus
`int` and `uint` (both 64 bits), and named versions. `len`/`cap` results, range
keys and `math/bits` counts are `int` values like any other. A backend must
implement Go's integer semantics (wrapping arithmetic, truncating signed
division with `MinInt / -1 == MinInt`, sign-filling `>>`, signed or unsigned
shift counts with a trap on a negative count, and sign- or zero-extending
conversions), as specified in [GO_INTEGERS.md](GO_INTEGERS.md). SBF has no
signed divide instruction, so signed division needs a lowering that does not
call `__divdi3`.

## `project.LoadSchema`

`func LoadSchema(dir string) (*ResolvedSchema, error)` in
[`internal/project/schema_load.go`](../internal/project/schema_load.go). It
loads and validates a schema-2 `gosvm.json` and resolves layouts with the same
code `gosvm generate` uses, including the `layouts.json` compatibility lock. It
writes nothing. `ResolvedSchema` holds:

- the validated `Config`;
- `States` and `Arguments`, as `WireLayout`s with discriminator, size and packed
  fields (absolute offsets and sizes, nested structs, array lengths, `GoType`);
- `Instructions`, with accounts in transaction order (kind, access, signer,
  owner, address, layout, PDA seeds, init/close), key relations and aliases;
- `Errors` (generated 6000+ codes and handler `Err*` constants);
- `ValidationOrder`;
- the `TypedProgram` the layouts were resolved in.

`State(name)` looks up a state layout by name. For schema details see
[SCHEMA2.md](SCHEMA2.md). LoadSchema certifies the declarations, not the handler
code. Run `Check` on the generated program (`gosvm check` does) for that.

## Failure semantics

Runtime traps, error codes and check order are a stable contract:
[ERROR_CONTRACT.md](ERROR_CONTRACT.md). A backend must reproduce them. The
compile-time diagnostics above are a separate matter. They come only from
`Check`, so they are identical for every backend.

## Equivalence evidence

[`internal/compiler/check_corpus_test.go`](../internal/compiler/check_corpus_test.go)
replays 312 programs recorded from the pre-split compiler (commit `dfcb8fc`) in
[`testdata/check_corpus.jsonl.gz`](../internal/compiler/testdata/check_corpus.jsonl.gz):

- every program the root test suite passed to `CompileProgram`;
- every program under `examples/` that declares `Process`, through `-emit-c`,
  and every example project through `gosvm check`;
- a supplementary table of rejections not otherwise exercised.

For each program, `Check` must reject with exactly the recorded error, or accept
when the old compiler accepted. `CompileProgram` must reproduce the recorded error
or the SHA-256 of the emitted C. The recording is never edited: programs whose
verdict changed on purpose since are listed with their new expected outcome in
the test's `corpusChanges` table. These are: range loops and `uint16` became
supported; signed integers, `int` and `uint` became supported; the two recorded
generic programs are now accepted; interface values are rejected with a message
naming the constraint rule; and the array-element, pointer-element and
struct-field messages now name the admitted types. `CheckedProgram` did not
change shape for range loops, `uint16` or signed integers. A backend must lower
`*ast.RangeStmt` as described in [GO_CONTROL.md](GO_CONTROL.md#range-loops),
`uint16` as wrapping 16-bit arithmetic, and signed integers as in
[GO_INTEGERS.md](GO_INTEGERS.md). Generics added the `Instances`,
`TypeInstances` and `Instantiated` fields and `Function.Instance`; existing
fields keep their meaning, and for a program without generics they are exactly
as before. Separately, the full-swap ELFs built with
`gosvm build -no-cache` were identical before and after the split: fogo-v0
`4697a9f5…`, solana-v3 `2b5f21d9…`, and target-less v1.51 `4e96d2bb…`.
