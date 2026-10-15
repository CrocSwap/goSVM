# Compound assignment on integers: host and SBF

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_INTEGERS.md](../../../docs/GO_INTEGERS.md#compound-assignment).

## Question

Do `+=`, `-=`, `*=`, `/=`, `%=`, `&=`, `|=`, `^=`, `&^=`, `<<=` and `>>=`
behave as in native Go on every integer type and every addressable target
(locals, fields, fields through pointers and pointer chains, local, struct
and pointed-to array elements, slice and nested elements, `*p`, present and
absent map elements, captured variables, loop post statements), including
wrapping, signed division, shifts at and beyond the width, division by zero,
negative shift counts, and Go's evaluation order with side effects; and do
programs without compound assignment compile exactly as before?

## Method

- `TestCompoundDifferential` runs
  [examples/compound-values](../../../examples/compound-values/program.go)
  (generated from a target × operator table, then ordinary Go) natively and
  as host-compiled C: 12,080 vectors over five modes (two over the twelve
  integer types including named ones, edge cases, evaluation order, loops),
  2,331 of which panic in Go and must abort at the same point with the same
  partial output (so side effects before each trap are compared too).
- `TestCompoundSBF` builds the fixture for fogo-v0 and solana-v3
  (platform-tools v1.57) and runs 380 vectors through the pinned runners
  (fogo-v0: gosvm-svm-runner 0.5.0; solana-v3: 0.6.0, installed into a
  scratch `GOSVM_CACHE` from the local packages under `results/svm/`); Go
  panics must fail with `ProgramFailedToComplete`, log `SBF program panicked`
  once and roll back. A second program measures compute: 128 iterations of
  `acc += k` vs `acc = acc + k`, and `m[j%8] += k` vs `m[j%8] = m[j%8] + k`,
  minus the 0-iteration baseline.
- Byte identity: the frozen corpus (`TestCorpusMatchesPreCheckCompiler`)
  keeps every entry's verdict and C hash except its two compound
  assignments (entries 161 and 232), which now compile; the minimum rejection
  count drops from 82 to 80. Full-swap ELFs are rebuilt with
  `scripts/verify_rebuild.py`; the recorded probe ELFs of allocation,
  interfaces, strings, function values, maps and `math/bits` reproduce.

```sh
GOSVM_CACHE=<scratch cache with both runners> GOSVM_INT_RESULTS=/tmp/int \
  go test -ldflags=-linkmode=external -run 'TestCompound' ./internal/compiler/
```

## Results

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| SBF cases (aborts with rollback) | 380 (78) | 380 (78) |
| Fixture ELF | `bb53b2c0…` | `ebcc2f97…` |
| `acc += k` / `acc = acc + k`, CU per iteration | 9.02 / 10.02 | 9.02 / 10.02 |
| `m[k] += v` / `m[k] = m[k] + v`, CU per iteration | 66.2 / 110.6 | 66.2 / 110.6 |

Every vector matched native Go on both targets. The map form saves one hash
lookup per update. Full-swap ELFs are unchanged: fogo-v0 `4697a9f5…`,
solana-v3 `2b5f21d9…`. Hashes are in [metrics.json](metrics.json); fixtures,
ELFs and runner reports stay local.

Order note: the target's index and pointer operands are evaluated before
the right operand (the spec's assignment order, and goSVM's order for `=`);
gc reads plain variables used as such operands after the right operand's
calls. The fixture avoids right operands that reassign their own target's
slice, map, pointer or index variable; `TestCompoundOperandOrder` pins
goSVM's result for those.

The local runners are not a public network; see
[target profiles](../../../docs/TARGET_PROFILES.md).
