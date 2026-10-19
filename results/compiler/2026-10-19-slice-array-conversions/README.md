# Slice-to-array conversions (2026-10-19)

Question: do `(*[N]T)(s)` and `[N]T(s)` compile and behave as in Go
([CrocSwap/goSVM#7](https://github.com/CrocSwap/goSVM/issues/7))?

Compiler: branch `fix-slice-array-pointer` (parent `79cf25c`); host Go 1.23.2
(native oracle), platform-tools v1.57, runners 0.5.1 (fogo-v0) and 0.6.0
(solana-v3).

## Before

- `(*[8]byte)(b)` passed Check (the pointer-conversion rule added for
  `(*T)(p)` on October 16 did not look at the operand's type), passed native
  `gosvm test`, and then the built-in backend cast the C `slice` struct to a
  pointer: `error: operand of type 'slice' where arithmetic or pointer type
  is required`, for both targets.
- `[8]byte(b)` was rejected by Check:
  `array conversions require identical underlying arrays; slice conversions unsupported`.

## Change

- Check admits both forms (`slicearray.go`); the emitter reads the slice
  once, aborts when `len(s) < N`, and yields the data pointer or a copy of
  the `N` elements (`{0}` for `N == 0`, which reads nothing).
- Evaluation order: both are plain operations, as in gc. The analysis
  records the length check as a trap and `[N]T(s)`'s element reads as reads,
  so calls in the same statement are hoisted before them.
- Compile-time initialization stops at a converting initializer, which then
  runs at run time; the interpreter used to return the slice as the pointer
  value (an internal panic).
- `gosvm features` counts them as `slice_array_conversions`.

## Evidence

`go test ./internal/compiler -run TestSliceArray` (`slicearray_test.go`).
Two programs (`slice-array-pointers`, `slice-array-values`) cover
`len == N`, `len > N`, `len < N` traps, aliasing writes through the pointer
and the slice (and through account data on SBF), nil and non-nil empty
slices with `N == 0`, `len((*[N]T)(nil))` as a constant, integer, struct
and array elements, named array types, generic instances calling generic
instances (`First[T]`, `Second[T]`), a pointer into a local array that
escapes, package-level initializers, and gc's order (a call that replaces
or writes the slice runs first; a call's panic wins over the conversion's
trap).

| Run | Inputs per program | Result |
| --- | --- | --- |
| native Go vs host C | 256 | all match (pointers: 144 failures, 77 panic calls; values: 104, 26), output bytes, partial writes before a failure and panic lines |
| SBF fogo-v0 | 96 | all match native Go (pointers: 63 aborts, 37 logging the panic line; values: 44, 12), with rollback |
| SBF solana-v3 | 96 | same |

Mutation checks: dropping the evaluation-order trap/read event makes both
programs disagree with native Go (a panic line is lost); dropping the
compile-time-initialization stop panics the compiler
(`interface {} is compiler.evSlice, not compiler.evPtr`).

## No change elsewhere

- `TestCorpusMatchesPreCheckCompiler`: one recorded rejection, entry 282
  (`a:=[24]byte(s)`, the old `TestArrayRejections` case), now compiles and
  is listed in `corpusChanges`; every other entry's verdict and C are
  unchanged (58 rejections left, guard 57).
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
  `clock2`, both targets) reproduce byte for byte
  (`results/compiler/2026-10-15-byval-rebuild/metrics.json`).

## Reproduce

```sh
make test   # or: go test -ldflags=-linkmode=external ./internal/compiler -run TestSliceArray
```
