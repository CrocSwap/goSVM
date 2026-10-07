# Optimized Whirlpools decoder regression

The SDK-2 incoming capacity extension preserves exact CU on all 166 optimized,
fixed-fee classic-token handler cases. All cases pass three runs against unchanged
scoped/full Rust, with pinned fixtures/runtime, committed state assertions,
errors, CPI logs and rollback. Median paired successful-case ratio remains
1.2808531366340767×; events are omitted.

The new ELF remains 83,424 bytes, SHA-256
`f4b7cdf0c18addc7de5d22ad3aa7c8cbdcc25ca5de1b9499a20a1e60173c7c34`.
Maximum static frame is 2,368 bytes. `summary.json` records input/output hashes,
commands and frames; `per-case-cu.json` records each unchanged CU comparison.
`verify.py.txt` preserves the exact driver.

Read [the decoder report and frontend pin](../../../docs/ACCOUNT_DECODER32.md).
The original 1.33-target snapshot and benchmark pins remain frozen.
