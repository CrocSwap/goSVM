# SDK-2 lifecycle boundary proof

**PASS:** 35 scenarios/46 transactions in three repetitions using the actual
compiled-SBF artifact and pinned runner 0.5.0. Independent expectations compare
all declared account bytes, owners, lamports and metadata; errors and CPI counts
are explicit. Repeated cases retain identical CU/log outputs.

The fixture proves signed/PDA System creation, rent funding/top-ups, current
nondefault Rent, owned shrink/grow/regrow zeroing, exact 10 KiB growth/allocation
boundaries, duplicate descriptors, close/recreate within and across invocations,
free-rent account retention, refund overflow and readonly/owner/authority failures.
Failed System CPI, failed handlers, an underfunded resize and multi-instruction
failure preserve transaction state. Closing has separate simulation-zero-record
and committed-absence assertions.

Native verification includes 100,000 random IEEE arithmetic rent comparisons
plus edges; generated C adds 1,000 rent vectors and 1,035 lifecycle vectors under
undefined-behavior sanitization. Root tests/vet pass separately. The exact
15,824-byte ELF is identified by `summary.json`; instrumented linking reproduces
it, and its largest static frame is 1,664 bytes. The manual matched-token adapter
rebuild retains its historical ELF hash.

This is SDK boundary evidence, not generated initialization/close policy, full
swap/escrow lifecycle, an external trial, or a fresh-validator comparison. The
rent helper rejects overflowing integer products rather than Rust release-mode
wrap. Measurements apply only to this fixture. `summary.json` binds all sources,
commands, runner and artifacts; earlier bring-up attempts are preserved separately.

```sh
python3 scripts/lifecycle2_verify.py --output results/compiler/new-lifecycle-proof
```

Use a new evidence directory with the pinned local archive and installed v1.51
LLVM. See the [fixture](../../../examples/lifecycle2/README.md) and
[SDK contract](../../../docs/SDK2.md).
