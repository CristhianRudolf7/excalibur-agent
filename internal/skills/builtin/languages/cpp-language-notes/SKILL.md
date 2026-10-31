---
name: cpp-language-notes
description: "Read, build and fix modern C++ code: RAII, templates, build flags and ABI care."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: C++ language notes
      name_es: Notas del lenguaje C++
      desc_en: "Read, build and fix modern C++ code: RAII, templates, build flags and ABI care."
      desc_es: "Leer, compilar y corregir C++ moderno: RAII, plantillas y ABI."
---
# C++ language notes

Use this skill on C++ codebases. Fix the standard version and warnings first; template errors read bottom-up and ABI mismatches link last.

## When to use

- Reading or fixing C++ sources, headers or templates.
- `undefined reference` to symbols that visibly exist (see also `linking-errors`).
- Choosing smart pointers, containers or error handling in new code.

## Procedure

1. Fix the standard: `-std=c++17` (or the project's version) plus `-Wall -Wextra -Werror -g`; mismatched standards across objects break ODR.
2. Read template errors bottom-up: the last lines name the actual constraint failure; concepts (C++20) failures read top-down instead.
3. Manage ownership with RAII: `unique_ptr` by default, `shared_ptr` only for shared ownership, references for non-owning views; never raw `new`/`delete` in new code.
4. Include C headers with `extern "C"` guards; keep header-only code inline-safe and respect one-definition rules.
5. Test with the project's runner (ctest, bazel test, or plain asserts); sanitize new code with ASan/UBSan.

## Pitfalls

- ODR violations (different definitions in different TUs) and mixing `libstdc++`/`libc++` cause link or runtime corruption.
- Exceptions across module boundaries and throwing from destructors end programs; check the project's policy first.
- Implicit conversions and narrowing hide bugs; `-Wconversion` on new code is worth the noise.

## Verification

- Strict-warning build clean under the project's standard; test suite green; ASan run of new paths exits 0.
