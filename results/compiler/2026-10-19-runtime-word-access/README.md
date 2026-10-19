# Whole-word runtime access on SBF

October 19, 2026. Local only; runners 0.5.1 (fogo-v0, LiteSVM 0.8.2) and 0.6.0
(solana-v3, LiteSVM 0.13.0), platform-tools v1.57. Design:
[BACKEND_INTERFACE.md § Runtime word access](../../../docs/BACKEND_INTERFACE.md#runtime-word-access).

## Why

The C runtime read and wrote the loader envelope and account fields one byte
at a time, compared 32-byte keys in a byte loop and staged every
`CreateAddress` result in a local before a byte copy. Disassembly of a
Hyperlane Go program showed about 9 SBF instructions per copied byte and
12 per compared byte: a `CreateAddress` call cost 1,944 CU, of which 1,500 is
the syscall and 288 the staging copy.

## Runtime routines (`TestRuntimeWordsSBF`)

Whole-transaction CU, main `939b215` vs this branch, identical on both
profiles (`fogo-v0-cu.json`, `solana-v3-cu.json`). All 49 cases match the host
on both:

| case | before | after | saved |
| --- | ---: | ---: | ---: |
| `CreateAddress` success (16 program keys) | 2,439 | 2,103 | 336 |
| `CreateAddress` on-curve failure (16) | 2,148 | 2,103 | 45 |
| `CreateAddress` with output aliasing the seeds, success (8) | 2,147 | 1,805 | 342 |
| `ResizeAccount` + `CloseAccount` (same key) + `Rent` + `IsPDA` | 2,559 | 1,897 | 662 |

Every case in a row costs the same CU. On `79cf25c`, before #25 merged the
bulk-memory change (#22), the success and lifecycle rows were 2,818 to 2,471
and 2,564 to 1,900. #22 lowered both sides by moving the test program's own
copies to syscalls; the savings moved by at most 11 CU.

## Hyperlane Go programs

Measured before #25, on `79cf25c`: `hyperlane-monorepo` `sealevel` at
`1d4694c13f`, built with `hyperlane-integration-2` and with
`hyperlane-integration-2` plus this branch, run through all 23 Rust functional
suites (584/584 pass both ways, 4,577 paired invocations). Per-call median 6%
less CU. Total CU per program: test ISM 10% less, composite ISM 6% less,
mailbox 7% less, token programs 6% to 8% less, IGP 5% less, fee 4% less. No
program got more expensive in total.
