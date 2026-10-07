# Milestone 2 independent developer trial

This is the final developer-workflow gate in [the roadmap](../ROADMAP.md).
An independent developer should reproduce the generated token swap and SOL
escrow using the CLI and documentation, and record where the workflow fails or
requires explanation. A maintainer's package rehearsal does not satisfy this gate.
The trial is local; it does not require deployment or a wallet.

## Trial kit

The kit contains a matching macOS arm64 CLI, pinned runner archive, complete
standalone `full-swap`, `escrow` and `full-swap-service` modules, SDK snapshots,
language/framework references, expected runtime reports, a verification script
and a report template. Go 1.22 and installed Solana platform-tools v1.51 LLVM
remain prerequisites. This is an existing-host private-alpha trial; fresh-host
installation and signed public distribution are later gates.

Maintainers package current sources with a CLI built from those same sources:

```sh
bash scripts/build-cli.sh build/trial-gosvm
python3 scripts/milestone2_trial.py --output results/compiler/new-m2-trial-kit --cli build/trial-gosvm
```

Extract the resulting `milestone2-trial.tar.gz` in a new directory outside the
goSVM checkout. It includes `TRIAL.md` with the developer commands. The package
script binds the compiler/SDK/application sources and every kit file by SHA-256.
Do not mix a different SDK snapshot with the included CLI.

## Developer procedure

1. Read `TRIAL.md` and the application READMEs. Record your experience, machine,
   tool versions, start time and any prior involvement in these applications.
2. Run `python3 verify.py --llvm /absolute/path/to/platform-tools/llvm` from the
   extracted kit. This logs the real CLI check/build/native/SBF commands for
   both applications and tests/runs the ordinary Go service in both pool modes.
   Follow the commands manually if you prefer; retain reports and logs.
3. Inspect the schema, named account bundles, generated clients and SBF reports.
   Explain one invalid-authority case and one failed-CPI/transaction rollback
   case using the documented policy. You should not need compiler edits, C,
   copied shared types/math or manual framework wire offsets to run these apps.
4. Record confusing instructions, unsupported features, errors, workarounds,
   maintainer assistance and whether you could reproduce the applications with
   the provided documentation. An optional small application change is useful
   feedback: for example a minimum escrow amount with a generated-client native
   test. Keep experimental changes in a copy after baseline verification.
5. Return your completed `REPORT.md`, `trial-results/summary.json`, command logs
   and any patch. The maintainer reviews the actual reports and resolves blocking
   usability issues before closing milestone 2.

Automation verifies baseline evidence; it does not certify developer independence
or supply feedback. Preserve each trial and any fixes in new result directories.
