# GoSVM → Bend coordination request — October 9, 2026

Relay to the Bend session. This is a request, not an edit of Bend sources;
GoSVM did not modify `../bendSVM`.

## Goal on the GoSVM side

Before moving on to core-language work, the user wants two hurdles clearly
passed:

1. Fogo and Solana from one Go project, with users minimally aware of ISA,
   toolchain or runner details.
2. A single program combining Go (shell: accounts, syscalls, CPI) with pure
   Bend and pure Rust libraries over the shared ABI v1, on **both** targets.

Item 2 on `solana-v3` needs a Bend v3 producer. GoSVM observed (read-only,
14:30 EDT) that Bend is already implementing one: `compiler/target-profile.ts`,
`tests/target-profiles.ts` and the updated `docs/library-artifact-contract.md`
record a `solana-v3` / v1.57 profile using the same clang/readelf content pins
that GoSVM qualified. GoSVM treats that as work in progress until Bend reports
a completed run and a published bundle.

## Requests

1. **Publish a `solana-v3` bundle** of `mixed.evaluate` once Bend's own checks
   pass, in a stable location such as
   `build/interop/solana-v3/<id>/library.manifest.json`, and report the
   manifest SHA-256. GoSVM will qualify the linked Go/Bend v3 image in a fresh
   results directory, against its modern LiteSVM 0.13.0 runner.

2. **Decide the fogo-v0 producer pin.** GoSVM compiles fogo-v0 with v1.57;
   Bend's fogo-v0 is v1.53, so users currently pass
   `-allow-library-tools v1.53`. Either:
   - (preferred) also offer fogo-v0 at v1.57, so one backend serves both
     languages on each target; or
   - state that v1.53-producer / v1.57-caller is a supported pairing Bend
     will keep, so GoSVM can make it a default allowance instead of a user flag.

3. **Stable consumer-invoked build entry.** For ergonomic project builds,
   GoSVM would like to run Bend's library build itself when a project declares
   a Bend source dependency (Bend source path + root → per-target bundles),
   instead of asking users to copy manifest paths and SHAs. Please confirm or
   specify the stable interface: command, arguments (`--target`), exit status
   semantics, where the manifest lands, and which environment variables
   (`SBF_TOOLS`, `SBF_V3_TOOLS`) GoSVM should set. GoSVM would pin each
   resulting manifest SHA in the project lock, as today.

4. **Confirm v3 final-link expectations for a C/Go shell.** GoSVM's v3 link
   supplies its own memory helpers, uses static syscalls and refuses
   unresolved non-syscall imports. Please confirm Bend's v3 objects make no
   other assumptions (e.g. stack/frame limits, the optional memory object,
   no writable allocated sections), so the Go shell's checks match yours.

## Not requested

Qualified SHA replacement packages, network deployment from Bend and any
change to the published fogo-v0 bundle
`ec18012f5bb98cd19813b721f2ec343d628c679052828a3eb122ca1b335bc6de`
(it remains GoSVM's accepted v0 pin).

Rust libraries in GoSVM programs will be built by GoSVM directly from Rust
crates using the same ABI v1 shape; that does not require Bend changes.
