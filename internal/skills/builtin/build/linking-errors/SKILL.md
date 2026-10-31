---
name: linking-errors
description: "Diagnose undefined references, missing libraries and rpath problems at link and load time."
metadata:
  excalibur:
    group: build
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Linking errors
      name_es: Errores de enlazado
      desc_en: "Diagnose undefined references, missing libraries and rpath problems at link and load time."
      desc_es: "Diagnosticar referencias indefinidas, librerías y problemas de rpath."
---
# Linking errors

Use this skill when compilation succeeds but linking fails (`undefined reference`, `cannot find -lfoo`) or the binary refuses to start (`error while loading shared libraries`).

## When to use

- `undefined reference to ...` at link time.
- `/usr/bin/ld: cannot find -l<name>`.
- `./app: error while loading shared libraries: libfoo.so: cannot open shared object file`.

## Procedure

1. Classify: compile errors mention `.c/.cpp` lines; link errors mention `ld`, `undefined reference`, or `-l`; loader errors happen when RUNNING the binary.
2. Find the provider: `apt-file search` or `apt-cache search` for the header/lib; dev packages (`libfoo-dev`) provide both headers and the `.so` symlink.
3. Fix link order: objects first, then libraries (`gcc main.o -lfoo`); static vs shared with `-Wl,-Bstatic/-Bdynamic` when mixing.
4. Inspect binaries: `ldd ./app` (loader view), `nm -D libfoo.so | grep <sym>` (exported symbols), `readelf -d ./app | grep -i 'rpath|runpath|needed'` (embedded search paths).
5. Fix runtime search: prefer `-Wl,-rpath,/path/lib` at link time over `LD_LIBRARY_PATH` hacks; use `ldconfig` only for real system installs.

## Pitfalls

- Name mangling: C++ symbols differ from C; missing `extern "C"` on C headers included from C++ is a classic.
- Mixing compilers or C++ standard libraries (libstdc++ vs libc++) across objects breaks links silently.
- `LD_LIBRARY_PATH` in checked-in scripts hides broken rpaths; fix the link instead.

## Verification

- `ldd ./app` shows no `not found` lines and the binary runs to its usage message.
- A rebuild from clean reproduces the success.

## References

- `references/link-diagnostics.md` — command table for ld, nm, readelf and ldd.
