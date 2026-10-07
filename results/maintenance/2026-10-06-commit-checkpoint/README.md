# Commit checkpoint — October 6, 2026

This checkpoint groups the compiler/SDK, generated framework, SVM tooling,
verification evidence, cleanup and roadmap documentation into separate commits.
Large generated fixture/result JSON files and the deep-cleanup build backup
remain on disk, with paths, sizes and SHA-256 recorded in `local-artifacts.json`.
They are intentionally excluded from Git. Ordinary Git checkout does not include
those local-only files; source snapshots, scripts, summaries and smaller evidence
remain committed. No historical evidence was modified or deleted.

The session mounts the original `.git` read-only. Commits are prepared in an
isolated writable checkout and delivered as a Git bundle; the original branch
and index cannot be advanced by this session.
