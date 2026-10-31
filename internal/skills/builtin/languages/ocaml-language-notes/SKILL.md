---
name: ocaml-language-notes
description: "Read and fix OCaml code: dune builds, pattern matching, options/results and opam envs."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: OCaml language notes
      name_es: Notas del lenguaje OCaml
      desc_en: "Read and fix OCaml code: dune builds, pattern matching, options/results and opam envs."
      desc_es: "Leer y corregir OCaml: dune, pattern matching y options/results."
---
# OCaml language notes

Use this skill on OCaml code. The type checker points at the inconsistency; exhaustive matching plus explicit option/result handling removes most runtime surprises.

## When to use

- Reading or fixing `.ml`/`.mli` files in a dune project.
- Non-exhaustive match warnings or `Match_failure` at runtime.
- Using libraries from opam in project code.

## Procedure

1. Enter the env: `eval $(opam env)`; build with `dune build`, test with `dune runtest` (see `ocaml-opam-packaging`).
2. Treat warnings as errors during development (`-w +A` in dev profiles); fix non-exhaustive matches by adding the missing cases, not by `_ ->` catch-alls that hide logic.
3. Model absence and failure with `option` and `result`; unwrap at boundaries with explicit matches, never `Option.get` on uncertain values.
4. Read interfaces first: `.mli` files state the contract; implementations follow. Use `ocamlformat` to normalize style.
5. Debug with `print_endline`/`Printf.eprintf` for quick traces and `ocamldebug` or bisect_ppx for deeper dives.

## Pitfalls

- `=` is structural equality and `==` is physical; mixing them on mutable or float values misbehaves.
- Partial functions (`List.hd`, `Option.get`) throw; total functions plus matches are the safe default.
- Mutable state (`ref`, arrays) defeats equational reasoning; confine it and document invariants.

## Verification

- `dune build` warning-free and `dune runtest` green; new matches cover all constructors (no catch-all hiding cases).
