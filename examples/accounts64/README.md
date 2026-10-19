# 64 incoming accounts

A low-level SDK-2 program for the [64-account decoder](../../docs/ACCOUNT_DECODER64.md).
Build it with the option inside an SDK-2 module (or a target-less directory):

```sh
gosvm -target fogo-v0 -max-accounts 64 -o build/program.so .
gosvm svm-test -target fogo-v0 -elf build/program.so -fixtures testdata/svm.json
```

Instruction data is a mode byte, the expected account count, then arguments.
Mode 0 adds one to the first data byte of every writable program-owned
descriptor (an account listed k times gains k); mode 1 makes a System transfer
with the program, payer and destination at the given indices; mode 2 bumps,
then fails, so the writes roll back.

`testdata/svm.json` covers 33 unique accounts, 64 descriptors, 65 (1001), a CPI
beyond index 32 and rollback; `testdata/default.json` is for a build without
the option (32 pass, 33 return 1001). `TestAccounts64SBF` in
`internal/compiler` writes both (`GOSVM_UPDATE_FIXTURES=1`) and runs them on
both targets. The fixture seeds are local test identities.
