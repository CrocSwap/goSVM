# Build-cache cleanup

October 6, 2026. Removed regenerable Cargo .rlib/.rmeta/.o/.d intermediates, incremental/fingerprint/build-script caches, and older M1 acceptance Go/toolchain caches. The preview inventory totaled 9.56 GiB of allocated files (before hard-link/shared-block effects). build/ decreased from about 14 GiB to 5.27 GiB; available volume space rose from roughly 5 GiB to 13 GiB.

Retained linked program binaries/libraries, source/SDK snapshots, current Go cache, latest M1 acceptance toolchain, all historical results and the verified Whirlpools delivery archive. Basanos and the sibling benchmark were untouched. Hash checks confirm all 4385 pre-existing result files are unchanged.

The retained runner reports `gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)`. A fresh forced SBF rebuild of the optimized Whirlpools handler reproduces SHA256 `15fc075e82d857a5eca89f71fdf0a55154ca3bc3809497b2ac2d3f78c9673398`. The initial check used the inaccessible default Go cache; the corrected check explicitly uses build/lifecycle-go-cache. Both logs are retained.

[Cleanup inventory and evidence hashes](cleanup.json), [post-cleanup verification](verification.json).

Future cleanup: `python3 scripts/clean_build.py` previews; `make clean` applies the same conservative selection. The script refuses to clean while build files are open. A subsequent Rust build or old acceptance rerun may reconstruct its caches.
