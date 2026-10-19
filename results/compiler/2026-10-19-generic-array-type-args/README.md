# Arrays inside generic type arguments (2026-10-19)

Question: do generic instances whose type arguments, or whose struct fields,
hold arrays through slices, pointers or maps compile and behave as in Go
([CrocSwap/goSVM#8](https://github.com/CrocSwap/goSVM/issues/8))?

Compiler: branch `fix-generic-array-type-args` (parent `79cf25c`); host Go
1.23.2 (native oracle), platform-tools v1.57, runners 0.5.1 (fogo-v0) and
0.6.0 (solana-v3).

## Before

Check failed with `array type was not declared before use` in two places:

1. **Type arguments.** Check validates each type argument while it
   discovers instantiations, before it declares any array type.
   `typeArgRule` bounded a top-level array (and a pointer to one) without
   looking it up, but sent everything else to `variableRule`, which looks
   arrays up. So `Pair[uint32, [32]byte]` and `map[K][4]byte` passed while
   `Pair[uint32, [][32]byte]`, `[]*[4]byte`, `**[4]byte`, `[]Key` (named
   array), `map[uint8][][4]byte`, `func([]*[4]byte)` and `Id([][4]byte{...})`
   failed.
2. **Generic struct fields.** Array types are declared where source writes
   them (`*ast.ArrayType` nodes, named types, direct array fields). A field
   `V [][3]T` of `Box[uint16]` holds `[3]uint16`, which source may write
   nowhere, so `Box[uint16]`, `PB[T]{P *[4]T}` and `Grid[T]{Rows []Row[T]}`
   failed even after (1).

## Change

- `typeArgRule` descends through pointer and slice types (keeping
  `pointerRule`'s and `sliceRule`'s rejections) and bounds the arrays it
  reaches with `arrayRule`; maps and function types already applied it to
  their components.
- Check and the emitter declare the arrays a struct field holds through
  pointer, slice, map and array types (`nestedArrays`) before the field is
  used. In programs where every array is written in source this declares
  nothing new, so their C is unchanged.

## Evidence

`go test ./internal/compiler -run TestGenericArrayTypeArgs`
(`generic_arrays_test.go`). `generic-array-type-args` is the issue's
`Pair[uint32, [][32]byte]` plus a sorted `[]Pair` lookup (the Borsh
`BTreeMap<u32, BTreeSet<[u8; 32]>>` encoding), aliasing through the found
slice, array equality, `[]*[4]byte`, `map[uint8][][2]uint16`, `[]Key`,
`[][][2]uint8`, a slice of instances holding `[][32]byte`, an inferred
`Id([][4]byte{...})` and a function-typed argument. `generic-struct-array-fields`
covers `Box[T]` with `[][3]T`, `*[4]T`, `map[uint8][][5]T` and `[2][][2]T`
fields (arrays written nowhere else), a pointer-receiver method appending
to them, and `Grid[T]{Rows []Row[T]}`. Both end in bounds or nil traps
selected by the input.

| Run | Inputs per program | Result |
| --- | --- | --- |
| native Go vs host C | 256 | all match (51 and 62 traps), including writes before a trap |
| SBF fogo-v0 | 64 | all match native Go (17 and 13 aborts, with rollback) |
| SBF solana-v3 | 64 | same |

`TestGenericArrayTypeArgsForms` compiles 15 forms (all rejected at
`79cf25c`) and host-compiles their C, and checks that the storage rules
still apply inside type arguments (`[][2000]byte` exceeds the per-value
limit, `[][0]byte` has a zero-size element, `*[]*[4]byte` is a pointer to a
slice).

## No change elsewhere

- `TestCorpusMatchesPreCheckCompiler` and the existing generics tests pass:
  the frozen corpus's C is unchanged.
- The five `examples/*` gosvm projects build to byte-identical ELFs with the
  `79cf25c` CLI and this branch's CLI (`gosvm build -no-cache -target
  fogo|solana`), and so does a fresh `gosvm new` starter:

| Project | Target | ELF SHA-256 (both) |
| --- | --- | --- |
| `escrow` | fogo | `7884712212fb2a2b…` |
| `escrow` | solana | `aedab88b8e5806f9…` |
| `full-swap` | fogo | `a66c5a97b1dea963…` |
| `full-swap` | solana | `b419245ba541da53…` |
| `multi-state` | fogo | `f1bd4fed20bda9b4…` |
| `multi-state` | solana | `d6a3357ac30dcb20…` |
| `tutorial` | fogo | `5bf3006a691f1891…` |
| `tutorial` | solana | `571ca8b36b7c4d90…` |
| `typed-swap` (and the starter) | fogo | `7284effba372dbc5…` |
| `typed-swap` (and the starter) | solana | `48669f4ec07ca02c…` |

- `scripts/verify_rebuild.py` with this branch's CLI, then
  `scripts/check_rebuild_reference.py`: the 4 reference ELFs (full swap and
  `clock2`, both targets) reproduce byte for byte.

## Reproduce

```sh
make test   # or: go test -ldflags=-linkmode=external ./internal/compiler -run TestGenericArrayTypeArgs
```
