# Restricted validator validation attempt

The patched runner, CLI, verifier and original manual Go token ELF built.
Starting a fresh validator was blocked by restricted loopback binding. The log
records `bind: operation not permitted`; no passing runtime summary was produced.
See [captured-oracle validation](../2026-10-05-lifecycle-verified/README.md).
