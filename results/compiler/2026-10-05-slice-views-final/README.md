# Corrected slice view experiment

This successful experiment corrects the initial native fixture capacity mismatch.
It passes 262 native reference vectors and 264 SDK/SBF scenarios in three runs,
including aliases, full-width bounds, nil/empty views, 1 KiB storage, real empty
SHA-256 and transaction rollback. Its 6,280-byte ELF and frame measurements match
[the subsequent supported snapshot](../2026-10-05-slice-views-supported/README.md).

The subsequent snapshot adds verification of descriptor aggregate copying without
array declarations. This experiment retains its own source hashes, logs, native
expectations and runtime reports; neither experiment is a fresh-validator oracle.
