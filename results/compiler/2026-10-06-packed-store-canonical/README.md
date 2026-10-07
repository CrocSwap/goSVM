# Preserved setup/harness attempt

Packed unit tests and fresh CLI/scaffold pass; native handler setup fails because its sibling Go module had not yet been copied. Corrected setup copies both modules before tests. Historical artifacts/logs remain intact; corrected experiments use new directories.
