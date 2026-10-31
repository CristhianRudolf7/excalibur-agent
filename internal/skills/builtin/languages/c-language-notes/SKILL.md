---
name: c-language-notes
description: "Read, build and debug C99/C11 code: pointers, memory, headers and undefined behavior."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: C language notes
      name_es: Notas del lenguaje C
      desc_en: "Read, build and debug C99/C11 code: pointers, memory, headers and undefined behavior."
      desc_es: "Leer, compilar y depurar C99/C11: punteros, memoria y UB."
---
# C language notes

Use this skill on C codebases. Compile strict with sanitizers first; most C bugs are memory or undefined behavior, not logic.

## When to use

- Reading or fixing C sources and headers.
- Segfaults, corrupted output, or "works with -O0, breaks with -O2".
- Writing small C helpers, test harnesses or reproducers.

## Procedure

1. Compile strict: `gcc -std=c11 -Wall -Wextra -Werror -g -O1 file.c -o prog` (see `gcc-clang-toolchain`).
2. Reproduce under sanitizers: `-fsanitize=address,undefined`; fix every report — it is a real bug even when the program "works".
3. Debug with `gdb ./prog`, `run`, `bt` on crash; print pointers with `p/x ptr` and watch ownership at malloc/free pairs.
4. Respect the header contract: include guards or `#pragma once`, no definitions (only declarations) in headers, `static` for file-local helpers.
5. Check error returns: every `malloc`, `fopen`, `read`/`write` can fail; propagate with clear cleanup paths.

## Pitfalls

- Use-after-free, double free and buffer overruns are undefined behavior: the compiler may "optimize" them into anything.
- `char` signedness, integer promotion and `sizeof` on pointers (not arrays) are perennial traps.
- `strcpy`/`sprintf` without bounds are bugs; prefer `snprintf` and explicit lengths.

## Verification

- Strict-warning build has zero warnings; ASan/UBSan run is clean; the reproducer passes under both `-O0` and `-O2`.
