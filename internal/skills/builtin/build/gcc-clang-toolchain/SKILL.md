---
name: gcc-clang-toolchain
description: "Compile and diagnose with gcc and clang: warning flags, sanitizers, include and library paths."
metadata:
  excalibur:
    group: build
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: GCC and Clang toolchain
      name_es: Toolchain GCC y Clang
      desc_en: "Compile and diagnose with gcc and clang: warning flags, sanitizers, include and library paths."
      desc_es: "Compilar y diagnosticar con gcc y clang: warnings, sanitizers y rutas."
---
# GCC and Clang toolchain

Use this skill when compiling C/C++ directly or when a build system hides the real compiler error. Explicit warning flags and sanitizers turn runtime mysteries into compile-time messages.

## When to use

- Compiling C/C++ files by hand or debugging a build-system failure.
- Turning on warnings, debug info or sanitizers.
- Missing headers or "cannot find -l" link failures.

## Procedure

1. Baseline strict compile: `gcc -Wall -Wextra -Werror -g -O1 main.c -o main` (same with `clang`); keep `-g` always so crashes are debuggable.
2. Point at staged deps: `-I/path/include` for headers, `-L/path/lib -lfoo` for libraries, `-Wl,-rpath,/path/lib` when the loader must find them at runtime.
3. Debug memory and UB fast with sanitizers: `-fsanitize=address,undefined -fno-omit-frame-pointer`, then run; fix every report before optimizing.
4. Inspect the toolchain: `gcc --version`, `clang --version`, `cpp -v` (include search order), `ld --version`.
5. Preprocess or assemble on demand: `-E` (preprocessed output), `-S` (assembly), `-v` (full driver trace).

## Pitfalls

- Order matters on link lines: libraries go AFTER the objects that use them (`gcc main.o -lfoo`, not `gcc -lfoo main.o`).
- `-O2` without `-g` gives fast but undebuggable crashes; develop with `-O1 -g`.
- Different compilers accept different flags; gate them per compiler in build files instead of hardcoding.

## Verification

- The strict-warning build is warning-free; the sanitizer run exits 0; `ldd ./main` resolves every library.
