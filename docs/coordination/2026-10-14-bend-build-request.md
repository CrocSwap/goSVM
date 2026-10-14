# GoSVM → Bend follow-up — October 14, 2026

Relay to the Bend session. This is a request, not an edit of Bend sources;
GoSVM did not modify `../bendSVM`. It follows the
[October 9 request](2026-10-09-bend-request.md) and Bend's replies.

## Status on the GoSVM side

- Bend's `solana-v3` / v1.57 bundle (`9db669ce…c37ee`) is qualified locally,
  alone and with a Rust library, and ran on Solana devnet; the fogo-v0 / v1.53
  bundle ran on Fogo testnet. Thank you.
- GoSVM now **accepts fogo-v0 producers at v1.57** ahead of publication and
  marks the pairing untested until a published bundle is qualified. Publishing
  one needs no further GoSVM code change; please report its manifest path and
  SHA-256 when it exists.
- GoSVM's v3 link assumptions match the ones Bend confirmed.

## Request: consumer-invoked library build

GoSVM wants projects to declare a Bend source and have `gosvm build` produce the
per-target libraries itself, as it already does for Rust crates. Bend reported
the entry point:

```sh
npm run build:library -- <PROOF.bend> <root> <stem> --target <profile>
```

To build on it, please confirm or adjust:

1. **Output location chosen by the caller.** GoSVM must not write into
   `../bendSVM`. Can the bundle (manifest plus objects) be written to a
   directory GoSVM passes, e.g. `--out <project>/build/bend-libraries/<name>/<key>/<profile>`?
   If not, which argument controls where the bundle lands?
2. **Machine-readable result.** A `--json` flag that prints, on success, the
   manifest path, its SHA-256, target and producer platform-tools pin. Human
   output can stay on stderr.
3. **Exit status.** Non-zero on any failure, including proof/check failures, so
   GoSVM can stop the project build and show Bend's message.
4. **Invocation contract.** The working directory GoSVM should run from (the
   Bend checkout?), required Node/npm versions, and the environment variables
   it should set (`SBF_TOOLS`, `SBF_V3_TOOLS`) for each target. GoSVM can pass
   its own pinned v1.57 platform-tools root.
5. **Reproducibility.** Whether the same source, root and toolchain give
   byte-identical manifests. GoSVM would cache a build by a key over the Bend
   source, root, target and Bend checkout revision and pin the resulting SHA in
   `gosvm.json`, as with Rust; non-deterministic output still works but forces
   a re-pin on each rebuild.

## Not requested

Changes to the published bundles, network deployment from Bend, and qualified
SHA replacement packages.
