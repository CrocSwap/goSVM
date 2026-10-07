# Full swap / independent service Clock regression

Passed October 5, 2026 with the Clock-enabled frontend and SDK-2 snapshot.
[summary.json](summary.json) binds the actual sources, commands, native checks,
SBF executions and artifact hashes. The program reproduces its previous ELF
byte-for-byte: `697f57b3b3512c3baa25785822c765239eb9861feb8fc852af94fb9de0420b13` (21,536 bytes).

All 108 preserved full-swap scenarios/110 transactions pass in three runs,
with native vectors, the 13-case project CLI suite, canonical shared types/codecs
and independent ordinary-Go service checks. [Previous proof/methodology](../2026-10-05-framework-swap-lifecycle-final/README.md)
records scope and comparison limitations; the updated Clock helper is unreachable
from this preloaded swap and changes neither its code nor validated behavior.
Full swap initialization/closing and broader lifecycle measurements remain open.

```sh
python3 scripts/framework_swap_verify.py --output results/compiler/new-regression
```

Use a new result directory. This is local LiteSVM evidence, not a fresh validator
or external developer trial.
