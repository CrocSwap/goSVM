# SDK-2 checked-token/PDA/CPI proof

October 5, 2026, macOS arm64. [Summary](summary.json): PASS. This is a preloaded
compiler fixture, separate from the bounded starter, manual full token swap and
dependency-heavy protocol workloads. It does not establish performance ratios
against those programs or a completed milestone 2.

The proof passed 116 native-Go vectors, 1,011 native-Go/generated-C CPI-boundary
vectors and 117 compiled-SBF scenarios in three repetitions, including 15 expected
failures. Independent Python wire/arithmetic/PDA expectations are retained in
[independent-expectations.json](independent-expectations.json).

Coverage includes wide and zero token amounts, checked references, current token
balances after CPI, explicit little-endian/key/bump seeds, two signer groups,
real classic SPL Token and System transfers, owner/mint/authority/privilege/layout
failures, bad PDA seeds, downstream insufficient-funds/overflow/frozen errors and
whole-transaction rollback after an earlier successful transfer/instruction.
Native callbacks are not the proof of runtime atomicity: SBF cases execute the
actual ELF through runner 0.5.0 (LiteSVM 0.8.2 plus its provenance-bound rent patch).

The 14,352-byte ELF has SHA-256
`48e227c1df8e732a22e2ab660ebd1314579ab8e41f52286491a17a27d79b058a`.
Instrumented compilation/linking reproduces that ELF hash. Its largest static
frame is the 3,712-byte entrypoint; [stack usage](stack-usage.tsv) is retained.
The summary binds source, fixture and runner hashes, commands and logs.

The CLI scaffold/check/native-test path explicitly selects schema 2 and SDK 2.
Existing-workload [regression evidence](../../svm/2026-10-05-m2-sdk2-regression/summary.json)
retains prior starter/full-token/lifecycle/sysvar ELF hashes and outcomes.
Root `make test` and `go vet ./...` also passed after the SDK-version/native-type
and 16-account-limit guards were added. Root tests do not discover nested modules;
the scaffold's native tests are recorded separately in this proof.

There is no fresh-validator comparison, clean-host result, account initialization,
resizing/closing authoring proof, full swap migration or external developer trial
here. See [API scope](../../../docs/SDK2.md). Historical prototype results remain
untouched; this directory includes additional owner/writable/program negatives.

Reproduce current sources into a new directory:

```sh
python3 scripts/checked_token_verify.py --output results/compiler/new-sdk2-proof
```

This requires installed v1.51 LLVM and the pinned local runner archive recorded
by the script; normal builds do not download either implicitly.
