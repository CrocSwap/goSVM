# Outgoing CPI bounds: 64 metas, 4 signer groups

October 19, 2026, macOS arm64, platform-tools v1.57 (fogo-v0, solana-v3), runners
0.5.1 and 0.6.0. Local LiteSVM runs only; no network validation.

`solana.Invoke`, the SDK-2 C runtime (`sol_Invoke`) and `sdk/cpi` now allow 64
account metas (was 16) and 4 PDA signer groups (was 2). Seeds stay at 16 per
group, 32 bytes each, 1,024 encoded bytes. Bounds and their justification are in
[SDK 2](../../../docs/SDK2.md#cpi-bounds); the contract change is in the
[error contract](../../../docs/ERROR_CONTRACT.md) changelog.

## Design

- `sol_Invoke` has 64-entry meta and 4x16-entry seed descriptor arrays and is
  `noinline`: its own frame grows 960 → 2,240 bytes; callers' frames do not.
- `cpi.Metas` keeps 16 records inline and moves them to one 576-byte heap array
  at the 17th `Add`. A fixed 576-byte buffer was measured first and rejected:
  zeroing it cost +324 CU on every 3-meta CPI. A shorter `Add` body (slicing one
  record) was also rejected: it changed LLVM's inlining so that the full swap's
  `go_Process` frame grew 3,904 → 4,480 bytes, a build error on v1.57. The
  committed `Add` keeps the original per-byte indexing.
- `cpi.Signers` keeps its 1,024-byte buffer; only the group count changed.

## Evidence

`TestCPILimitsSBF` builds [examples/cpi-limits](../../../examples/cpi-limits/program.go)
for both profiles (SDK-2 decoder, 29 incoming accounts) and a C callee
([callee.c.txt](../../../examples/cpi-limits/callee.c.txt)) that fails unless it
receives exactly the requested number of accounts, its first `groups` accounts
are signers and every later account is the expected new account or duplicate
(so each meta record's index, including the heap-spilled ones, is checked
on chain). 19 CPI cases plus two direct callee checks pass on both profiles:
3/16/17/22/32/64 metas with 0–4 groups through the builders and the raw
encoding; 65 metas → 3002 (builder) / 2006 (raw); 5 groups → 3003 / 2007.

Native: `sdk/cpi` tests cover 64 records (inline/heap switch at 17), the 65th,
four groups, the fifth, and the 64-meta/4-group callback. The native-Go/C
boundary differential now draws metas up to 66 records and one to five random
signer groups, plus 16/17/22/63/64/65 metas with four full 16-seed groups.

[cu.json](cu.json) has every case. Before = `origin/main` (79cf25c) with the same
harness, old-limit cases only; totals include the 29-account entry decode and
the callee.

| Case | fogo-v0 before | after | solana-v3 before | after |
| --- | ---: | ---: | ---: | ---: |
| builder, 3 metas | 5,136 | 5,146 | 5,091 | 5,101 |
| builder, 3 metas, 1 group | 5,392 | 5,401 | 5,347 | 5,356 |
| builder, 16 metas | 7,073 | 7,096 | 7,030 | 7,053 |
| raw, 3 metas | 4,414 | 4,414 | 4,369 | 4,369 |
| raw, 16 metas, 2 groups | 5,524 | 5,524 | 5,481 | 5,481 |
| builder, 17 metas | 3002 | 7,308 | 3002 | 7,266 |
| builder, 22 metas | 3002 | 8,068 | 3002 | 8,031 |
| builder, 22 metas, 4 groups | 3002 | 9,049 | 3002 | 9,012 |
| raw, 22 metas, 4 groups | 2006 | 6,158 | 2006 | 6,115 |
| builder, 64 metas, 4 groups | 3002 | 15,577 | 3002 | 15,588 |

The 17th meta costs about 210 CU (the allocation, the 144-byte copy and one more
account); later metas about 150 each, mostly runtime and callee.

Existing suites (`gosvm test --svm`): escrow 42 steps (legacy profile), full swap
15 (legacy), tutorial 18 (both profiles) pass unchanged; CU +0 to +14, +21 and
+7. ELFs grow 1,640–1,848 bytes because `sdk/cpi` now reaches the allocator.

Static frames (v1.57; escrow and full swap also v1.51): full swap `go_Process` 3,904 and escrow/tutorial
`Metas` holders 1,408/1,920, all unchanged; `sol_Invoke` 2,240.

[metrics.json](metrics.json) is `scripts/verify_rebuild.py` (now the default
reference): fresh rebuilds are identical; the full swap changed to
`bcd9253e…` (fogo-v0) and `86f0ed15…` (solana-v3), clock2 is unchanged.
The check corpus lists the 33 C hashes the runtime change moved.

## Pre-existing, not caused here

- The checked-token fixture, staged as an SDK-2 project, already fails to build
  on `main` under v1.57 (`entrypoint` 4,544 bytes). The full swap's `go_Process`
  is 192 bytes under the limit, so small SDK changes can flip it.
- With host Go 1.23.2, `TestStdlib*` and `TestFullSwapFacts` (`/tmp` vs
  `/private/tmp` paths) fail on `main` too.

## Reproduce

```sh
GOSVM_CPI_RESULTS=<new dir> make test   # or: go test -run 'TestCPILimitsSBF|TestSDK2InvokeBoundaryDifferential' ./internal/compiler
python3 scripts/verify_rebuild.py --output <new dir>
```
