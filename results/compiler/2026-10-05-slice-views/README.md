# Initial slice fixture attempt

This attempt stopped after compilation and native reference execution because
the native fixtures supplied different buffer capacities from the SBF account
inputs. Go 1.22's hex decoder reuses its input byte buffer, leaving spare capacity.
The context fixture's capacity checks consequently returned status 202.

No SBF scenario run is claimed by this attempt. The preserved summary, logs,
native expectations and compiled artifacts show the failure. The corrected
native driver explicitly caps account/instruction slices at their serialized
byte lengths while retaining the same storage. Fresh evidence is recorded in
[the corrected experiment](../2026-10-05-slice-views-final/README.md).
