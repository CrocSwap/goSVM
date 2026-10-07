# Artifact tracking cleanup — October 6, 2026

Generated archives, executable/object/program binaries, C/LLVM/stack dumps, logs,
traces, large generated fixtures and raw execution reports are removed from the
Git index. Original files remain on disk under their historical paths, unchanged.
`local-artifacts.json` records every newly untracked file's size, SHA-256 and reason.
The earlier commit checkpoint separately inventories the large files already ignored.

Source and SDK snapshots, reproduction scripts, small fixtures, summaries and
compact observations remain versioned. `case-metrics.json` retains observed CU,
error and commit outcomes from raw reports, together with their source hashes and
selected build/runtime identity fields. Every retained metric was checked against
its original report. Parsed `program_units` observations overlap with transaction
CU and are not additive phase attribution. Correctness claims remain in the
original summaries; compact metrics do not replace full account/log evidence.

Ordinary checkout no longer includes these local-only raw files. Full historical
reproduction may require restoring the referenced artifacts. No artifact hosting
service has been configured. Existing Git history is preserved, so tracked raw
files from this cleanup can also be recovered from commit `2def309`, for example:

```sh
git show 2def309:results/compiler/2026-10-06-phoenix-dependencies-final/frontend.tar.gz > /tmp/frontend.tar.gz
```

This changes current tracking, not existing history or its disk footprint. Basanos,
the sibling benchmark and required LiteSVM vendored program assets are untouched.
