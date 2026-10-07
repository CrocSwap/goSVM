# Retained unsuccessful verification attempt

The first proof fixture used 31 unique account keys in a legacy transaction, exceeding its packet-size limit. This is a fixture construction failure. The 17-account no-op had already passed. The corrected run uses valid duplicate metas at 31/32, alongside native serialized tests for 32 unique accounts.

This directory is not passing evidence. See [the final decoder report](../../../docs/ACCOUNT_DECODER32.md). Logs and the attempted driver are retained unchanged.
