---
name: rust-language-notes
description: "Read and fix Rust code using the borrow checker, traits, Results and cargo errors."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Rust language notes
      name_es: Notas del lenguaje Rust
      desc_en: "Read and fix Rust code using the borrow checker, traits, Results and cargo errors."
      desc_es: "Leer y corregir Rust con borrow checker, traits y Results."
---
# Rust language notes

Use this skill on Rust code. The compiler is the mentor: read its errors fully, satisfy ownership first, and let `cargo` drive.

## When to use

- Fighting borrow-checker or lifetime errors.
- Choosing `Option`/`Result` handling, traits or async.
- Reading unfamiliar crates via docs and types.

## Procedure

1. Run `cargo check` and read the FULL error: rustc usually prints the fix (`consider cloning`, `borrowed value does not live long enough` with spans).
2. Fix ownership before logic: prefer borrowing (`&`), clone only at boundaries, restructure to shorten borrows instead of fighting lifetimes.
3. Handle errors with `?` and concrete error types (`thiserror` for libraries, `anyhow` for binaries); avoid `.unwrap()` outside tests and prototypes.
4. Explore APIs via types: `cargo doc --open`, rust-analyzer hover types, and trait bounds to discover available methods.
5. Format and lint always: `cargo fmt` and `cargo clippy -- -D warnings` before any test run (see `rust-cargo-workflows`).

## Pitfalls

- `Clone` everywhere compiles but hides design debt and copies; borrow first.
- Lifetimes on structs propagate; prefer owned data or scoped borrows over lifetime-parameterized APIs.
- Async needs an executor; bare `async fn` without a runtime never runs.

## Verification

- `cargo fmt --check`, `clippy` and `cargo test` all green; no new `unwrap()`/`expect()` in non-test code.
