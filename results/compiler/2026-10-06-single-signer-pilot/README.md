# Compact signer pilot: incorrect negative expectation

This attempt is preserved as `passed:false`. The original 20-case baseline and
24-case prototype serialization suites each passed three SBF runs. Successful
general/compact PDA-signed System transfers and explicit post-transfer rollback
also passed before the first negative CPI case stopped the run.

The fixture incorrectly expected SDK custom error 2008 for a valid off-curve PDA
with the wrong bump. The runtime correctly returned `PrivilegeEscalation` because
the derived signer did not authorize the source account. Error 2008 is the SDK's
oversized instruction-data guard and is unrelated to this failure. The later
readonly-source expectation had the same mistaken SDK/runtime distinction.

The [corrected experiment](../2026-10-06-single-signer-supported/README.md) preserves
runtime privilege errors and adds general-builder controls for both bad bump
and readonly source. No canonical SDK or benchmark source was changed.
