# General multi-account fixture validation

October 5, 2026. Runner 0.4.0 and `gosvm-svm-fixtures-v1` now execute general
named-account/signer scenarios through one persistent VM. `gosvm svm-test` accepts
an existing ELF; project `test --svm -svm-fixtures` keeps native tests, generation,
and compilation. Schema-1 starter fixtures remain supported.

Three complete runs passed 108 full-token scenarios/110 transactions per run.
Every step was simulated and submitted, with complete asserted account bytes and
metadata, exact errors, CU, and CPI log counts checked. Scenarios cover all 105
Go token corpus vectors, two swaps followed by atomic transaction rollback,
owner overrides, and subsequent checkpoint isolation. This goes beyond the
original harness's mostly simulated negative cases by submitting them as well.

The fresh independent validator 3.0.15 harness passed its matched 315 simulations,
six committed swaps, and six rollback checks across Go, lean Rust, and Anchor.
All 105 Go vector CU/error results, main ELF, token image, and feature IDs match
the general CLI. The declarative path runs the Go artifact; it is not a new
three-language execution-speed comparison.

Expected account bytes derive from initial fixture files plus independent
full-width swap math. VM output snapshots are not used as the account oracle.
Error/CU expectations were exported only after the existing harness validated
them. The fixture corpus targets the matched Go wire adapter, not a new compiler
or framework implementation.

## Evidence

- [`token-fixtures.json`](token-fixtures.json) and [`spl-token.so`](spl-token.so):
  executable corpus and checksum-pinned token dependency.
- [`fresh-validator.json`](fresh-validator.json) and
  [`fresh-validator.log`](fresh-validator.log): fresh differential baseline.
- [`general-0.json`](general-0.json), [`general-1.json`](general-1.json),
  [`general-2.json`](general-2.json): complete results; case/step outputs agree
  exactly across all three runs, including logs.
- [`selected-results.json`](selected-results.json): one selected stateful
  scenario includes all three ordered transactions.
- [`image-rejection.log`](image-rejection.log), [`state-rejection.log`](state-rejection.log),
  and [`clock-rejection.log`](clock-rejection.log): checksum mismatch, deliberate
  account byte mismatch, and incomplete sysvars fail without stale reports.
- [`controlled-clock-results.json`](controlled-clock-results.json): exact slot
  9,007,199,254,740,993 and negative timestamps survive the general file/report path.
- [`starter.json`](starter.json): all 14 original schema-1 starter fixtures pass.
- [`project-general.json`](project-general.json): the small general example
  passes through project native-test/generate/build workflow.
- [`controls.log`](controls.log): three repeated real SBF syscall probes still
  pass checkpoint/history/fees/blockhash tests and watch unchanged accounts outside
  the transaction message.
- [`rust-tests.log`](rust-tests.log): typed-control atomicity, reusable checkpoint,
  atomic reset overrides, protected sysvar initialization, and protocol tests.
- [`summary.json`](summary.json): source/fixture/artifact hashes and scope.

The earlier [first run](../2026-10-05-general-fixtures/README.md),
[project workflow run](../2026-10-05-general-final/README.md), and
[protected-account run](../2026-10-05-general-04/README.md) remain separate source
snapshots. Reproduce into a new directory:

```sh
python3 scripts/svm_fixtures.py --output results/svm/new-general-fixture-experiment
```

Prepare the three matched token ELFs under `build/anchor` using the existing
Anchor methodology. The script requires already-installed pinned SBF tools,
validator 3.0.15, Go, and cached host Rust dependencies. `--llvm` can select the
backend. Basanos source and outputs were untouched.

General fixture execution currently uses LiteSVM; the fresh validator comparison
uses the independent benchmark harness. Native-program aliases, address lookup
tables, fixture control steps, initialization/closing and wider rent/lifecycle
coverage remain open. Only macOS arm64 has been executed. The dependency-heavy
protocol workload was not run. No portable timing, memory, or deployment claim
is made. See [format and limits](../../../docs/SVM_FIXTURES.md).
