---
name: ocaml-opam-packaging
description: "Build OCaml projects with opam switches, dune and opam file pinning."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: OCaml packaging with opam
      name_es: Empaquetado OCaml con opam
      desc_en: "Build OCaml projects with opam switches, dune and opam file pinning."
      desc_es: "Compilar proyectos OCaml con switches opam, dune y pinning."
---
# OCaml packaging with opam

Use this skill for OCaml checkouts: pick the right compiler switch, install pinned deps, and build with dune.

## When to use

- A project has `.opam` files, `dune-project` or an `ocamlformat` config.
- Compiler or library version mismatches.
- Setting up a reproducible OCaml environment.

## Procedure

1. Check `opam --version && ocaml -version && dune --version`; list switches with `opam switch list`.
2. Create a local switch per project: `opam switch create . <version> --deps-only` or install deps with `opam install . --deps-only`.
3. Build with `dune build`, test with `dune runtest`, format-check with `dune fmt` when configured.
4. Pin exact deps: `opam pin add <pkg> <version>` for overrides; commit the resulting `.opam` constraints.
5. Enter the env via `eval $(opam env)` after switching; verify `which ocaml` points at the switch.

## Pitfalls

- The system OCaml and the opam switch are different compilers; always `eval $(opam env)` in fresh shells.
- Global switches leak between projects; prefer local (`./_opam`) switches.
- Missing system libs (`libgmp-dev`, `m4`) break switch creation: install them first (see `apt-offline-installs`).

## Verification

- `dune build` and `dune runtest` pass inside the activated switch on a clean checkout.
