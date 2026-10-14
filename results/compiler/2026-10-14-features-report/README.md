# Feature report: examples/full-swap (October 14, 2026)

First `gosvm features` report ([schema](../../../docs/FEATURES_REPORT.md)),
produced for the verified-IR backend team to size their IR. Workload: the
generated full token swap example (`examples/full-swap`, schema 2, SDK 2,
target-less legacy project = `legacy-v1.51-v3`), as committed at `dfcb8fc`.
This is a source report only: no compilation, CU or footprint is measured, and
it says nothing about the bounded starter or the dependency-heavy protocol
workload.

## Command

From the repository root (macOS needs the external-linker workaround and an
ad-hoc signature for a manually built binary):

```sh
go build -ldflags=-linkmode=external -o build/gosvm ./cmd/gosvm
codesign --force --sign - build/gosvm   # macOS only
build/gosvm features -dir examples/full-swap -o results/compiler/2026-10-14-features-report/full-swap.json
```

The project's existing `zz_gosvm.go` was read as-is (no `-generate`). Two runs
produce identical bytes (`TestDeterministicOutput`).

## Headline numbers

| | handwritten | generated (`zz_gosvm.go`) |
| --- | --- | --- |
| files / lines | 4 / 264 | 1 / 2,555 |
| SDK call sites | 30 | 189 |
| distinct SDK functions/methods | 9 | 17 |
| integer types written | `byte`, `uint64` | `byte`, `uint64` (plus `int` values from `len`) |
| max loop nesting | 1 (condition-only loops) | 1 (three-clause loops) |

Handwritten SDK calls: `token.Balance` (8), `token.Transfer` (3),
`token.TransferSigned` (3), `token.CreateSigned` (2), `token.CloseSigned` (2),
`token.Load` (2) and `pda.Seeds` methods `AddByte` (4), `AddKey` (4),
`AddBytes` (2). Generated-only calls include `system.Create`,
`solana.CloseAccount`, `pda.Seeds.Matches`, `token.Read` and the account
accessors (`solana.Key`, `Executable`, `Writable`, `Owner`, `ProgramID`, ...).
No Clock, Rent or return-data calls are used. Full counts and `file:line`
positions are in [full-swap.json](full-swap.json).
