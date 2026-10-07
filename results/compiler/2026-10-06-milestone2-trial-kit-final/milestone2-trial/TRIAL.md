# goSVM milestone 2 trial

Start with [the trial procedure](docs/MILESTONE2_TRIAL.md), then the
[swap](examples/full-swap/README.md), [escrow](examples/escrow/README.md) and
[ordinary Go service](examples/full-swap-service/README.md). This archive needs
no goSVM checkout, Cargo, registry downloads, RPC or wallet. Go 1.22, macOS arm64
and installed platform-tools v1.51 LLVM are prerequisites.

From this directory, run:

```sh
python3 verify.py --llvm /absolute/path/to/platform-tools/llvm
```

The verifier uses an isolated Go/goSVM cache, installs the included runner,
checks/builds/tests both application modules, runs all packaged SBF suites and
tests/runs the separate ordinary Go service. Commands and expected reports are
inspectable. Fresh results go to `trial-results/`; `--output` selects another new
directory. No downloads or public transactions occur.

For manual CLI use, set `GOSVM_CACHE` to the same absolute directory reported by
the verifier and put this kit's `tools` directory on PATH. Use `-llvm` with build
and SBF tests. The native CLI path handles the macOS Go 1.22 external-linking
workaround. `gosvm test --svm` retains generation/native/build/actual-SBF phases.

Explain an invalid-authority and a rollback case from the SBF reports. Complete
`REPORT.md`, including assistance and unsupported-feature feedback. Automated
passing results alone do not establish an independent developer trial.

The app READMEs retain references to historical repository experiments; those
reports are outside this small archive. The baseline reports needed here are
under `expected/`. These examples are experimental; the managed swap gives its
creator authority to drain and close, and escrow allows creator cancellation.
