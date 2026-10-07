# Generated escrow lifecycle proof

Passed October 5, 2026 on the recorded macOS arm64 host: 32 scenarios, 42
transactions and 28 expected failures, repeated three times through runner 0.5.0
with its pinned local feature profile. An additional project `gosvm test --svm`
run produced identical case, CU and log outputs. Packaged fixtures exactly match
independent expectations reconstructed by the verification script.

The proof builds an isolated CLI, installs the pinned local runner archive,
scaffolds schema 2/SDK 2, copies application sources/config/history, checks generated
artifacts byte-for-byte and runs native tests. The SDK stays as scaffolded. It also
runs 27 invalid lifecycle declarations with generated-artifact preservation checks
and a native multi-init case requiring every target to be validated before funding.

The actual SBF program creates a rent-funded PDA, accepts a SOL deposit, validates
a hashlock claim or creator cancellation, closes and reuses the PDA. Failures
cover signer/owner/recipient substitution, duplicate accounts, preallocated or
funded targets, wrong seeds/bump/secret, failed System deposit, refund overflow,
current/custom/free Rent and whole-transaction rollback. Atomic cases roll back
creation/deposit or restore previously closed owned state after a later failure.
Independent byte/lamport expectations check complete state rather than only codes.

| Artifact/operation | Result |
| --- | ---: |
| ELF | 25,280 bytes |
| Largest static frame | 1,984 bytes |
| Open, tested normal lifecycle | 20,508 CU |
| Claim, tested normal lifecycle | 8,356 CU |
| Cancel, tested normal lifecycle | 6,756 CU |

ELF SHA-256: `4b858bceb77129196635f0873d6c61357180de013a7f90106a802fe7042c93f3`.
Stack-instrumented linking reproduces that same hash; every recorded frame is
static and within 4,096 bytes. [summary.json](summary.json) records source/tool
hashes, commands and individual timing observations; [project-svm.json](project-svm.json)
and `svm-0.json` through `svm-2.json` record actual execution.

```sh
python3 scripts/escrow_verify.py --output results/compiler/new-escrow-proof
```

This is cancellable SOL escrow with rent/donations paid to the close recipient,
without a deadline or fair-exchange guarantee. It is not a production audit, token
exchange, fresh-validator check, repeated comparative build benchmark or external
developer trial. Full swap lifecycle and remaining milestone-2 gates are tracked
in the [audit](../../../docs/MILESTONE2_ACCEPTANCE.md). Historical results remain intact.
