# Experimental SVM runner protocol

October 5, 2026. Use a shared LiteSVM process with ordered JSON batches over
stdio for the first fast-testing workflow. Keep the runner protocol experimental
and full-validator testing available. This decision follows a controlled
[transport comparison](../results/svm/2026-10-05-transports/README.md), not an
assumption that process boundaries are free.

## Decision and evidence

All six combinations of HTTP, stdio, and embedding, with batches of one or 64
calls, reproduced complete fixture responses in seven seeded shuffled
repetitions. The same LiteSVM 0.8.2/Agave 3.0.10 implementation served every
transport. The captured HTTP fixtures also matched the earlier validator
snapshot on errors, CU, application state assertions, token ELF, and feature IDs.

Token replay took a median 147.9 ms through stdio batches and 140.2 ms through
embedded batches. The bounded Go starter took 4.84 and 3.18 ms. Initialization
also favored embedding by a few milliseconds. These are small enough differences
to favor simpler deployment and process isolation for this initial workflow.
JSON serialization and complete response validation remain in both variants.

The embedded experiment uses a small C ABI and dynamic library around our same
Rust core. It measures that boundary, not the separate upstream pure-Go engine.
The current [upstream Go project](https://github.com/LiteSVM/litesvm-go) now uses
Mithril and requires Go 1.25.7; integrating it would change both the engine and
the project's pinned Go toolchain. Evaluate its SBFv3/CPI/rollback compatibility
separately before comparing developer workflows.

Stdio keeps cgo, dynamic loading, and native-link ownership out of the project
CLI. A pinned local runner package can now be installed once and shared by
projects; see [installation and release limits](SVM_RUNNER_PACKAGING.md). A VM
process failure can be reported without crashing the Go CLI. Public signing/download
distribution and additional hosts have not been verified yet. Keep embedding available as
an experimental benchmark, rather than imposing it on all project builds.

## Version 1 envelopes

Launch the binary with `--stdio`. Its version must match the CLI's experimental
runner pin. Each UTF-8 JSON envelope is terminated by one newline, with schema
1 and an unsigned request ID. Stdout contains protocol frames; stderr contains
process diagnostics. Maximum request/response size in the Go client is 16 MiB.

The current runner pin is 0.5.0 (LiteSVM 0.8.2 + rent-error patch). The
[local source patch](../third_party/litesvm-0.8.2/GOSVM_PATCH.md) preserves an
instruction failure before post-execution rent validation. Atomic account
overrides from 0.4.0 remain supported. Earlier runners are rejected by this CLI. The schema-1
envelope remains experimental; the exact binary version is the compatibility gate.

```json
{"schema":1,"id":0,"op":"init","config":{"features":[],"payer":"base58 key","accounts":[],"programs":[]}}
```

An initialization config supplies an explicit feature-ID list, a local fee-payer
key, account fixtures, and program images. An account entry is `pubkey` plus an
`account` object containing base64 data, owner, lamports, executable, and rentEpoch.
A program entry contains its base58 `id` and base64 `elf`. The abbreviated example
above illustrates envelope shape; its placeholder key is not executable input.
Initialization happens once per session and reports `{"ready":true}` on success.

```json
{"schema":1,"id":1,"op":"batch","calls":[{"jsonrpc":"2.0","id":42,"method":"getLatestBlockhash","params":[]}]}
```

The batch result is an ordered array of per-call result/error objects with their
original call IDs. The CLI checks envelope schema/identity, array length, every
call ID, and every adapter error. Batches contain at most 4,096 calls; the current
CLI groups at most 64. IDs are echoed, not used for asynchronous multiplexing.
Calls run sequentially, including stateful scenarios.

The compatibility adapter currently supports blockhash/account reads, simulation,
submission, status lookup, and runtime metadata. It is not general Solana RPC.
Readiness/confirmation are synchronous test markers. This call vocabulary is an
internal bridge for existing fixtures, not a stable application/client API.
Unknown session operations, unsupported methods, or unknown features fail.

Transactions retain VM atomicity: a failed instruction/CPI rolls back that
transaction's application state. A batch is **not** atomic. Earlier successful
transactions in a batch remain applied if a later transaction fails. Independent
starter cases restore the initial VM checkpoint before each fixture, with
instruction ordering within each transaction preserved. EOF closes the runner. Cancellation terminates the
owned child; the CLI does not leave a background VM process behind.

## Typed sysvars and checkpoints

Initialization accepts an optional `sysvars` object. `set_sysvars` accepts the same
object after initialization; `get_sysvars` returns both current typed values.
General fixtures now expose this operation as an optional step `sysvars` object
before its transaction; see [step semantics](SVM_FIXTURES.md). This uses the
unchanged 0.5.0 runner binary and pinned package.
Only Clock and Rent are supported. An included value must provide every field:

```json
{"schema":1,"id":2,"op":"set_sysvars","sysvars":{"clock":{"slot":42,"epoch_start_timestamp":1700000000,"epoch":1,"leader_schedule_epoch":2,"unix_timestamp":1700000123},"rent":{"lamports_per_byte_year":3480,"exemption_threshold":2.0,"burn_percent":50}}}
```

Clock timestamps are signed int64; slot/epoch values are uint64. Rent has a uint64
rate, finite nonnegative floating threshold, and integer burn percentage 0–100.
Omit either complete value to leave it unchanged. Unknown fields/sysvars,
incomplete values, and invalid ranges fail before either sysvar changes. Typed
writes update both the serialized account and VM syscall cache.

`{"schema":1,"id":3,"op":"get_sysvars"}` returns a `result` object with
`clock` and `rent`. These values also appear in runtime metadata and fast reports,
with full integer precision. The legacy adapter's `getSlot` remains a readiness
marker; use `get_sysvars` for the controlled clock.

`snapshot`, `reset`, and `expire_blockhash` use envelopes without a payload and
return `{"done":true}`. Init creates one checkpoint; snapshot replaces it with
the current VM and adapter submission statuses. Reset restores that checkpoint,
including accounts/lamports, sysvar cache, loaded programs/features, transaction
history, statuses, and latest blockhash. Repeated reset reuses the checkpoint.
Execution/request counters and timing remain cumulative session diagnostics.
Checkpoint state is in memory within one process; no disk format is promised.

Reset optionally accepts `accounts`, using the same entries as initialization.
It restores a temporary checkpoint clone and applies ordinary account replacements
before committing the VM/status reset. Any invalid entry rejects the entire
operation. Programs and sysvars are protected; initial ordinary-account fixtures
also cannot replace them. The Go client exposes this as `ResetAccounts`.
General fixture scenarios use it to isolate overrides, then execute ordered steps
without resetting between transactions. General steps may explicitly request
a fresh blockhash, and successful simulations may have distinct full-state
assertions from committed reads. See [the fixture format](SVM_FIXTURES.md).

The transaction-history capacity is explicitly configured to 4,096 on new and
cloned VMs. LiteSVM's history limit depends on map capacity; cloning an empty map
otherwise loses spare capacity and disables duplicate detection. Replay tests
check duplicate rejection before reset and successful replay after reset.

Changing Clock sets only the fields supplied; it does not advance consensus,
epochs, SlotHashes, or blockhash validity. Explicit `expire_blockhash` rejects the
old blockhash; reset can restore it. Rent control does not establish full bank
rent collection, lifecycle, or fee compatibility. See
[compiled-probe evidence](../results/svm/2026-10-05-controls-final/README.md).

## CLI workflow

`gosvm test` runs native Go tests. `gosvm test --svm` additionally compiles the ELF
and runs the schema-1 fixtures through one stdio session. `gosvm test --sbf`
retains the full-validator path. Choose one integration mode per command.

```sh
gosvm test --svm -svm-runner /absolute/path/gosvm-svm-runner
gosvm test --svm -svm-run 'rollback$' -svm-runner /absolute/path/gosvm-svm-runner
gosvm test --svm -svm-sysvars /absolute/path/sysvars.json -svm-runner /absolute/path/gosvm-svm-runner
```

Selection order is an explicit runner flag, `GOSVM_TEST_RUNNER`, a verified
managed installation, then an executable on PATH. A damaged managed installation
fails rather than silently falling back. Normal tests never install tools. See
[the pinned local installer](SVM_RUNNER_PACKAGING.md).
No test silently downloads or builds it. `-svm-run` matches fixture names with a
Go regular expression; invalid expressions or zero matches fail rather than
reporting an empty passing suite. Native Go flags still pass after `--`.
`-svm-sysvars` reads the Clock/Rent object above (without envelope fields) and
applies it before capturing the initial checkpoint. Every fixture starts from
that same VM state. These controls apply only to `--svm`.

The CLI starts from an embedded snapshot of the observed 237 local validator
3.0.15 feature IDs. This is not a mainnet feature policy. `GOSVM_TEST_FEATURES`
can supply a different explicit JSON array; unknown runner features fail.
Activation slots are normalized to zero. Other sysvar controls remain future
work, and runtime versions still differ from validator 3.0.15.

Successful runs save `build/svm-results.json` with selection, per-case CU/errors,
ELF/fixture/runner hashes, runtime metadata, and timing. The report is removed
before a new fast run so failure cannot leave an old passing report. Failed state
checks identify the first differing byte or differing lengths. Submitted state
means in-memory VM persistence, not cluster confirmation. Fast tests never use a
wallet or public cluster.

## Remaining gates

Add other needed sysvars, broader initialization/rent compatibility,
checked runner downloads/signing, and supported
host validation. Review protocol compatibility/versioning before public alpha.
The compiler and framework remain separate roadmap work; this protocol does not
expand the Go language subset or implement schema-2 account bindings.
