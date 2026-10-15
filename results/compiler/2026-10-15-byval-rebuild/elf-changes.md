# SBF ELFs changed by the aggregate-parameter fix

Every SBF ELF that `go test ./internal/compiler ./internal/project` builds
(the SBF suites on fogo-v0 and solana-v3) was hashed at the previous commit
(`8843887`) and with this fix, keyed by the program's sources, target and
heap budget (temporary instrumentation, not committed). 136 ELFs were built
by both: **88 are byte-identical, 48 changed**. Every suite passes with the
changed ELFs (same outputs, errors, logs and rollback as native Go). Two
ELFs are new (`examples/byval-values`, both targets).

## Recorded October 14/15 ELFs that changed

| Results | Program | Target | Before → after |
| --- | --- | --- | --- |
| [2026-10-14-globals](../2026-10-14-globals/README.md) | `examples/global-values` | fogo-v0 | `f48ad1eb…` (42,128) → `ec09db53…` (42,240) |
| | | solana-v3 | `764a2ec6…` (41,456) → `3275211f…` (41,560) |
| [2026-10-14-globals-value-types](../2026-10-14-globals-value-types/README.md) | `examples/global-value-types` | fogo-v0 | `157aed94…` (46,496) → `31898de3…` (42,568) |
| | | solana-v3 | `95b85844…` (45,088) → `8c38143e…` (44,688) |
| [2026-10-14-interfaces](../2026-10-14-interfaces/README.md) (also listed in maps) | interface suite ELF (`iface_elf_sha256`) | fogo-v0 | `c7daf643…` (45,696) → `2afea5b7…` (45,952) |
| | | solana-v3 | `8e88eb9e…` (63,888) → `dea8b937…` (64,704) |
| [2026-10-14-value-types](../2026-10-14-value-types/README.md) | value-types suite ELF (`elf_sha256`) | fogo-v0 | `c8e7dd33…` (43,592) → `fae9e102…` (43,616) |
| | | solana-v3 | `eae9703d…` (45,496) → `03d45c94…` (45,688) |
| this directory | full swap | fogo-v0 / solana-v3 | see [README](README.md) |

Each of these programs copies an aggregate parameter somewhere or appends
(the growth helper's C changed). The recorded ELFs of
[stdlib](../2026-10-14-stdlib/README.md) and
[eval-order](../2026-10-15-eval-order/README.md) had already changed at the
previous commit (evaluation-order merge, and the eval-order cases added in
that merge); the stdlib-values ELF changes again here (fogo-v0 253,848 →
260,112 bytes, solana-v3 289,576 → 294,232). The other recorded ELFs the
suites still build are unchanged (compound-assignment CU probe, func-values
calls, globals' 14 other programs, interfaces' dispatch, math-bits, the
value-types CU probe, clock2). Recorded ELFs the suites no longer reproduce
at the previous commit (allocation heap probes, func-maps, func-values
`func_elf`, maps, strings, compound-assignment `compound_elf`) were already
superseded by later merges and are not compared.

## The other changed ELFs (not recorded in results)

40 further probe and combination programs of the suites changed (sizes from
−56 to +4,592 bytes). Their hashes are in the local comparison logs, not
tracked.
