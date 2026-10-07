# Retained unsuccessful verification attempt

The signed-transfer fixture reused Phoenix's fee payer as its authority. The runner funds that payer during initialization, invalidating the asserted initial authority balance. All no-op boundary cases and the first write/alias/rollback probes passed; the CPI itself succeeded. The final run uses the independently signed fixed-seed authority.

This directory is not passing evidence. See [the final decoder report](../../../docs/ACCOUNT_DECODER32.md). Logs and the attempted driver are retained unchanged.
