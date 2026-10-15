# gc's evaluation order — October 15, 2026

At the user's decision goSVM now evaluates expressions exactly as gc (Go
1.22.0) does, and the order is contracted in
[ERROR_CONTRACT.md](../../../docs/ERROR_CONTRACT.md#evaluation-order) for the
Qushta verified backend. Before, goSVM read plain operands where they
appeared, left to right; gc's order pass (`cmd/compile/internal/walk/order.go`)
evaluates calls and a few other *sequenced* operations into temporaries first
and reads plain operands afterwards.

## gc's behaviour (probes)

`probe/main.go` (run with `probe/run.sh`) prints 214 cases with the pinned Go
1.22.0; the output (`probe/gc-1.22.0.txt`) is identical with default flags,
`-gcflags=-l` and `-gcflags='-N -l'`. `x` and friends are package variables
that the called function (`f`) changes.

| Construct | gc |
| --- | --- |
| `x + f()`, `-x + f()`, `x/(f()+1)`, `uint8(x) + f()` | `x` read after `f` |
| `f(x, g())`, variadic and struct/array/slice literal elements | `x` after `g` |
| `two(x, two(x, f()))` | inner `x` read when the inner call runs (after `f`), outer `x` after the inner call |
| `fv(g())` (function value), `i.M(g())` (interface), `v.M(g())`, `p.M(g())` (value receiver copy), method value `v.M` | read after `g` |
| `s[i] + f()`, `arr[i]`, `m[k]`, `p.a`, `*p`, `string(b)`, `str + f()`, `str == f()`, `any(x)` comparisons | read after `f` |
| `len(s)`, `cap(s)`, `append(...)`, `copy(...)`, `s[i:j]`, `[]byte(str)`, `x.(T)` with `T` non-pointer, `&&`/`||` | evaluated where they appear (sequenced) |
| `x.(*T)`, `x.(T)` with pointer-shaped `T` | plain (read after calls) |
| conversion to an interface of a non-addressable operand of a type gc converts by address (`uint8`, `bool`, 16-byte structs, pointers such as `&s[i]`); `x+0` of `int`/`uint16`/`uint32`, strings, slices, one-word arrays/structs | where it appears; plain |
| map key of the generic map path (`map[uint8]`, `map[bool]`, `map[uint16]`, `map[any]`, 16-byte struct keys, elements > 128 bytes), non-addressable | where it appears (reads, `=`, `op=` targets, literal keys); fast paths (`uint32`/`int32`, 8-byte keys, strings) and addressable keys are plain |
| map read keyed by `string(b)` | sequenced |
| `m[k] = append(m[k], f())` | map operand (unless a variable) and key (unless a variable of exactly `uint32`/`uint64`/`string`, or any variable on the generic path) copied before `f`; the first argument reuses them |
| `map[K]V{k: 3, 1: 2}` with `k == 1` | 3: constant entries (no interface conversion other than of `nil`) are inserted first; other entries in source order, each its own owner |
| `a[i] = f()`, `p.a = f()`, `*p = T{f(), x}`, `b[0] += f()`, `m[k] op= f()` | target operands read after `f` (new slice, pointer, map, index) |
| `r1, r2 = x, f()`, `var a, c = x, f()`, `return x, f()`, `defer g(x, f())`, `switch x + f()` | `x` after `f` |
| `s[j], j = 1, 2`; `j, s[j] = 1, 2` | stores at the old `j` (spec) |
| `switch x { case f()+1: }` | tag read and saved before the cases |
| `z[5] + panic()`, `z[5] = panic()`, `q.a + panic()`, `x/0 + panic()`, `nilmap[k] = panic()`, `nilfunc(panic())`, `niliface.M(panic())` | the panic first |
| `ii.(string) + panic()`, `len(z[5:9]) + panic()`, `len(make([]T, -1)) + panic()` | the sequenced operation's trap first |

The one optimizer effect (`probe/optimizer`): with inlining, gc's SSA back end
may fault on a nil pointer read early by the order pass *after* a later
inlined call panics (`f(uint8(p.a), w())`, `b(*q == v) + w()`); with
`-gcflags=-l` it follows the order pass. goSVM follows the order pass. This is
recorded in the contract and excluded from the vectors (no nil `gp`/`gq` with
panicking writers).

## What changed

- **Check** (`internal/compiler/evalorder.go`) classifies the sequenced
  operations (`CheckedProgram.Sequenced`) and, per *owner* (simple statement,
  root expression, sequenced operation, `&&`/`||` operand, dynamic map-literal
  entry), finds plain operations that precede a sequenced one in source order
  and that it could observably reorder: a read of storage the sequenced one
  may write (package variables; address-taken or closure-captured locals;
  anything through a pointer, slice or map), or a trap racing a call that may
  log (`panic`, CPI, return data, dynamic calls). Write and log effects of
  calls are summarized transitively (a function assigning only its own locals
  writes nothing). Those owners are recorded in `Hoisted` with their sequenced
  operations; `MapLiteralOrder`, `SharedOperands` and `HoistedConversions`
  record the map-literal and `m[k] = append(m[k], ...)` rules.
- **Emitter** evaluates a `Hoisted` owner's list first, saves the values and
  emits the owner left to right using them (unused values are internal
  errors); map literals follow `MapLiteralOrder`. No other C changes.
- **Docs**: ERROR_CONTRACT "Evaluation order" (rules, traps, the optimizer
  exception, changelog), BACKEND_INTERFACE "Evaluation order" (why the
  decision is data in `CheckedProgram` rather than rewritten syntax), and the
  old deviation notes in GO_INTEGERS, GO_GLOBALS, GO_MAPS, GO_FUNCTIONS,
  GO_INTERFACES, GO_STRINGS, GO_CONTROL and HANDOFF now point at the
  contract.

## Evidence

- `examples/eval-order` (generated by `examples/eval-order/gen`: 912 generated
  cases combining 17 operand locations and their writers with 26 expression
  and 8 statement forms, and 11 assignment targets with 6 assignment forms;
  66 hand-written cases from the probe table). `TestEvalOrderDifferential`:
  5,868 host vectors (six per case, with panicking writers and random traps)
  match native Go (2,990 aborts, 2,834 with a panic line). With `Hoisted`
  disabled, 1,499 vectors in 538 cases differ.
- `TestEvalOrderSBF`: 2,934 cases (three per case; 1,505 aborts, 1,425 with
  the panic line required and the rest with no panic line) pass on fogo-v0
  (runner 0.5.0) and solana-v3 (runner 0.6.0), runners installed from the
  local packages into a scratch `GOSVM_CACHE`; ELF
  `17c6a47b…6696` (fogo-v0) and `5bdbff7d…a0f7` (solana-v3).
- `TestEvalOrderHoisted`, `TestEvalOrderLocalsAndTraps`, `TestEvalOrderMaps`,
  `TestEvalOrderNothingToHoist` check the Check fields;
  `TestCompoundOperandOrder` and `TestGlobalOperandOrder` now pin gc's
  results.
- `make test` and `go vet ./...` pass; every SBF suite ran (none skipped).

## Byte identity

`python3 scripts/verify_rebuild.py --reference
results/compiler/2026-10-14-v157-rebuild/metrics.json`: full swap fogo-v0
`4697a9f5…cfc0` and solana-v3 `2b5f21d9…ba9e`, clock2 `0f768370…3804` and
`54f34b65…66e5`, all identical to the reference.

The frozen check corpus keeps every verdict; four entries' C changed and are
listed in `corpusChanges` (pointer-values `b.Value(next(s))` copies `*b` after
`next`, which writes through a slice; control-values reads `i[1]` after
`next(s), next(s)`; each recorded twice). Every SBF suite was run with the
baseline (`48fb51f`) and this branch; ELFs that changed, all because a call
there may write an operand read before it (or a constant map entry follows a
computed one), with unchanged results:

| Suite (2026-10-14 record) | fogo-v0 | solana-v3 |
| --- | --- | --- |
| compound (compound-assignment) | `bb53b2c0` → `3f77e5be` | `ebcc2f97` → `61bbcd74` |
| func values (func-values) | `e6519f9c` → `cf7daa5d` | `fa1777bf` → `1429d7f9` |
| func-maps combination (func-maps) | `99662e27` → `ae286ef2` | `280a454c` → `27b6d15b` |
| func-strings combination (func-maps) | `e2e63d72` → `b3dc14f1` | `7cf14582` → `e3821234` |
| maps (maps; static entries first) | `1c9d45e0` → `21f086a1` | `4f2108e1` → `68a27dcf` |
| strings (strings) | `998161cc` → `9913aa2f` | `70174787` → `943a070b` |
| allocation heap32 / heap64 (allocation) | `21ac0e94` → `1dbc3061`, `f1d481d8` → `b26f72d9` | `4e6b6ee0` → `13ba40a9`, `91005d64` → `daad889b` |

Unchanged: globals, global value types, interfaces, int values, mixed, value
types, generics, bits, range/uint16, full swap and clock2.

`metrics.json` has the full hashes and counts.

## Reproduce

```sh
sh results/compiler/2026-10-15-eval-order/probe/run.sh /tmp/probe   # gc's table
go run ./examples/eval-order/gen && gofmt -w examples/eval-order/program.go
make test
build/gosvm runner install -archive <0.5.0 package>   # with GOSVM_CACHE=<scratch>
build/gosvm runner install -archive <0.6.0 package>
GOSVM_CACHE=<scratch> go test -ldflags=-linkmode=external -run SBF -v ./internal/compiler/
python3 scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json --output <new dir>
```
