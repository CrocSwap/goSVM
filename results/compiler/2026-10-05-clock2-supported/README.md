# SDK-2 runtime Clock proof

Passed October 5, 2026 on the recorded macOS arm64 host. The native SDK and
compiled C adapter match 1,010 randomized/boundary vectors under undefined-behavior
sanitization, including guard bytes, syscall counts, canonical word encoding and
output preservation after failure. SDK 1 rejects the new accessor.

A fresh schema-2/SDK-2 scaffold supplies the actual on-chain API. The compiled
probe reads runtime Clock through the syscall, consumes timestamp bits without
signed Go types, and checks exact account bytes and errors in 12 scenarios/15
transactions, repeated three times with identical CU/log outputs. Controlled
1000/1010/unchanged timestamps drive state changes; negative timestamp extremes,
time regression, unsigned word extremes, consumer arithmetic overflow, wrong
output lengths, failed handlers and atomic later-instruction rollback are covered.
Repeated identical instructions explicitly use fresh blockhashes.

The ELF is 4,760 bytes, SHA-256
`104cccf14fc4d68d299896bea65b6962dfa00349cbe208816e2ea4af50c58da7`.
Every frame is static; the maximum is 1,152 bytes. Instrumented linking reproduces
the exact executed ELF hash. [summary.json](summary.json) records commands,
source/tool hashes and individual timing observations. `svm-0.json` through
`svm-2.json` record execution. The [earlier failed attempt](../2026-10-05-clock2/README.md)
is preserved with its duplicate-transaction fixture failure.

```sh
python3 scripts/clock2_verify.py --output results/compiler/new-clock-proof
```

The API serializes five 64-bit little-endian words; signed Clock timestamps retain
their two's-complement bits. Output must be exactly 40 bytes (2016 otherwise).
Failed reads preserve output. Native tests set `ReadClock`, whose absence returns
2009. See [the SDK contract](../../../docs/SDK2.md#runtime-clock).

This is a Clock boundary and small uint64 consumer probe, not a Whirlpools
handler, Orca U128 reward verification, fresh-validator check or developer trial.
The benchmark must pin the updated frontend and SDK together and verify its own
actual-handler timestamp/reward behavior before interpreting handler CU.
