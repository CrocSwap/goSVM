# Pointer regression after byte views

The updated compiler retains all 196 scenarios in each of three runs,
including exact native-Go state/error expectations, CU/log stability and
transaction rollback. The 3600-byte ELF retains SHA-256 `b4b53ed0faf6e3be0e326973aaef406fb57735d786bd682174726c82f0ee5975`.

The summary binds this compiler snapshot, exact commands, native references,
fixture and ELF hashes, runner identity and stack measurements. The earlier
pointer experiment remains a separate historical snapshot. These are local SBF
checks, not a fresh-validator or clean-host claim.
