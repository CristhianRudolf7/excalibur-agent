---
name: fortran-language-notes
description: "Read, build and fix Fortran code: fixed vs free form, modules, arrays and common compilers."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Fortran language notes
      name_es: Notas del lenguaje Fortran
      desc_en: "Read, build and fix Fortran code: fixed vs free form, modules, arrays and common compilers."
      desc_es: "Leer, compilar y corregir Fortran: formatos, módulos y arrays."
---
# Fortran language notes

Use this skill on Fortran codebases (scientific models, legacy libraries). Identify the source form and standard first; implicit typing is the historic trap.

## When to use

- Compiling `.f`, `.f90`, `.F90` sources with gfortran/ifx.
- `Implicit type` surprises, shape mismatches, or segfaults in array code.
- Interfacing Fortran routines with C or Python.

## Procedure

1. Identify the form: `.f`/`.for` is fixed-form (columns matter, 72-char lines); `.f90`+ is free-form. Never mix conventions in one file.
2. Compile with `gfortran -std=f2018 -Wall -Wextra -g -fcheck=all -O1 file.f90 -o prog`; `-fcheck=all` catches array-shape and bounds bugs at runtime.
3. Start every unit with `implicit none`; declare intents (`intent(in/out/inout)`) on dummy args and use modules over `common` blocks.
4. Remember column-major arrays: loop the first index fastest; pass explicit-shape or assumed-shape (`(:)`) dummies with interfaces so the compiler checks shapes.
5. For C interop use `iso_c_binding` (`bind(C)`); for Python use `f2py` or expose a C-compatible layer first.

## Pitfalls

- Without `implicit none`, undeclared variables silently become typed by first letter (i–n integer, else real).
- Fixed-form comments (`C`, `*` in column 1) and line continuations (char in column 6) corrupt code pasted from free-form sources.
- Uninitialized variables and out-of-bounds writes are silent without `-fcheck=all`; develop with it on.

## Verification

- Strict-warning build clean; `-fcheck=all` run passes; results match a reference output to the expected tolerance.
