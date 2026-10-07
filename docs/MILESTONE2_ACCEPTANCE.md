# Milestone 2 completion audit

The active user goal is **complete milestone 2**, retaining the exit criteria in
[the roadmap](../ROADMAP.md). This checklist tracks the full deliverable set;
completed compiler experiments do not substitute for framework/application gates.
Milestone 1 is closed, with its two additional checks explicitly skipped.

| Requirement | Current status | Evidence needed for completion |
| --- | --- | --- |
| Module-aware packages and ordinary-Go type identity | Implemented within documented offline subset | [Import evidence](../results/compiler/2026-10-05-shared-imports-final/README.md), transitive/unsupported diagnostics and dependency-cache behavior |
| Fixed arrays and safe bounded slice views | Scalar arrays and borrowed byte views implemented within documented bounds | [Array evidence](GO_ARRAYS.md), [view evidence](GO_VIEWS.md): lifetime, capacity/bounds, aliases and native-Go/C/SBF/stack comparisons |
| Methods and constrained pointers | Implemented within the lexical borrowing subset | [Pointer/method evidence](GO_POINTERS.md): receiver copies, aliases, nil/bounds, compile-time escapes and native-Go/C/SBF comparisons |
| Practical control flow and multiple returns | Multiple unnamed results/assignments, switch and continue implemented and verified | [Control evidence](../results/compiler/2026-10-05-control-values/README.md), evaluation order and failure effects, native-Go/C/SBF agreement; explicit status-code contract |
| Schema-2 instruction/account framework | Token/PDA/key-field bindings and generated init/authorized-close policy implemented; both application lifecycles verified | [Schema-2 APIs](SCHEMA2.md), [108-scenario full swap proof](../results/compiler/2026-10-06-framework-swap-managed/README.md); [Generated lifecycle/escrow proof](../results/compiler/2026-10-06-escrow-token-lifecycle/README.md); [Full swap lifecycle proof](../results/compiler/2026-10-06-managed-swap-supported/README.md) verified |
| Stable wire layouts, versioning, clients and IDL | Implemented and both application layouts/clients verified; experimental compatibility policy | [Resolved canonical types/codecs/version history](SCHEMA2.md), generated application clients/common vectors; schema-1 preserved |
| Checked classic-token/PDA/CPI APIs | Bounded SDK 2 and generated constraints implemented; signed vault creation and closing integrated and verified | [SDK 2](SDK2.md), [117-scenario boundary proof](../results/compiler/2026-10-06-checked-token-managed/README.md) and [full swap proof](../results/compiler/2026-10-06-framework-swap-managed/README.md); [Lifecycle integration proof](../results/compiler/2026-10-06-managed-swap-supported/README.md) passes; normal/Signed SDK helper native checks also pass |
| Application lifecycle | SDK operations and generated lifecycle policy verified in escrow and the complete creator-managed swap | [35-scenario/46-transaction lifecycle proof](../results/compiler/2026-10-05-lifecycle2-supported/README.md) covers current rent, signed/PDA creation, explicit funding, resize/close/reuse, aliases and rollback; [Escrow proof](../results/compiler/2026-10-06-escrow-token-lifecycle/README.md) covers generated payer/authority/refund policy; [29-scenario/36-transaction swap lifecycle proof](../results/compiler/2026-10-06-managed-swap-supported/README.md) covers creation, deposits, swap, drain/close/reuse and rollback |
| Full token swap migration | Full lifecycle and repeated comparative measurements verified | [Application](../examples/full-swap/README.md): canonical types/math, generated bindings, all 108 manual-baseline scenarios/110 transactions retained with explicit framework error mapping; [Managed lifecycle](../results/compiler/2026-10-06-managed-swap-supported/README.md) and [preserved corpus/service regression](../results/compiler/2026-10-06-framework-swap-managed/README.md) and [125-sample build/CU/footprint experiment](../results/compiler/2026-10-06-milestone2-bench-supported/README.md) pass |
| Independent escrow-style application | Cancellable SOL hashlock escrow implemented; native and actual-SBF lifecycle proof passes | [Application/workflow](../examples/escrow/README.md), [32 scenarios/42 transactions × three SBF runs](../results/compiler/2026-10-06-escrow-token-lifecycle/README.md): create, claim/cancel, reuse, authority/substitution/duplicates, failed CPI and transaction rollback; external developer trial remains separate |
| Independent ordinary Go service | Actual legacy/managed swap consumer implemented; isolated dependency, wire, lifecycle codec and math checks pass | [Service](../examples/full-swap-service/README.md) imports canonical swap types/calculation and generated codecs without compiler/runtime or SDK replacement; shared wire/native/SBF swap proof recorded; managed layout/arguments and CLI mode now covered by the same isolated consumer proof |
| Trial-developer workflow | Independent trial open; kit and outside-checkout maintainer rehearsal pass | [Trial procedure](MILESTONE2_TRIAL.md), [kit/rehearsal evidence](../results/compiler/2026-10-06-milestone2-trial-kit-final/README.md): reproduce both applications through documented CLI without compiler/C edits or manual binding offsets; obtain independent logs plus unsupported-feature/usability feedback; resolve blocking findings |

The existing [shared-package client](../examples/shared-packages/README.md) proves
pure library reuse using handwritten codecs and local development module
replacements. It does not prove schema-2 clients or full-token application reuse.
The manual token/lifecycle corpora prove runtime capabilities. The generated
escrow additionally proves Go authoring of rent-funded initialization and
authorized closing. The creator-managed swap now starts with unallocated pool
and vault accounts and exercises deposits, swaps, draining and closing. Its
external mints/user token holdings remain explicit fixture inputs. Public signed releases, wider-host
verification and production support remain later milestones.

For closure, inspect authoritative source, generated artifacts, command logs,
wire vectors, native/SBF reports and application measurements for every row.
Do not infer application security or transaction atomicity from a native mock.
Do not mark the goal complete while any required gate is missing or unverified.

October 6 checkpoint: all implementation/application and measurement rows above
have evidence. The independent developer trial remains the sole open gate; a
maintainer's automated rehearsal is explicitly insufficient to close it.

The optional [compact single-signer adoption](COMPACT_SIGNER_ADOPTION.md) now
passes a canonical Whirlpools rebuild (166 cases × three runs) and current
checked-token, swap/service, managed-lifecycle and escrow regressions. This SDK
helper does not close the independent trial gate. The previously prepared trial
kit retains its complete matching pin; it is not implicitly upgraded.
