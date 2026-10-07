# Compact single-signer owner investigation

**PASS:** isolated SDK-helper prototype using the benchmark's frozen frontend
`15b1d1cd...` / CLI SHA `75eca8ab...`, matching copied SDK, LLVM v1.51/O2/SBF v3,
and pinned runner 0.5.0. The canonical SDK, compiler, benchmark source and pins
are untouched. This is a proposed helper API, not a shipped SDK addition.

The compact `cpi.SingleSigner` reserves 530 bytes and directly builds the existing
flat one-group encoding. It avoids an intermediate `pda.Seeds` (529-byte buffer)
followed by a `cpi.Signers` (1,024-byte buffer) and the second seed-payload copy.
Six Whirlpools seeds still encode as exactly 116 bytes. `InvokeSingle` delegates
to the unchanged low-level `solana.Invoke`, retaining its account/meta/data/seed
guards and native runtime invocation. General builders retain their original
one/two-group and combined-capacity policy; the new builder supports one group.

| Same prototype ELF / same accounts | CU |
| --- | ---: |
| Existing seed construction plus signer copy | 5,590 |
| Compact signer construction and output | 3,327 |
| Existing general signer: real System transfer | 6,357 |
| Compact signer: same real System transfer | 4,019 |

The isolated construction comparison saves 2,263 CU. The paired transfer case
saves 2,338 CU (~36.8%) in this small program. These are observed complete probe
transactions, not projected full-Whirlpools savings or exact copy attribution.
Dispatch/codegen and output paths can affect the comparison. The unchanged
baseline ELF reproduces the benchmark probe's CU exactly after removing that
experiment's 150-CU ComputeBudget instruction; its 2,454-CU copy difference and
1,277-CU repeated-validation difference remain exact.

## Verification

- Native: 1,000 randomized encodings against an independent encoder and the
  general builder; maximum 16×32-byte seeds (530 encoded bytes), rejected-add
  state preservation, key/scalar little-endian encodings, invoke guards and exact
  callback status propagation.
- SBF: original 20-case baseline, 24-case extended prototype, and seven actual
  System-CPI scenarios each pass three repetitions. Account bytes/metadata and
  errors are asserted, with exact repeated CU/log equality and ELF/runner hashes.
- Actual runtime: general and compact PDA transfers move seven lamports with
  identical final states; wrong valid off-curve bump and readonly source both
  retain `PrivilegeEscalation`; custom failure after successful compact transfer
  restores all watched accounts.
- Original malformed-seed, nil signer, third signer-group, combined-capacity,
  malformed tick discriminator/pool/flags/length failures remain checked. Compact
  33-byte seed, seventeenth seed and nil signer failures are additionally checked.
- Instrumented stack compilation/relink reproduces the exact 15,712-byte
  prototype ELF. Largest static frame is 2,816 bytes; this is a per-function
  measurement, not a call-chain bound. The combined test includes both builders,
  tick validation and additional CPI/error handlers; ELF size is not an estimate
  of the compact builder's production size effect.
- All authoritative input files and benchmark SDK/source guards remain unchanged.

`summary.json` records all inputs, commands, case CU, frontend/runner pins and ELF
hashes. `single.go.txt`, `prototype-program.go.txt`, emitted C, fixtures, reports
and stack usage preserve the candidate and reproduction. Native tests are in
the repository script input and the isolated staged SDK.

## Next adoption gate

Keep this prototype separate until the benchmark integrates it into a new derived
SDK/module snapshot and reruns the complete 166-case Token/handler corpus. This
experiment does not run real Token transfers, two-group signing through the new
builder, full-handler performance or public SDK packaging. Two-group signing
continues to use the unchanged general builder. Builder bytes borrow its backing
value; callers must keep it alive and avoid mutation during invocation.

```sh
python3 scripts/single_signer_verify.py --output results/compiler/new-single-signer
```

This requires the preserved sibling frontend/module and the local pinned runner
installed by the milestone-2 benchmark. Choose a new result directory. The earlier
fixture-error pilot is preserved separately. No independent developer trial or
milestone-2 closure is implied.
