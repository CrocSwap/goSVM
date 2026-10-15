# Package-level variables and `init` on SBF: fogo-v0 and solana-v3

October 14, 2026. Local runners only (`network_validation: false`).
Semantics: [GO_GLOBALS.md](../../../docs/GO_GLOBALS.md).

## Question

Do package-level variables and `init` functions behave on SBF exactly as in a
freshly started native Go process, on every invocation: Go's initialization
order (dependency order within a package, imported packages first in Go 1.21's
package order, `init` functions in file and source order, blank and
multiple-value initializers), variables of every kind, mutation that persists
for the rest of the invocation, closures, method values and function-valued
variables across packages, read-only tables, and traps that abort and roll
back? What does initialization cost per invocation, and do programs without
package-level variables stay byte-identical?

## Method

[examples/global-values](../../../examples/global-values/program.go) has five
packages: `log` (an initialization trace), `zeta`, `beta`, `alpha` (two
files) and the entry package (two files): 63 named variables (11 of them
read-only), a 616-byte globals block and 7 `init` functions. Its `Run(out, in)`
dumps the initial state (mode 0) or runs operation scripts, table lookups,
function values, errors and maps, cross-package mutation and heap use
(modes 1 to 6). Native Go keeps variables for the life of a process, so the
native oracle ([oracle/main.go](../../../examples/global-values/oracle/main.go))
runs **one fresh process per vector**, as every SBF invocation is fresh.

- `TestGlobalsDifferential` (`internal/compiler/globals_diff_test.go`):
  2,042 vectors natively and as host-compiled C with the default 32 KiB heap.
- `TestGlobalsSBF` (`internal/compiler/globals_sbf_test.go`): builds the
  five-package program with a Context entrypoint for each target with
  platform-tools v1.57 and runs 241 vectors plus an idle invocation through
  the pinned runners (fogo-v0: gosvm-svm-runner 0.5.0 / LiteSVM 0.8.2;
  solana-v3: 0.6.0 / LiteSVM 0.13.0), 1,400,000 CU limit. Completed vectors
  must leave the native output bytes; vectors that panic natively must fail
  with `ProgramFailedToComplete`, log `SBF program panicked` once and roll
  back. It also builds seven probe programs whose `Process` returns at once,
  so their CU above the baseline is the cost of initialization.

```sh
GOSVM_GLOBAL_RESULTS=/tmp/global-results go test -ldflags=-linkmode=external \
  -run 'TestGlobals' ./internal/compiler/
```

(macOS needs the external linker flag; `make test` runs both tests when the
v1.57 backend and runners are installed and skips the SBF one otherwise. Here
the runners were installed from the 0.5.0 and 0.6.0 packages into a scratch
`GOSVM_CACHE`.) Fixtures, ELFs and runner reports stay local;
[metrics.json](metrics.json) keeps the summaries.

## Results

All 2,042 host vectors match fresh native processes (600 Go panics, each an
abort at the same point: modes 2 to 5 index past tables, call nil function
values, write nil maps and fail assertions). All 242 SBF cases pass on both
targets (65 aborts with rollback). The initialization trace is
`LbBCzZa2AD4231_mI` on every path: `log`, then `beta` before `zeta` (Go 1.21:
both depend only on `log`, `beta` sorts first), then `alpha` (needs `zeta`;
`alpha.go` before `alpha2.go`), then the entry package's variables in
dependency order `4 3 1 2`, the blank `_`, the multiple-value `m`, and `init`.

| | fogo-v0 | solana-v3 |
| --- | --- | --- |
| Example ELF SHA-256 | `f48ad1eb…` | `764a2ec6…` |
| Idle invocation (initialization + entry) | 2,264 CU | 2,268 CU |
| Initialization of the whole example | 2,179 CU | 2,183 CU |
| Highest vector | 43,429 CU | 43,433 CU |

Initialization cost per invocation (probe CU minus the 85-CU baseline;
identical on both targets):

| Probe | CU |
| --- | --- |
| Read-only tables (`[8]uint64`, `[]uint32` of 5, a string) | 0 |
| `var hits [16]uint64` written elsewhere (allocate the 128-byte block) | 14 |
| `var counter uint64 = 5` written elsewhere | 16 |
| Three sentinel errors `var ErrA error = &E{"a"}` | 76 |
| `var registry = map[string]uint64{...}`, 4 entries | 442 |
| `init` filling a `[64]uint64` in a loop | 400 |

Read-only placement makes constant tables free; everything else runs on
every invocation, as Go-exact semantics require.

## Byte identity

`scripts/verify_rebuild.py --reference results/compiler/2026-10-14-v157-rebuild/metrics.json`
with a CLI built from this branch: fresh rebuilds are identical and match the
reference for both targets: full swap fogo-v0 `4697a9f5…`, solana-v3
`2b5f21d9…`; clock2 fogo-v0 `0f768370…`, solana-v3 `54f34b65…`. The frozen
check corpus matches except for the nine recorded package-variable and `init`
programs, which now compile (`corpusChanges`).

## Library shakeout

The [library shakeout](../2026-10-14-library-shakeout/README.md) found
package-level variables to be the largest blocker. Rerunning its pass 2
(standard-library stand-ins; the same sources) with this compiler:

| | Shakeout (`764e4a9`) | This branch |
| --- | --- | --- |
| Declarations removed before the remainder checks | 252 | 185 |
| Rejections "only functions and constants are supported at package scope" | 9 | 0 |

56 declarations are no longer removed. Every remaining removal of a variable
is a cascade of another blocker: `MustFromHex`/`MustFromDec` (which `panic`),
`math/big` and other standard-library imports, type aliases and arrays of
pointers (`[5]*Int`). The peel log (`shakeout-pass2.jsonl`) stays local, like other raw reports; [metrics.json](metrics.json) keeps the counts.
