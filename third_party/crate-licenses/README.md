# Supplementary crate license texts

Some crates in the SVM runners' dependency graphs declare the MIT license in
`Cargo.toml` but publish no license file in the crate. MIT requires the
copyright and permission notice to accompany copies, so
`scripts/package_runner_release.py` copies the upstream text from
`<crate>-<version>/` into the runner archive as `licenses/<crate>-<version>/…`
when that exact crate version ships no license file, and marks it in
`CRATE_LICENSES.txt` as "upstream text supplied by goSVM". Entries are keyed by
version because licenses change between releases (`enum-iterator` 1.5.0 is MIT;
its 0.8 and 2.x releases are 0BSD). The packaging script refuses to build an
archive in which a crate without an Apache-2.0 license still lacks a text.
Retrieved October 16, 2026; an inventory, not a legal review.

| Crate | In runners | Copyright line | Source of the text (byte for byte) |
| --- | --- | --- | --- |
| `enum-iterator` 1.5.0 | 0.5.1 | `Copyright (c) 2023 Stephane Raux` | `LICENSE` of <https://github.com/stephaneyfx/enum-iterator> at the crate's recorded commit `0a3fbd0a0e94` (`.cargo_vcs_info.json`); `Cargo.toml` authors: Stephane Raux |
| `five8` 0.2.1 | 0.5.1 | `Copyright (c) 2024 Kevin Heavey` | `LICENSE` of <https://github.com/kevinheavey/five8> at the crate's recorded commit `ced29f570f8d` |
| `five8` 1.0.0 | 0.5.1, 0.6.0 | `Copyright (c) 2024 Kevin Heavey` | same file at `311279fb8146` |
| `five8_const` 1.0.0 | 0.5.1, 0.6.0 | `Copyright (c) 2024 Kevin Heavey` | same file at `311279fb8146` (the workspace that publishes the crate) |
| `five8_core` 0.1.2 | 0.5.1 | `Copyright (c) 2024 Kevin Heavey` | same file at `2fed607efced` |
| `five8_core` 1.0.0 | 0.5.1, 0.6.0 | `Copyright (c) 2024 Kevin Heavey` | same file at `311279fb8146` |
| `qstring` 0.7.2 | 0.5.1, 0.6.0 | `Copyright 2017 Martin Algesten` | the "MIT License" section of the crate's own `README.md` (identical upstream at <https://github.com/algesten/qstring>, which has no license file); `Cargo.toml` authors: Martin Algesten |
| `void` 1.0.2 | 0.5.1, 0.6.0 | `Copyright (c) 2015 The rust-void Developers` | `LICENSE-MIT` of <https://github.com/reem/rust-void> (commit `ea5a2526d7a8`, merged the day 1.0.2 was released; the release commit itself has no license file, and this is the project's only MIT text); `Cargo.toml` authors: Jonathan Reem |

All five8 commits hold the same `LICENSE` (unchanged since the repository's
initial commit, May 2024). The crate sets above are the macOS arm64, Linux amd64
and Linux arm64 graphs of runners 0.5.1 and 0.6.0 (`cargo metadata
--filter-platform`, build dependencies included).
