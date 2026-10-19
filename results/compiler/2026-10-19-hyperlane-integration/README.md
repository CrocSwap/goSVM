# Hyperlane contribution integration

October 19, 2026; macOS arm64; pinned Go 1.22.0; platform-tools v1.57;
LiteSVM runners 0.5.1 (fogo-v0) and 0.6.0 (solana-v3). Local validation;
no public-network or complete-Hyperlane compatibility claim.

This combines contributor PRs #9–#24, including #12's standalone memory-helper
fix and the later #23/#24 compiler fixes. Contributor commits remain in the
merge history. Earlier per-PR result directories describe their individual
checkpoints and are unchanged.

Integration decisions and corrections:

- Incoming 64-account decoding remains opt-in and low-level only.
- Outgoing CPI supports 64 metas and four signer groups, with 16 inline metas.
- Native `GetReturnData` now rejects overlapping copied-output and setter
  buffers like both SBF runtimes. Added exact/partial overlap, empty reads and
  caller-set-then-silent-CPI clearing probes; corrected clearing documentation.
- ATA creation checks the known ATA/System/token program IDs and delegates
  canonical-address validation to ATA. A substituted address propagates
  `InvalidSeeds` on both targets; `Address` and `Matches` remain explicit APIs.
- All six SDK-2 example snapshots include the combined source generation.
- New Token-2022 and ATA error codes are recorded in ERROR_CONTRACT.

## Rebuild reference

Two different fresh source/cache/temp paths produce identical clean ELFs for
each workload and target. The full swap includes the combined outgoing-CPI
and memory changes, so it differs from either standalone PR reference.
`clock2` retains the memory PR's bytes. `rebuild/metrics.json` records the
comparison with that earlier memory-only reference (`matches_reference` is
therefore false for the full swap); these new hashes are the current pins.
A separate fresh verification against these pins reproduces all four ELFs.

| Target | Workload | ELF bytes | SHA-256 |
| --- | --- | ---: | --- |
| fogo-v0 | full-swap | 70160 | `234859bb9a00c80657369bd307fa43d227941e11c2bb5633018898220c412f6c` |
| fogo-v0 | clock2 | 5272 | `d85d909e057f6c6e11c468306e0b9222609dc3498b26cbf9b6dbad1d29bcd327` |
| solana-v3 | full-swap | 70168 | `0e7c540b986a9c1e31349f45a6bd656c731b5502a071787e5502cbc8eb14ca89` |
| solana-v3 | clock2 | 4248 | `88611a509e500806729bf4e9624fac873b6f302960497c26fd70113781cc6f73` |

## Validation

Focused combined compiler/SDK/native-C and SBF tests pass, including the frozen
compiler corpus, both new compiler fixes, the CPI limits and real Token/ATA
programs. All separate example/service modules were tested. Full root tests and
GitHub Linux/macOS CI are pending at this checkpoint; final results will be
recorded before merge. Raw command logs and generated binaries stay local.

## Reproduce

```sh
bash scripts/build-cli.sh build/gosvm
make test
python3 scripts/verify_rebuild.py --output <new-directory> --cli build/gosvm
python3 scripts/check_rebuild_reference.py <new-directory>/metrics.json
```

Run native tests in the separate modules from `.github/workflows/ci.yml` and
`gosvm test --svm` in full-swap, escrow, tutorial, fee-vault, logging and
find-address. macOS 26 with Go 1.22 needs external linking for the tutorial's
pure-Go solrpc tests (`GOFLAGS=-ldflags=-linkmode=external`), or a newer host Go.
