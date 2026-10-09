# Modern runner 0.6.0 local package — October 9, 2026

`gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz` packages the modern runner binary
(LiteSVM 0.13.0 / Agave 4.0 / SBPF 0.14.4 + rent-error precedence patch) whose
binary SHA-256 `550bac10c7f127d6ca6fb73df6e54942ec7347f3b9a68208e592ae085e9dff30`
was used in the [October 7 target acceptance](../../compiler/2026-10-07-target-profiles-final/README.md).
The frontend embeds the matching pin (`internal/runner/pins/0.6.0-darwin-arm64.json`;
copy in `pin.json`). The archive is local-only (ignored, like the 0.5.0 package);
`pin.json` and `provenance.json` are tracked; `dependencies.json` is a local-only inventory.

```sh
gosvm runner install -archive results/svm/2026-10-09-runner-0.6.0-package/gosvm-svm-runner-0.6.0-darwin-arm64.tar.gz
gosvm runner status
```

Built from the existing binary with `scripts/package_modern_runner.py --binary`;
two packaging runs produced identical pins. The binary embeds absolute checkout
paths, so rebuilding elsewhere yields different bytes that this pin rejects.
This is a local experimental package: no public download, notarization or
redistribution approval (see `provenance.json` scope).
