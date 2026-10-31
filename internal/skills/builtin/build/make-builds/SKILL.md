---
name: make-builds
description: "Drive make-based builds: read the Makefile first, then build, test and clean targets."
metadata:
  excalibur:
    group: build
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - read_file
    display:
      name_en: Make-based builds
      name_es: Compilaciones con make
      desc_en: "Drive make-based builds: read the Makefile first, then build, test and clean targets."
      desc_es: "Leer el Makefile y usar sus targets de build, test y clean."
---
# Make-based builds

Use this skill for any project with a `Makefile`. The Makefile is the documentation: read it before running anything, then use its targets instead of hand-rolled commands.

## When to use

- A `Makefile`, `makefile` or `*.mk` exists in the project.
- Choosing between `make`, `make all`, `make test`, `make install`.
- A make build fails and the error scrolls past the real cause.

## Procedure

1. Read the Makefile top: default target, `all/test/clean/install` targets, overridable variables (`CC`, `CFLAGS`, `PREFIX`).
2. Build in order: `make -j"$(nproc)"` then the project's own `make test` / `make check` target; never skip to install first.
3. Override cleanly on the command line: `make CC=clang CFLAGS='-O2 -g' PREFIX=/usr/local`.
4. On failure, re-run serially with `make -j1` to get the first real error, and `make -n <target>` to preview commands without running them.
5. Clean with `make clean` / `make distclean` before a from-scratch verification build.

## Pitfalls

- Tabs are syntax: if you edit a Makefile, keep recipe lines tab-indented or make fails cryptically.
- `make install` without `PREFIX`/`DESTDIR` pollutes the system; stage with `make DESTDIR=/tmp/stage install` first.
- Parallel builds (`-j`) can hide ordering bugs; a green parallel build plus a red serial build means a missing dependency.

## Verification

- A clean-checkout `make -j` plus the project's test target passes; `make -n install` shows files landing under the intended prefix.
