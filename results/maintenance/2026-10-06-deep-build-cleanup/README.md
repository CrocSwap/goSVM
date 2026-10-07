# Deep build cleanup — October 6, 2026

The user's direction was to discard rebuildable workspace output. The entire
`build/` directory (6.18 GiB allocated) was removed.
Non-cache working snapshots are retained in `build-snapshots.tar.gz`
(316.4 MiB compressed), deduplicating identical file contents and
preserving executable permissions and symlinks. Approximate net reclamation is
5.87 GiB before small report overhead.

All 4,965 snapshot entries were verified against their archived bytes, modes
or symlink targets before deletion. All 5,409 preexisting result
files retain their SHA-256 hashes. External toolchains, the sibling benchmark
and Basanos were untouched. `cleanup.json` records the complete inventory and
integrity checks. Go/Cargo caches, installed tool copies under build, and Cargo
target outputs can be rebuilt rather than retained.

To restore the archived working paths, run from the repository root:

```sh
tar -xzf results/maintenance/2026-10-06-deep-build-cleanup/build-snapshots.tar.gz
```

Restoration is optional for ordinary current-source builds. Historical scripts
that expect a pinned frontend/module beneath `build/` may require restoration.
Root source and canonical examples were not removed.

Future preview: `python3 scripts/clean_build.py --deep`.
Future cleanup with a fresh report/archive: `make clean-deep`.

Validation also exercised archive deduplication, executable permissions,
symlinks, cache exclusion, archive resumption, refusal of mismatched archives,
and complete deletion with preserved evidence in an isolated temporary tree.
