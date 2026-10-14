# goSVM agent orientation

Start with [HANDOFF.md](HANDOFF.md), then the draft [ROADMAP.md](ROADMAP.md).
These record the user's priorities, completed experiments, validation, and
proposed product milestones. Roadmap suggestions are not an active goal or
scheduled task. [README.md](README.md) is the public introduction; keep it focused
on the project, developer benefits, a short quickstart, and links to deeper docs.

Detailed setup, compiler architecture, historical benchmark measurements,
supported syntax, reproduction commands, and cache cleanup are preserved in
[the development reference](docs/DEVELOPMENT.md). Consult the handoff and
[milestone-2 audit](docs/MILESTONE2_ACCEPTANCE.md) for current status rather than
treating older checkpoint descriptions as current requirements.

Keep this project experimental. Preserve historical evidence under `results/`;
use a new results directory for a changed experiment. Distinguish the bounded
starter, full token swap, and dependency-heavy protocol workload when discussing
CU, build times, or footprint. Keep Basanos source and build outputs untouched.

The generated starter is a separate Go module: root `go test ./...` does not
test it. See the handoff for focused validation and pinned toolchain details.
Use `make test` for root tests; on macOS it handles the pinned Go loader workaround.
Run example/service tests within their own modules when relevant to a change.

Keep benchmark claims tied to their exact workload, frontend/SDK pins, compiler
optimization, runtime features, and cache state. The scoped Whirlpools handler
is distinct from the complete upstream contract and the generated token swap.
Native callback tests do not establish SBF transaction rollback.

Generated experiment binaries, archives, dumps, and raw reports are local-only;
keep source, summaries, compact metrics, and reproduction instructions tracked.
Follow [the artifact retention inventory](results/maintenance/2026-10-06-artifact-untracking/README.md).
Do not delete historical evidence or change required vendored runtime assets as
part of routine build cleanup.

Current network/backend selection and explicit migration are documented in
[target profiles](docs/TARGET_PROFILES.md); preserve target-less v1.51 projects'
historical meaning. [Bend libraries](docs/BEND_LIBRARIES.md) consume pinned producer
artifacts: fogo-v0 at v1.57 (default) or v1.53 and solana-v3 at v1.57, all
qualified locally. [Rust libraries](docs/RUST_LIBRARIES.md) are built by
GoSVM from crates. `gosvm deploy`, `call` and `authority` reach public networks;
local acceptance alone does not establish public-network compatibility. Keep modern runner/bootstrap outputs
rebuildable and untracked.
