# Generated full swap migration

October 5, 2026, macOS arm64. [Summary](summary.json): PASS.
This is the full-width classic-token swap with preloaded accounts, distinct from
the bounded starter and dependency-heavy protocol experiment. Generated lifecycle policy, full application lifecycle and
escrow remain milestone-2 requirements; this proof does not close that milestone.

The [application](../../../examples/full-swap/README.md) uses canonical shared
Go types/math, generated packed codecs/account validation and checked token/PDA
helpers. Its handler contains no wire offsets. Eight-account ordering, pool and
instruction bytes agree with the preserved matched manual/Anchor corpus, using
explicit application discriminators. General Anchor IDL compatibility is not
claimed. The manual source and all historical results remain unchanged.

## Correctness and authoring

- All 108 existing scenarios/110 transactions pass in three actual-SBF runs,
  retaining state/log/CPI assertions and explicit expected security failures.
  The [error map](error-mapping.json) translates 27 named manual validation cases
  into framework categories; application/SPL errors propagate unchanged.
- 106 native direct-call vectors agree with independent packed-wire and
  arbitrary-precision quote expectations. The loader's account-limit case and
  compound transaction atomicity are covered by SBF rather than native callbacks.
  Native effects after a failed second CPI have separate expectations from
  transaction rollback; neither is silently equated with the other.
- The documented project `test --svm -svm-fixtures` command passes the packaged
  13-case suite against the identical ELF. [Project report](project-core.json)
  records that ordinary CLI path with the runner's built-in classic-token program.
  The full corpus explicitly loads its separately pinned historical Token ELF.
- The independent ordinary-Go service imports the actual swap types, generated
  account/instruction codecs and shared quote. Its isolated dependency list has
  no goSVM compiler/runtime package, and no SDK replacement is needed. Independent
  common wire vectors and 1,000 quote inputs pass; the application's native model
  retains the manual baseline's 100,000 quote and 100,000 mul/div tests plus edges.
- Generator diagnostics reject malformed/mixed seeds, wrong widths/selectors,
  inappropriate PDA signers/programs, bad token declarations, invalid key relations
  and reference collisions before altering generated artifacts. PDA-only adapter
  imports, nested scalar seeds, literal/empty seeds and token-field comparisons
  are checked. Native/SBF swap fixtures validate key/bump generation end to end.
- The explicit SDK-2 aligned memory path passes 7,680 alignment/count/value
  checks under UBSan and 1,000 native-Go/C array-value comparisons. Unaligned
  addresses/tails retain byte copies; SDK-1 and unpinned compiler memory output
  retain their historical path. Full SBF checks cover the resulting application.

## Runtime and footprint

| Matched full swap | Manual Go baseline | Current generated Go |
| --- | ---: | ---: |
| Wide swap CU | 20,329 | 45,941 |
| ELF bytes | 8,064 | 21,536 |
| Largest static frame | Separate historical evidence | 3,456 bytes |

Both retain two real SPL Token transfers, consuming 9,290 CU within the pinned
Token program. The current generated ELF has SHA-256
`697f57b3b3512c3baa25785822c765239eb9861feb8fc852af94fb9de0420b13`;
stack-instrumented linking reproduces its hash. [Stack usage](stack-usage.tsv)
retains each static frame; none exceeds 4,096 bytes. This initial framework still
adds substantial runtime/ELF overhead. No performance parity claim is made.
Single build observations are logged separately, not presented as repeated cold
or edited-build benchmarks. Further framework build/footprint measurements remain
part of the full application gate.

The first complete [binding proof](../2026-10-05-framework-swap-supported/summary.json)
used 91,265 CU/19,968 bytes. [Aligned-copy proof](../2026-10-05-framework-swap-wordcopies/summary.json)
used 56,897 CU/20,232 bytes. Current checked balance reads and transfer validation
avoid copying unused token state while preserving all account checks. Each source
snapshot retains its own hashes/artifacts; these are changed experiments, not
replacements for historical measurements. Earlier failed orchestration pilots
are also preserved and not counted as passing.

Root `make test` and `go vet ./...` passed after these changes. Nested application
and service tests are recorded separately in this proof. The validator oracle is
historical; no fresh-validator, clean-host, public distribution or external
trial-developer result is claimed. Runner 0.5.0 uses LiteSVM 0.8.2 and the recorded
rent-error patch. Account initialization/rent/payer/authority/closing/reuse,
escrow, full lifecycle measurements and developer trials are still required.

```sh
python3 scripts/framework_swap_verify.py --output results/compiler/new-framework-swap-proof
```

Choose a new output name. The script requires installed v1.51 LLVM and the pinned
local runner archive; it rebuilds/scaffolds/checks/tests the application and binds
source, oracle, runtime and artifact identities in the summary/logs.

This rerun uses the lifecycle SDK and preserves the preloaded workload.
The separate [lifecycle proof](../2026-10-05-lifecycle2-supported/README.md)
validates rent/System/owned lifecycle boundaries; it does not provide generated
application initialization/close policy. Historical result directories retain
their own original measurements and source hashes.
