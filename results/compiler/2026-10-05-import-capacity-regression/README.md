# Array-capacity regression after package imports

October 5, 2026. All 60 full-capacity scenarios across byte/uint32/uint64/bool pass
in three SBF repetitions after module-aware imports. Every 1 KiB variant retains
its [prior capacity snapshot's](../2026-10-05-array-capacity-final/README.md) exact
ELF hash, native-Go byte/error expectations and 2,112-byte handler/64-byte adapter
frame measurements. Copies, zeroing, wrapping, failed accesses and rollback pass.

[Summary and current source hashes](summary.json) bind the saved variants, fixtures,
C, ELFs, native expectations, stack usage and reports to this increment. Earlier
evidence is preserved. [Current import evidence](../2026-10-05-shared-imports-final/README.md)
records root/nested checks. These are fixture frame measurements, not a universal
function/call-graph guarantee; no fresh validator is used.
