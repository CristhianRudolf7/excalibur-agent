---
name: autotools-projects
description: "Build autotools projects through the configure, make and make check sequence."
metadata:
  excalibur:
    group: build
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Autotools projects
      name_es: Proyectos autotools
      desc_en: "Build autotools projects through the configure, make and make check sequence."
      desc_es: "Compilar proyectos autotools con configure, make y make check."
---
# Autotools projects

Use this skill for projects with `configure.ac` / `configure` scripts. The canonical sequence is configure, make, check; generating the build system is a separate step.

## When to use

- A project ships `configure` (or only `configure.ac` plus `autogen.sh`).
- `./configure` fails on a missing dependency or rejects a prefix.
- Running the project's own test suite (`make check`).

## Procedure

1. If no `configure` exists, generate it: `./autogen.sh` or `autoreconf -fi` (needs autoconf/automake/libtool installed).
2. Configure out-of-tree when supported: `mkdir build && cd build && ../configure --prefix=/usr/local`; otherwise in-tree `./configure --prefix=/usr/local`.
3. Read `config.log` (not just stdout) when a check fails; it shows the exact test program and error.
4. Build and test: `make -j"$(nproc)" && make check`; install with `make install` or staged `make DESTDIR=/tmp/stage install`.
5. Common knobs: `CC=clang`, `CFLAGS='-O2 -g'`, `--enable/--disable-<feature>`, `PKG_CONFIG_PATH` for staged deps.

## Pitfalls

- A stale `config.cache` lies after environment changes; `rm config.cache` or re-run with a clean tree.
- `configure` checks are compile-link probes: missing `-dev` headers fail them (see `apt-offline-installs`, `linking-errors`).
- Never `make install` as root without reviewing `make -n install` first.

## Verification

- `config.status --version` and `make check` pass; `config.log` shows no failed required check you ignored.
