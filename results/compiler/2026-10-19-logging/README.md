# Program logging: `solana.Log`, `Log64`, `LogPubkey` (2026-10-19)

Question: can program Go code log as Rust programs do with `msg!`,
`sol_log_64` and `Pubkey::log`? The compiler already used `sol_log_` for the
panic line only.

Compiler: branch `gap3-logging` (parent `79cf25c`); Go 1.22.0;
platform-tools v1.57; runners 0.5.1 (fogo-v0) and 0.6.0 (solana-v3). Local
only (`network_validation: false`).

## Design

| Call | Syscall (v3 static hash) | Log line | Cost |
| --- | --- | --- | --- |
| `Log(c, message string)` | `sol_log_` (`0x207559bd`) | `Program log: <message>` | max(100, length) CU |
| `Log64(c, v0, v1, v2, v3, v4)` | `sol_log_64_` (`0x5c2a3178`) | `Program log: 0x1, 0x2, 0x0, 0x0, 0x0` | 100 CU |
| `LogPubkey(c, key []byte)` | `sol_log_pubkey` (`0x7ef088ca`) | `Program log: <base58 key>` | 100 CU |

The hashes are Murmur3-32 of the names (reproducing the source's `abort` and
`sol_log_` values); the two new imports join the admitted fogo-v0 list. Each
returns 0, so a call is an ordinary statement; `LogPubkey` returns 2019 for a
key that is not 32 bytes, without logging. A message that is not valid UTF-8
fails the instruction in the runtime's syscall, as for Rust (a check in the
program would cost CU per byte); the native SDK panics there, and native
contexts get each line's text through `Context.WriteLog` (nil discards it).
The logging C runtime is emitted only when a program calls one of the three.
Evaluation order treats them as logging calls that write nothing.

## Results

| Check | Command | Result |
| --- | --- | --- |
| Native formatting | `go test ./solana -run TestLog` | 500 `Log64` lines match Go's `%#x`, 500 keys match `fixture.Base58` (leading zero bytes and the zero key included); nil `WriteLog` discards |
| Native UTF-8 rule | same | 20,008 strings: `Log` panics exactly when `utf8.ValidString` is false (overlong, surrogate, above U+10FFFF, truncated) |
| Native Go vs generated C | `go test ./internal/compiler -run TestSDK2Log` | 610 vectors: the same bytes, words and keys reach the syscalls, same statuses; runtime emitted only when called; coexists with the panic runtime; SDK 1 rejects the calls |
| SBF, both targets | `gosvm test --svm` in [examples/logging](../../../examples/logging/README.md) | 4 scenarios, 5 transactions pass on fogo-v0 and solana-v3 |
| Full-swap reference ELFs | `-no-cache` builds, both targets | `a66c5a97…` / `b419245b…`, unchanged |
| Example modules | `GOWORK=off go test ./...` in tutorial, escrow, full-swap, logging | pass (snapshots refreshed) |

The SVM scenarios assert each of the four lines a note writes (a constant, UTF-8
text, `Log64` words including `0x0` and `0xffffffffffffffff`, the author's key),
an empty message, and three invalid texts (a stray `0xff`, a surrogate, a
truncated sequence) that fail with `ProgramFailedToComplete` after the first
line, logging `failed: invalid utf-8 ...` or `failed: incomplete utf-8 ...`,
with the note count rolled back.

| Scenario (step) | fogo-v0 CU | solana-v3 CU |
| --- | ---: | ---: |
| note-logs-four-lines (1) | 3,344 | 3,344 |
| note-logs-four-lines (2) | 3,294 | 3,294 |
| invalid-utf8-62616420ff2062797465-fails (1) | 2,690 | 2,690 |
| invalid-utf8-eda080-fails (1) | 2,634 | 2,634 |
| invalid-utf8-63757420e4b8-fails (1) | 2,658 | 2,658 |

Not added: `sol_log_data` (Anchor-style events, `Program data:` base64 lines)
and `sol_log_compute_units_`; both would follow the same pattern.
