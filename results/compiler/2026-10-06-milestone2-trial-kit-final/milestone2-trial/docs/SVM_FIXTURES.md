# Experimental general SVM fixtures

The `gosvm-svm-fixtures-v1` format describes named accounts, local test signers,
additional ELF images, and ordered transaction scenarios. It is a testing format,
independent of the generated binding schema. The compiler/framework language
subset has not changed. Existing schema-1 `testdata/sbf.json` files keep working.

## Commands

For a generated project, native tests, generation, and compilation still run:

```sh
gosvm test --svm -svm-fixtures "$PWD/testdata/svm.json" \
  -svm-runner /absolute/path/gosvm-svm-runner
```

For an already-compiled SBFv3 ELF, without requiring a generated project:

```sh
gosvm svm-test -elf build/program.so -fixtures testdata/svm.json \
  -runner /absolute/path/gosvm-svm-runner -report build/svm-results.json
gosvm svm-test -elf build/program.so -fixtures testdata/svm.json \
  -runner /absolute/path/gosvm-svm-runner -run '^stateful-swaps' \
  -report build/selected-results.json
```

`svm-test` executes the supplied artifact; build it and run native tests separately.
Selection is explicit runner flag, `GOSVM_TEST_RUNNER`, verified managed cache,
then PATH. See [local installation](SVM_RUNNER_PACKAGING.md). Current pin:
runner 0.5.0, LiteSVM 0.8.2 with the documented rent-error patch. No command downloads dependencies. File arguments are
relative to the command's working directory; additional ELF paths inside a
fixture are relative to that fixture's directory.

[`examples/typed-swap/testdata/svm.json`](../examples/typed-swap/testdata/svm.json)
is a complete small example covering a swap, exact failure, and atomic rollback.
The [full token corpus](../results/svm/2026-10-05-general-complete/token-fixtures.json)
covers eight-account token CPIs and multi-transaction scenarios. It uses the
matched wire adapter in `build/anchor/go-token.so`; use that exact artifact.

## Fixture fields

| Field | Meaning |
| --- | --- |
| `format` | Exact string `gosvm-svm-fixtures-v1` |
| `program_id` | Canonical base58 address for the supplied main ELF; alias `program` |
| `payer_seed` | 32-byte hex Ed25519 seed for the deterministic local fee payer; alias `payer` |
| `signers` | Additional `{name, seed}` test signing keys |
| `programs` | Additional `{name, address, elf, sha256}` images; checksum required |
| `native_programs` | `{name, address}` aliases for the native System program; only `11111111111111111111111111111111` is supported |
| `accounts` | `{name, address, initial?}` account declarations |
| `sysvars` | Optional complete typed Clock/Rent objects from the [runner protocol](SVM_RUNNER_PROTOCOL.md) |
| `cases` | Named scenarios with optional `overrides` and ordered `steps` |

Aliases contain letters, digits, underscores, and hyphens, starting with a letter.
Addresses must be unique; reuse one alias for duplicate instruction references.
An account declaration may share an alias/address with its signer. The payer's
initial balance is the runner's fixed local-test budget of 1,000,000,000,000
lamports. To test another payer balance, declare a `payer` account without
`initial` and supply a scenario override.

An `initial` account state contains hex `data`, base58 `owner`, `lamports`,
`executable`, and `rent_epoch`. Owner is required; other omitted state fields
default to empty data, zero lamports/epoch, and false executable. Use explicit
values for inspectable expectations. Omit `initial` to reference an existing VM
account or an address that should be created by a transaction. Executable images
belong in `programs`; use typed controls for sysvars.

Every seed in these fixtures is local test material. The runner signs locally and
uses no wallet, remote RPC, or public cluster.

## Scenarios and expectations

Each scenario starts by restoring the initial VM checkpoint and applying its
`overrides`: `{name, state}` ordinary-account replacements. Overrides are applied
to a temporary clone; rejection leaves current state and checkpoint unchanged.
Programs and sysvars cannot be replaced through account fixtures or overrides.

Each step is one transaction with 1–8 ordered `instructions`. An instruction
declares its program alias, hex data, and ordered account references:

```json
{"program":"program","accounts":[{"account":"pool","signer":false,"writable":true},{"account":"authority","signer":true,"writable":false}],"data":"00000000"}
```

Duplicate account references remain duplicated in the instruction. The message
contains one key per public key and merges signer/writable privileges, as a
Solana client does. Every requested signer must have a declared test seed. Steps
share state, fees, statuses, and transaction history; the following scenario
starts from the checkpoint again.

`expect` requires an explicit `error` (`null` for success, or the exact transaction
error JSON) and nonempty account assertions. A check is `{account, state}` for
complete bytes/owner/lamports/executable/rent-epoch equality, or
`{account, absent:true}`. Every writable instruction account except the fee payer
needs a state assertion. Successful simulated state and submitted state are
checked; failed transactions check exact errors and persisted rollback state.
Unchanged watched accounts outside the message retain their pre-transaction state.

Successful steps may declare `simulation_accounts`, a list of state assertions
overriding the shared checks for those same watched accounts. Submitted checks
remain mandatory and unchanged. This represents closing exactly: simulation
returns a zero-lamport System-owned account record; committed reads return no
account. Failed simulations expose no checked post-state, so these overrides
are rejected for failed steps.

Optional `cu` requires exact simulated CU. Without it, nonzero compute is
required; explicitly set `cu:0` for an intended pre-execution failure. Optional
`log_counts` maps a nonempty substring to the exact number of log lines containing
it. This can assert CPI invocation/success counts. Reports retain the simulation
logs. Failure messages identify scenario, step, account, and first differing byte
or metadata.

Repeated identical transaction contents under the same blockhash have the same
signature and can trigger duplicate detection. A step can explicitly request
`fresh_blockhash:true` before constructing its transaction. This calls the
runner blockhash-expiry control and permits a state-dependent retry to have a
new signature. Steps otherwise preserve hashes and transaction data. Fixture selection selects a whole
scenario, including all its ordered steps.

An optional step `sysvars` object sets Clock and/or Rent before transaction
construction, using the same complete fields as suite initialization. Omitted
sysvars retain their current values; later steps inherit the update until another
control or scenario reset. Add this field alongside `instructions` and `expect`
in a transaction step, using the program's actual state expectations:

```json
{
  "sysvars": {
    "clock": {
      "slot": 42,
      "epoch_start_timestamp": 1700000000,
      "epoch": 1,
      "leader_schedule_epoch": 2,
      "unix_timestamp": 1700000123
    }
  }
}
```

Controls are host actions before the transaction, so transaction
failure rolls back application writes but retains the controls. Updating Clock
does not implicitly change blockhash, transaction history, SlotHashes or other
sysvars; request `fresh_blockhash` separately when needed. Every supplied Clock
or Rent must have exactly its complete non-null fields, with valid typed ranges.
Unknown/duplicate fields are rejected. Step controls must include at least one
sysvar; empty/null objects fail. The whole suite is validated before selection
or execution, including controls in unselected scenarios.

If the suite contains any step controls, each reported step includes its effective
Clock/Rent values, including inherited values and reset-only scenarios. Integer
precision is preserved. Suites without step controls keep the earlier report
shape. [The stateful control corpus](../results/svm/2026-10-05-scenarios-complete/README.md)
checks real syscall output and serialized account bytes, rollback and isolation.

## Reports, limits, and validation

Successful reports include scenario/step results, exact errors/CU/logs, ELF and
dependency hashes, fixture/runner hashes, feature IDs/sysvars, and separate
startup/execution timing. `committed:true` records checked submission/status,
including expected transaction failures. Application writes from failed
transactions are rolled back. No bank/consensus confirmation is implied.

A previous SVM report is removed before execution. Standalone output refuses to
overwrite input files, their hard links, or unrelated existing files. A failure
cannot leave an earlier passing report at that output path.

Bounds: fixture file 16 MiB, each account's hex-decoded data 1 MiB, 4,096 account
declarations, 1,024 scenarios, 64 steps/scenario, 32 references/instruction,
64 state checks/step, 64 signers, and the 1,232-byte transaction packet limit.
These are harness limits; programs/runtime impose their own stricter bounds.
Runner frames remain limited to 16 MiB. Transactions use the legacy message
format; lookup tables are not implemented. Clock/Rent controls are explicit host
actions rather than bank/consensus advancement.
Additional SBF instruction programs require pinned ELF images; the native System
program has an explicit alias. Each step shares its exact error expectation
between simulation and submission; error phase overrides are not implemented.
Transactions whose errors differ between phases, including LiteSVM duplicate
detection, fail these shared error assertions. The lower-level session API remains available for those
tests. General fixture execution currently uses LiteSVM.

The [validation experiment](../results/svm/2026-10-05-general-complete/README.md)
compares all 105 Go corpus cases with a fresh validator through the independent
benchmark harness, then runs 108 declarative scenarios/110 transactions three
times. Expected account bytes derive from genesis files and independent math.
That experiment used seeded accounts. The later
[lifecycle experiment](../results/svm/2026-10-05-lifecycle-verified/README.md)
checks transaction-created mints/token accounts, a Go swap, draining, closing,
recreation, rent rejection, and rollback. It explicitly records a `rent_epoch`
metadata difference and the runner rent-error precedence patch. General bank
rent collection, other hosts, and a supported distribution remain unproven. Full-validator starter tests and the
independent token harness remain available.
