# Packed tick code-generation inspection

Preserved baseline C and optimized LLVM IR identify packed read/write and key-comparison candidates. This inspection does not measure CU attribution. Follow-up [controlled variants](../2026-10-06-packed-codegen-controls-final/README.md) select packed writes and reject the other simple transformations.
