# Lifecycle rent-funding bring-up attempt

**Not a passing proof.** Real SBF signed account creation passed. The next resize
grew a 145-byte account to 200 bytes without funding the larger rent minimum, so
the VM returned `InsufficientFundsForRent`. Original logs and `passed:false`
summary are preserved. The [final proof](../2026-10-05-lifecycle2-supported/README.md)
funds growth explicitly and also asserts this underfunded-resize failure/rollback.
