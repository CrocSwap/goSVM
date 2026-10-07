# Managed swap fixture bring-up

Native SDK checks, CLI scaffold/generation and SBF compilation passed. Fixture
validation rejected the substituted writable intruder account because it lacked
an explicit state assertion. No SBF case execution is claimed for this attempt.
The subsequent fixture adds the intruder to the full-state assertions; the failed
summary and logs remain preserved.
