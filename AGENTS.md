# goSVM agent orientation

Start with [HANDOFF.md](HANDOFF.md), then [README.md](README.md) and the draft
[ROADMAP.md](ROADMAP.md). These record the user's priorities, completed
experiments, validation, and proposed product milestones. Roadmap suggestions
are not an active goal or scheduled task.

Keep this project experimental. Preserve historical evidence under `results/`;
use a new results directory for a changed experiment. Distinguish the bounded
starter, full token swap, and dependency-heavy protocol workload when discussing
CU, build times, or footprint. Keep Basanos source and build outputs untouched.

The generated starter is a separate Go module: root `go test ./...` does not
test it. See the handoff for focused validation and pinned toolchain details.
