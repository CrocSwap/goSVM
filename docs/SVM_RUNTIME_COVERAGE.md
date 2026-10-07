# Experimental SVM coverage and milestone 1 acceptance

Milestone 1 establishes a fast development loop for the current bounded and
classic-token programs on macOS arm64. Its acceptance scope is the pinned runtime
subset below. Broader Solana coverage, public distribution and wider host support
are separate work; they are not an unlimited list of milestone 1 requirements.

This matrix records executed experiments, not a production support promise.
LiteSVM is 0.8.2 with the documented rent-error patch; the sidecar is 0.5.0 and
the validator oracle is Agave 3.0.15. The runner uses Agave runtime 3.0.10 and
an observed local-validator feature profile. It does not claim mainnet parity.

## Tested subset

| Area | Executed coverage | Evidence and limits |
| --- | --- | --- |
| SBF programs | Platform-tools v1.51, SBFv3; bounded Go/lean Rust/Anchor and matched token ELFs | [Initial comparison](../results/svm/2026-10-05-litesvm/README.md); other SBF versions are not accepted by the fast CLI |
| Transactions | Legacy messages, multiple accounts/signers, merged privileges and ordered multi-instruction transactions | [General fixtures](../results/svm/2026-10-05-general-complete/README.md); no address lookup tables/versioned-message fixture authoring |
| Classic SPL Token CPI | Real transfers, PDA signing, exact account bytes, failed CPI and atomic rollback | [Matched token fixtures](../results/svm/2026-10-05-general-complete/README.md); token image is pinned, Token-2022 is outside this gate |
| Native System/account lifecycle | Transfer, allocate, assign, create; transaction-created mints/token accounts; drain, close, refund and recreation | [Lifecycle corpus](../results/svm/2026-10-05-lifecycle-verified/README.md); Go pool remains genesis seeded; authoring APIs are milestone 2 |
| Errors and rent | Exact checked error categories/indices, successful underfunded creation rejected; existing instruction errors preserved | [Patch and metadata differences](../results/svm/2026-10-05-lifecycle-verified/README.md); new-account `rent_epoch` differs explicitly, and broad bank rent collection is not modeled |
| Clock/Rent controls | Initial controls and ordered per-step updates; typed cache/account views and real SBF syscall reads; uint64/int64 boundary values | [Stateful controls](../results/svm/2026-10-05-scenarios-complete/README.md); this corpus has independent byte expectations, not a new validator CU oracle |
| Isolation/history | Reusable checkpoints, atomic ordinary-account overrides, fee/status/history restoration, duplicate checks and explicit blockhash expiry | [Lower-level controls](../results/svm/2026-10-05-controls-final/README.md) and [stateful fixtures](../results/svm/2026-10-05-scenarios-complete/README.md); changing Clock does not implicitly advance history or blockhash |
| Developer commands | Native tests plus actual compiled-SBF tests, selection, state/error/CU/log assertions, stale-report removal and diagnostics | [Workflow measurements](../results/svm/2026-10-05-workflow-complete/README.md); bounded and manual full-token workflows are distinct |
| Installation | Pinned local archive, integrity checks, staged installation and managed selection without Cargo in test workflows | [Installer validation](../results/svm/2026-10-05-runner-install-complete/README.md); installed-host experiment, not clean-machine OS verification |

Successful and failed transactions retain independent full-state assertions.
Known differences are described explicitly rather than normalized away. SVM
confirmation markers are synchronous test outcomes, not bank/consensus behavior.
General fixtures run through LiteSVM; the independent token/lifecycle harnesses
provide the validator oracle, while starter `test --sbf` remains available.

## Milestone 1 completion checklist

**Closed October 5, 2026 by user decision.** The user explicitly skipped the
additional fresh-validator and clean-host checks and authorized milestone 2.
Existing passing evidence and the recorded compatibility review are the accepted
finish line. The skipped checks were not executed or represented as passing.

- [x] Fast compiled-ELF workflow and deterministic fixture isolation.
- [x] Bounded/token/lifecycle correctness evidence, with explicit runtime and
  account-metadata limits.
- [x] Complete edit/test measurements and provisional local latency targets.
- [x] Pinned local runner package and verified offline installation.
- [x] Documented tested subset and remaining acceptance gates.
- [x] Reproducible [acceptance procedure](MILESTONE1_ACCEPTANCE.md), with
  [current-source installation/workflow rehearsal and compatibility review](../results/svm/2026-10-05-m1-acceptance-final/README.md).
- [x] Accepted recorded compatibility review and explicitly documented limits.

| Additional check | Closure status |
| --- | --- |
| Final fresh-validator pass | Skipped by the user for milestone 1; local RPC binding remains restricted. Captured comparisons remain historical evidence. |
| Clean macOS arm64 host verification | Skipped by the user for milestone 1. Current-host cache isolation is not clean-machine evidence; fresh-machine installation remains part of the public-alpha work. |

Do not add every future sysvar, syscall or protocol feature to this checklist.
Add coverage when an application requires it, with an explicit scope and oracle.
Additional sysvars, Go sysvar authoring, Token-2022 and lookup tables are outside
this completion gate. Consensus/bank simulation is not the lightweight runner's
purpose. The dependency-heavy protocol and proposed external-program port keep
their own metrics and compatibility evidence; Basanos remains untouched.

Public download hosting, Developer ID signing/notarization, license/notice review,
release ownership and Linux/wider-host distribution belong to milestone 3's
public alpha gate. They remain required before public release. Milestone 2 focuses
on Go language/framework foundations and complete multi-account applications.
