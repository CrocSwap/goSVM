# SDK-2 32-account decoder: passing proof

October 6, 2026. Read [the report and benchmark-owner handoff](../../../docs/ACCOUNT_DECODER32.md).

`summary.json` passes 384 native serialized-loader vectors, eight no-op boundary
cases per SDK × three SBF runs, 41 write/alias/CPI/rollback cases × three, and the
430-case standard Phoenix plus 16/17-account real handler reproduction against
Go/scoped/upstream Rust × three each. Standard Phoenix CU is unchanged on every
case, retaining the 1.3701157742402315× median paired successful-case ratio.
The 31/32 SBF account-count fixtures use duplicate keys to fit legacy packets;
native tests additionally cover 32 unique descriptors.

`baseline-identity-audit.json` compares all standard cases with the accepted Go
and both Rust baseline reports (exact CU/errors/commit and normalized logs).
`root-tests.log` and `root-vet.log` retain passing root verification.

`frontend.tar.gz` ships the executable macOS arm64 CLI, matching SDK and complete
frontend source with its file manifest. `phoenix-source.tar.gz` ships the unchanged
Phoenix source with that SDK. Archive/CLI/ELF pins are in `summary.json`;
`delivery-verify.json` records extraction, all 116 manifest-file hashes, executable
mode and an exact Phoenix ELF rebuild. Use a new benchmark pin; keep original
pins and historical reports frozen. No Rust source or ELF was changed.

`verify.py.txt` is the exact driver used. See the linked report for the separate
Whirlpools and SDK-application regressions and the retained unsuccessful setup
attempts. No fresh-validator, clean-host or public-release claim is made.
