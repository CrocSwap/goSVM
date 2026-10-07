# Milestone 1 acceptance rehearsal

October 5, 2026, existing macOS arm64 M2 desktop. All installation/workflow
checks passed with a source-installed CLI, isolated empty Go/module/goSVM caches,
the pinned runner archive and a copied, verified managed backend. This is an
installed-host rehearsal, not a clean-machine installation or a fresh validator
comparison. Milestone 1 remains open for those two checks.

## Executed checks

| Workload | Scenarios/cases | Transactions | Result |
| --- | ---: | ---: | --- |
| Generated bounded starter | 14 | Starter simulation/submission checks | Exact recorded cases and freshly compiled ELF match |
| Explicit project-general example | 3 | 3 | Exact recorded cases and ELF match |
| Matched full-token Go adapter | 108 | 110 | Exact state/error/CU/log cases and fresh 8,064-byte ELF match |
| Original manual token lifecycle | 6 | 26 | Exact cases and fresh 8,000-byte ELF match |
| Ordered Clock/Rent syscall controls | 6 | 15 | Exact cases and fresh 1,664-byte probe match |

The starter is a freshly generated standalone module. Its native tests and the
project-general native tests ran with `-count=1`; application ELFs were rebuilt
without reusing prior SBF build output. Only Go was available on the testing PATH;
the compiler resolved the verified managed backend and the pinned installed
runner. Cargo, Rust, a validator and host Clang were absent from that PATH.
Source bootstrap separately used installed Go/host build tools.

The metadata-negative check rejected the exact validator `rent_epoch` expectations
and removed a previous valid report. There was no normalization or weakened
account-state assertion. The pinned runner is unchanged at 0.5.0.

## Evidence and review

- [Summary, source hashes, command records and acceptance status](summary.json).
- [Independent captured-oracle compatibility review](compatibility-review.json):
  all 105 Go token vector CU/errors, lifecycle cases, token/main ELF hashes and
  active feature IDs match. The fixture files differ only in `rent_epoch`.
  Vendored LiteSVM changes remain limited to `src/lib.rs`, and all runtime-source
  hashes match the pinned package provenance.
- [Root/nested Go checks and five Rust runner tests](checks.json).
- [Backend receipt](backend-receipt.json) and [runner receipt](runner-receipt.json).
- [Starter](starter.json), [project-general](project-general.json),
  [token](full-token.json), [lifecycle](lifecycle.json), [sysvar](sysvars.json)
  reports, with freshly compiled token/probe ELFs alongside them.
- [Metadata rejection](metadata-rejection.log). `summary.json` names every other
  command log, including bootstrap, installation, doctor, compilation and tests.

The local RPC probe returned `Operation not permitted`, so no new validator was
started. The prior captured independent oracle remains evidence; it does not
close the final fresh-validator gate. The backend came from this desktop's
existing managed installation, so this run is explicitly ineligible for the
clean-host gate. Timings are command diagnostics, not new workflow benchmarks.
The dependency-heavy protocol and Basanos were not exercised or modified.

Use [the acceptance procedure](../../../docs/MILESTONE1_ACCEPTANCE.md) on a clean
host and a validator-capable environment. Rehearse locally into a new directory:

```sh
python3 scripts/milestone1_acceptance.py \
  --output results/svm/new-m1-rehearsal \
  --runner-archive results/svm/2026-10-05-runner-install-complete/gosvm-svm-runner-0.5.0-darwin-arm64.tar.gz \
  --existing-backend build/polish/cache/toolchains/v1.51/darwin-arm64 \
  --host-note 'Existing host, copied backend; not clean-machine evidence'
```

The two earlier harness bring-up attempts,
`../2026-10-05-m1-acceptance/` and `../2026-10-05-m1-acceptance-complete/`, retain
their `passed:false` summaries and logs. They exposed acceptance-script setup
issues, corrected before this final run; they are not passing evidence.
