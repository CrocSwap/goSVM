# Complete multiplication-helper SBF ABI proof

633 edge/random full-width 128-bit products pass both v0 and v3 × three runs,
including nonzero high limbs and negative bit patterns interpreted modulo 2^128.
A narrow C wrapper calls the actual freestanding `__multi3` through its SBF ABI;
it checks exact simulated/submitted account bytes against Python integer math.
This is a helper ABI proof, not an expansion of supported Go integer types.

`summary.json` records source/CLI/runtime/fixture pins, commands and artifacts.
The emitted C, separate helper objects, relocations, disassembly, frame data and
all reports are retained. The helper has no calls/imports and uses zero stack.
`verify.py.txt` is the exact driver.

Read [the compiler/SDK handoff](../../../docs/PHOENIX_DEPENDENCIES.md).
