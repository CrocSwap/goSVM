# Complete developer workflow measurements

October 5, 2026, macOS arm64, Go 1.22.0, platform-tools v1.51 and runner 0.5.0
(LiteSVM 0.8.2 plus the documented rent-error patch). All 60 measured workflows
passed, with unchanged fixture outcomes, CU, errors and logs. This supplies the
first complete edit/test latency measurements for milestone 1; distribution,
supported-host and broader runtime work remain open.

## Command latency

Five serial repetitions per cell, with workload order shuffled using seed
20261005. Values are median external command wall seconds. These are observations
on a shared desktop with installed tools and a warmed native compilation cache.

| Workflow | Bounded starter, 14 cases | Full token swap, 108 scenarios |
| --- | ---: | ---: |
| Empty project build directory | 0.288 | 0.533 |
| No source or test change | 0.179 | 0.269 |
| Equivalent source refactor | 0.229 | 0.533 |
| Source refactor and native tests forced with `-count=1` | 0.286 | 0.536 |
| Test edit | 0.338 | 0.536 |
| Native tests forced, SBF artifact unchanged | 0.229 | 0.425 |

The bounded project runs generation, native tests, backend checks, SBF build and
all fast fixtures in one `gosvm test --svm -timings` command. The full token
workflow sums three command wall durations: native tests, SBF build and
`gosvm svm-test`. It tests the original Go business logic and separately builds
the previously validated Anchor wire adaptation. It is a manual workflow, not
generated multi-account bindings. The dependency-heavy protocol and proposed
external-program port are separate workloads and were not measured here.

Source refactors rename a local variable without changing semantics. Both workloads
rebuilt SBF, retaining the exact ELF hash, while Go reused native test results.
The combined `-count=1` variant proves native execution as well as SBF rebuild.
Test edits add a uniquely named zero-input assertion; each invalidated the native
result cache and retained the SBF artifact. Empty project outputs retain the
native Go cache. Cache receipts, ELF timestamps and hashes verify these conditions.

For the combined source-edit/forced-test run, median bounded phases were 0.168 s
native tests, 0.047 s SBF build and 0.020 s SVM tests. Full-token command phases
were 0.275 s, 0.130 s and 0.176 s respectively. Phase medians need not add to the
median whole workflow. Internal CLI timings exclude process startup, initial
argument parsing/project discovery and teardown; external wall measurements include
them. Runner startup, fixture time and VM execution are recorded separately.

Five paired, shuffled starter controls measured 0.178 s without diagnostics and
0.180 s with diagnostics. Individual paired differences range from -5.4 to +3.7
milliseconds. This sample does not isolate overhead below desktop scheduling noise.
The earlier [pilot](../2026-10-05-workflow/README.md) used sequential controls;
it should not be used to attribute their timing difference to instrumentation.

## Bootstrap and footprint

| Setup operation | Seconds |
| --- | ---: |
| CLI source build with a new empty Go cache | 5.689 |
| CLI source rebuild with that cache | 0.290 |
| Runner host rebuild with existing target and cached crates | 0.703 |
| Scaffold | 0.022 |
| First bounded project workflow after CLI bootstrap | 4.757 |
| First full-token workflow after the bounded workflow | 0.814 |

First project runs are sequential and share the cache: the bounded run also
compiles native testing dependencies absent from CLI bootstrap. The full-token
first run benefits from those dependencies. Neither is an independent fresh-machine
installation comparison. Tool downloads and backend installation are excluded.
The initial runner empty-target build remains the historical compatibility-spike
measurement; the runner was not rebuilt cold in this experiment.

The bounded ELF is 3,216 bytes; the matched token ELF is 8,064 bytes. Final project
build directories, including reports and receipts, are 18,907 and 124,560 bytes.
The shared Go cache is 151,526,175 bytes, and the retained runner host target is
445,266,765 bytes. The runner binary is 6,886,240 bytes. These categories are
separate: small project artifacts do not imply a dependency-free bootstrap.
Installed Clang/LLD and frontend binary sizes and hashes are in the summary.
Basanos source and outputs were untouched.

## Evidence and reproduction

- [Summary](summary.json): raw samples, ranges, phase distributions, sampling
  order, setup, footprint, source and binary hashes.
- [Generated and staged input hashes](input-files.json) and [edit plan](edit-plan.json).
- Representative forced-edit results: [bounded](bounded-source_edit_uncached_native-0-results.json)
  and [token](full_token-source_edit_uncached_native-0-results.json).
- Corresponding timing records: [bounded](bounded-source_edit_uncached_native-0-timing.json)
  and [token](full_token-source_edit_uncached_native-0-timing.json).
- [Diagnostic success and failure checks](diagnostic-checks.json): native-only,
  build and SVM success; generation, native and backend failures; stale SVM report
  removal and restored success. Failed commands emit `passed:false` with only
  attempted phases.
- [Repository validation](checks.json): root tests/vet, separate starter tests,
  source hash verification and local documentation links.

Every SVM run checks 14 or 108 cases, exact equality with its initial result,
runner/ELF hashes, expected cache behavior and historical matched token results.
This is a regression against captured validator-compatible fixtures; it does not
add a fresh validator latency comparison. Restricted local RPC binding prevented
that measurement. Full-validator tests remain available separately.

From the repository root with the installed pinned tools and runner:

```sh
python3 scripts/svm_workflow.py --output results/svm/new-workflow --samples 5
```

The script refuses existing output/staging directories, uses a new local Go cache,
builds its own frontend, stages both projects, verifies that checkout source hashes
remain unchanged, and restores staged source/test files after measurement.
The diagnostic checks are additional validation recorded for this snapshot.

Provisional local regression targets are median **at most 0.5 s** for the bounded
starter and **at most 1.0 s** for the full-token combined source-edit/forced-test
workflow under these cache/tool conditions. Both meet those targets. Reassess
them on supported hosts and representative larger programs; they are neither
user-set release requirements nor portable latency guarantees.
