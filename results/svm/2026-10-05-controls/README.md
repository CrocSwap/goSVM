# Superseded control-validation attempt

October 5, 2026. The SBF probe and fresh-validator case comparison passed, but the
script failed its metadata check: untyped Go JSON decoding rounded the controlled
slot 9,007,199,254,740,993 to 9,007,199,254,740,992 in the fast report. The runtime
and probe retained the exact value. This directory preserves that failed attempt
and must not be presented as a completed validation.

Raw JSON metadata now preserves integer precision. See the
[final validation](../2026-10-05-controls-final/README.md).
