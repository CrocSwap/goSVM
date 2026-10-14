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
3. requires bodies and rejects recursion;
4. runs the borrow checker (no reference may outlive its storage; locals whose
   references escape are moved to the heap);
5. applies every declaration, type, statement and expression rule of the
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
| `Functions` | Every function and method in declaration order, each with `Decl`, direct program `Callees`, a `Library` binding if the body is native, its `Borrows`, and the locals it moves to the heap (`HeapVars`, `HeapLiterals`) |
| `CallOrder` | The same functions, callees before callers (acyclic) |
| `Libraries` | Pure native library bindings in the graph |

`BorrowSummary.Results[i]` gives the argument slots a returned pointer or slice
may alias. Slot 0 is the receiver for methods. `SDK` is true if the result may
alias SDK-owned storage such as account data; `Heap` is true if it may refer to
the invocation heap (an allocation, a moved local, or a reference loaded from a
field or element). A result never aliases the callee's own stack locals.
`BorrowSummary.Escapes` lists the argument slots whose referenced storage the
function may store into heap data; a caller that passes its own local there
moves that local to the heap.

### Allocation

Heap allocation ([GO_ALLOCATION.md](GO_ALLOCATION.md)) adds no IR of its own:
`new`, `make`, `append` and `copy` are `*types.Builtin` calls, slice literals
(and elided `&T{...}` elements, whose `types.TypeAndValue` type is `*T`) are
`*ast.CompositeLit`s, and slices of any admitted element are `*types.Slice`
values. Check decides heap placement: `Function.HeapVars` are local variables,
value parameters and receivers whose storage must be allocated on the heap (at
the declaration, each time it executes, or at function entry for parameters and
receivers), and `Function.HeapLiterals` are the `&T{...}` literals to allocate.
Every other local stays on the stack and is never referenced from heap data. A
backend must reproduce the allocator (an 8-byte in-use counter at the heap start,
8-byte rounding and alignment, abort when `Program.HeapBytes` (0: 32 KiB) is
exhausted), Go 1.22's `growslice` capacities, and the `makeslice` range checks.
`Program.HeapBytes` comes from `gosvm.json` `heap_bytes` or the CLI's
`-heap-bytes`.

Check does not lower SDK calls. Calls into `gosvm/solana` and the `gosvm/sdk/*`
packages are ordinary typed calls. The C backend maps the SDK intrinsics onto its
runtime templates (`*.c.txt`). Another backend needs its own lowering for them.

`math/bits` calls look the same: they are standard-library calls in the typed
program (`types.Func` with `Pkg().Path() == "math/bits"`), are not listed in
`Function.Callees`, and have no Go body in the checked program. Check has
already certified that each is a supported function and that `RotateLeft*`'s
count is a constant (its `types.TypeAndValue.Value`). The C backend lowers them
to `bits.c.txt`; another backend needs its own lowering, with the results and
traps in [GO_BITS.md](GO_BITS.md). `UintSize` is an untyped constant (64).

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
verdict changed on purpose since (range loops and `uint16` became supported; the
array-element message now names `uint16`) are listed with their new expected
outcome in the test's `corpusChanges` table. `CheckedProgram` did not change shape
for these additions; a backend must lower `*ast.RangeStmt` as described in
[GO_CONTROL.md](GO_CONTROL.md#range-loops) and `uint16` as wrapping 16-bit
arithmetic. Allocation turned 32 more recorded rejections (escaping locals,
`copy`, `make`, slice literals, `[]uint64`, non-byte views and pointer/slice
fields) into accepted programs, listed in the same table; it added the
`HeapVars`, `HeapLiterals`, `Escapes` and `Heap` fields above, which are empty
or false for every program that compiled before. Separately, the full-swap ELFs built with
`gosvm build -no-cache` were identical before and after the split: fogo-v0
`4697a9f5…`, solana-v3 `2b5f21d9…`, and target-less v1.51 `4e96d2bb…`.
