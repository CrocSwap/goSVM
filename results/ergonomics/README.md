# First developer workflow slice

This is the original snapshot. See [installation/workflow polish](../polish/README.md)
for the subsequently implemented managed backend installer and CLI improvements.

Measured October 4, 2026 on the shared Apple M2 desktop, using Go 1.22.0,
Solana platform-tools v1.51 / SBF v3, and validator 3.0.15. This is a small
bounded arithmetic AMM starter, not the token-CPI or Basanos-shaped workload.

## Implemented workflow

```
bash scripts/install.sh "$HOME/.local/bin"
gosvm doctor
gosvm new my-swap
cd my-swap
gosvm check
go test ./...
gosvm build
gosvm test --sbf
```

The installer builds a standalone CLI from source. Its templates and narrow SDK
are embedded, so scaffolding has no dependency on the compiler's checkout. The
starter is a separate Go module with a checked local SDK snapshot. Offline native
tests passed with `GOPROXY=off` and `GOWORK=off` in a temporary directory outside
the repository. An inspectable copy is in `examples/typed-swap` (a nested module).

The compiler now handles multi-file packages, named unsigned types, and nested
value structs. The project generator binds a typed handler to one writable,
nonexecutable, program-owned state account. It generates byte codecs, checks,
a Go client, and a custom JSON IDL. It checks the entire on-chain subset before
writing generated artifacts and preserves unchanged generated files' mtimes.

## Observed results

| Operation | Time |
|---|---:|
| Create standalone project | 18 ms |
| Check/generate project | 13 ms |
| Clean SBF build, median of 3 | 189 ms |
| Edit SBF build, median of 3 | 162 ms |
| No-op SBF build, median of 3 | 57 ms |
| Native tests, forced execution, median of 3 | 744 ms |
| Native tests + build + local validator integration suite | 9.78 s |
| CLI source bootstrap, empty isolated Go build cache | 13.38 s |

Clean/edit samples were 147–322 ms; this machine is not isolated benchmark
hardware. A clean contract build deletes the project's build output, retaining
installed tools and warm OS caches. Native test timings allow the shared Go
compilation cache. Bootstrap includes external linking and ad-hoc signing on
macOS. Downloads are disabled/excluded. All raw commands and timings are in
[`benchmark.json`](benchmark.json); command output is preserved alongside it.

The **3,216-byte ELF** uses **486 CU** for one successful swap. The build keeps
only that ELF and a **153-byte receipt** (3,369 bytes total) before test reports
and logs. The stripped CLI is **7,000,608 bytes** (6.68 MiB), including the native
RPC test runner. Project source, tests, generated client, IDL, and SDK snapshot
are small; exact scaffold and bootstrap-cache sizes are in the raw report.

The old bounded AMM reported 351 CU, but it used a different account adapter and
had no wire discriminators. The new result adds 135 CU; it is not an isolated
measurement of struct or code-generation overhead. There is no equivalent Rust
framework benchmark in this slice. The existing full-width token-CPI regression
still reports 20,321 CU for Go versus 20,380 CU for Rust on its wide-swap case.

## Validation

- 1,000 native generated-entrypoint cases against arbitrary-precision arithmetic.
- Native invalid-account/owner/flags/length/discriminator and handler-error tests,
  checking that failed instructions do not write state.
- All **14 actual-SBF fixtures** simulated and submitted on a new local validator;
  checks include two persistent swaps and atomic success-then-failure rollback.
- SBF result file binds measurements to the ELF and fixture SHA-256 hashes.
- New compiler tests cover nested value copies, evaluation order, zero values,
  named integer wrapping, deterministic multi-file output, cross-file recursion,
  unsupported types, and added/edited/deleted source cache invalidation.
- Generated clients also compile for unaligned byte/uint32/uint64 wire fields.
- Root `go test ./...` and `go vet ./...` pass. Nested starter tests pass separately.
- Existing token-CPI regression: **206 simulations, 4 committed swaps, 4 rollback
  checks**, with unchanged CU for the reported wide-swap case.

## Remaining installation and framework work

This is not yet a frictionless distribution. First installation still needs Go,
host Clang on this Mac, and the existing large platform-tools download. The
source installer does not download or verify a minimal Clang/LLD release bundle.
`doctor` checks the expected LLVM version; it does not cryptographically attest
backend provenance. A signed release, checksummed backend acquisition/cache,
Linux verification, and upgrades remain work to do.

The binding format has one handler and one account, flat unsigned wire fields,
and explicit numeric errors. It has no generic signer/PDA constraints, account
initialization, token custody layer, TS client, Anchor compatibility, deployment
command, user package imports, heap, or arrays. Reordering wire fields changes
persistent layout and requires a deliberate migration. The stateful sample is
an arithmetic experiment, not a production AMM.

Run `python3 scripts/ergonomics_bench.py` to reproduce this isolated workflow and
refresh these raw measurements. Previous protocol benchmark snapshots are
preserved separately and describe their historical source/tool versions.
