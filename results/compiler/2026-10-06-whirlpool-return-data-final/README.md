# Optimized Whirlpools return-data frontend regression

All 166 fixed-fee classic-token, event-omitting handler cases pass Go and unchanged
scoped/full Rust × three. Exact errors, committed state, CPI logs and rollback
assertions remain intact. The matching fresh SDK includes return-data support;
this handler does not call the setter.

Unsigned CPI meta-length lowering changes per-case CU by −3..+10. The paired
successful-case median ratio is 1.2812010716398177× (prior optimized snapshot:
1.2808531366340767×), still below the 1.33 target. The new ELF is 83,480 bytes,
SHA-256 `dd424484f740b6d0b1861b278a1afe0c1aa1a359a04ff7f2affd345713c8f9d0`;
maximum static frame is 2,368 bytes. Earlier snapshots are frozen.

`summary.json`, `per-case-cu.json`, frames and repeated reports retain evidence.
`verify.py.txt` preserves the driver with an explicit matching-SDK argument.
Read [the frontend/SDK handoff](../../../docs/PHOENIX_DEPENDENCIES.md).
