# SDK-2 lifecycle boundary fixture

This experimental fixture exercises current Rent reads, checked System creation
and funding, owned resize/close operations, duplicate descriptors and reuse. It
uses account indices and an explicit probe wire format to test the runtime
boundary. It does not supply generated application authority/payer policy or
complete the swap/escrow lifecycle gate.

The [proof](../../results/compiler/2026-10-05-lifecycle2-supported/README.md) executes
the actual compiled SBF artifact in the pinned runner. It verifies full account
bytes, owners and lamports for signed/PDA creation, explicit rent top-ups,
resize/zeroing, close/recreate, boundary/security failures and atomic rollback.
Native and generated-C tests separately check rent rounding and lifecycle behavior.

```sh
python3 scripts/lifecycle2_verify.py --output results/compiler/new-lifecycle-proof
```

Run from the repository root with installed v1.51 LLVM and the pinned local runner
archive. Choose a new evidence directory. The script builds an isolated CLI,
scaffolds SDK 2, stages this fixture in that module, executes three SBF repetitions,
checks static frames and reproduces the exact ELF with instrumented linking.
It also rebuilds the historical manual token adapter and checks its ELF identity.
No fresh-validator or external developer-trial result is claimed. See
[SDK 2](../../docs/SDK2.md) for API contracts and native-context requirements.
