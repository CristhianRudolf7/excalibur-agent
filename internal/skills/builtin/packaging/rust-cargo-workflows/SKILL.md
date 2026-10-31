---
name: rust-cargo-workflows
description: "Build, test and pin Rust projects with cargo, including vendored offline builds."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Rust workflows with cargo
      name_es: Flujos Rust con cargo
      desc_en: "Build, test and pin Rust projects with cargo, including vendored offline builds."
      desc_es: "Compilar, probar y fijar proyectos Rust con cargo, incluso sin red."
---
# Rust workflows with cargo

Use this skill for any Rust checkout: build offline when needed via vendoring, keep the lockfile sacred, and separate fast checks from slow ones.

## When to use

- Building, testing or linting a crate or workspace.
- No network: vendor dependencies first on a connected machine.
- Slow builds: split check, clippy and full test into stages.

## Procedure

1. Inspect with `cargo --version && rustc --version`; match `rust-toolchain.toml` if present (`rustup show`).
2. Fast loop first: `cargo check --workspace` (typecheck only), then `cargo clippy -- -D warnings`, then `cargo test --workspace`.
3. Release artifacts with `cargo build --release`; never commit `target/` (keep it gitignored).
4. Offline: `cargo vendor vendor` on a connected machine, add `[source.crates-io] replace-with = "vendored-sources"` to `.cargo/config.toml`, then `cargo build --offline` at the target. Keep `Cargo.lock` committed for binaries.
5. Update deliberately: `cargo update -p <pkg>` for one crate, full `cargo update` only when re-pinning everything.

## Pitfalls

- Deleting `Cargo.lock` for a binary re-resolves everything and hides version drift; commit it.
- `cargo test` compiles the world; use `cargo test <filter>` and `--lib` to iterate fast.
- Link errors at the end of a successful compile are system libraries, not Rust: see `linking-errors`.

## Verification

- `cargo check`, `cargo clippy` and `cargo test --workspace` are all green; `cargo build --offline` succeeds where required.
