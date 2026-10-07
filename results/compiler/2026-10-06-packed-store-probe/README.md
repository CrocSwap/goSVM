# Direct packed-store SBF proof

PASS: **422 cases × three runs**, 64 successes and 358 failures. The canonical optimized CLI compiles both matching writer widths. Tests verify exact 32/64-bit output at unaligned offsets, empty/short storage, full-width unsigned offsets, aborts, rollback after partial writes and rollback after successful writes followed by Custom 7331. Simulation and committed state are checked independently by the preserved fixture adapter.

[Summary](summary.json), [fixtures](fixtures.json), source and generated C, ELF and three per-case reports preserve evidence. ELF is 2,888 bytes, SHA-256 `07cf8861707335389c806dcede7695ad006a2245ad697f203df440040acb6517`. Fixture CU includes the 150-CU budget prefix; these are semantic probes, not full-handler performance attribution. Reproduce in a new directory using `python3 scripts/packed_store_probe.py --output results/compiler/NEW-EXPERIMENT`. See [compiler optimization](../../../docs/PACKED_STORE_OPTIMIZATION.md).
