# Complete creator-managed swap lifecycle

Passed October 6, 2026 on the recorded macOS arm64 host. [summary.json](summary.json)
binds the source, commands, checks and artifact hashes. This proof uses the actual
CLI, fresh schema-2/SDK-2 scaffold, generated artifacts, pinned classic SPL Token
image, runner 0.5.0 and local feature profile.

The ELF is 68,320 bytes, SHA-256
`8d37edb41b22b2ed59d89a6625ebe7721797d428cf4df387eea307dd55c872ab`. Every frame is static; the maximum is 3,968 bytes. Instrumented
linking reproduces the exact executed ELF. No older results are overwritten.

29 scenarios/36 transactions, including 25 expected failures, pass in three
SBF repetitions and an additional project CLI run. Independent Python byte/math,
PDA and lamport expectations verify complete account state. Pool/vaults start
unallocated: rent-funded creation, token initialization, both reserve deposits,
a managed swap, both withdrawals, SPL vault closing, owned pool closing and reuse
are covered. Current/custom/free Rent, signer/owner/substitution/duplicate failures,
frozen second deposit/refund, altered close authority, refund overflow and atomic
later-instruction rollback are exercised. Packaged lifecycle fixtures match the
independently reconstructed suite exactly.

Native SDK tests check encoding, preconditions, effective close authority,
stale references and propagated errors. The separate app's native handler tests
exercise the generated lifecycle without claiming callback atomicity.

| Tested managed operation | CU |
| --- | ---: |
| Create and both deposits | 86,742 |
| Swap | 47,858 |
| Drain and close | 61,869 |

```sh
python3 scripts/managed_swap_verify.py --output results/compiler/new-managed-proof
```

This is creator-owned liquidity without LP tokens. The creator may drain and
close at any time. Classic non-native token support excludes wrapped SOL,
Token-2022, delegate and multisig paths. External mints/user holdings are preexisting
inputs. It is not a production audit, fresh-validator or developer-trial result.
Individual command times are not repeated comparative build benchmarks.
