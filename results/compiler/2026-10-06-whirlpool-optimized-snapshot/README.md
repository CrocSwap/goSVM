# Optimized Whirlpools delivery snapshot

October 6, 2026. [Download the experimental snapshot](optimized-snapshot.tar.gz).

Archive SHA256: `25743a6fb546530424b97dffa6f71366239c995e8968c23d4c76ba19b37c5adb`.

Includes both source modules, exact matching SDK copies, executable macOS arm64 CLI, complete frontend source archive, measured handler ELF and file/mode manifest. Hash and mode checks pass. An actual build from the extracted archive reproduces ELF SHA256 `15fc075e82d857a5eca89f71fdf0a55154ca3bc3809497b2ac2d3f78c9673398`.

The [completed benchmark proof](../2026-10-06-whirlpool-optimized-final/README.md) measures **1.280853× scoped Rust** at O2, median paired successful handler CU, with 166 scenarios and 29,713 arithmetic cases each passing three repetitions against the unchanged references. Scope remains classic-token fixed-fee swaps, events omitted. Individual successful ratios can exceed 1.33×.

[Packaging proof/commands](summary.json), [application change and adoption report](../../../docs/WHIRLPOOLS_133_TARGET.md). Use a new complete application/frontend/SDK pin; original benchmark defaults are preserved. Exact package.py.txt is retained here.
