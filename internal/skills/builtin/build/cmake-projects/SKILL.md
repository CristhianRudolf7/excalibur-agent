---
name: cmake-projects
description: "Configure and build CMake projects out-of-source with presets, flags and install prefixes."
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
      name_en: CMake projects
      name_es: Proyectos CMake
      desc_en: "Configure and build CMake projects out-of-source with presets, flags and install prefixes."
      desc_es: "Configurar y compilar proyectos CMake fuera del fuente con presets."
---
# CMake projects

Use this skill for any project with `CMakeLists.txt`. Always build out-of-source, prefer presets when the project ships them, and fix the toolchain before re-configuring.

## When to use

- Configuring (`cmake -S . -B build`) or rebuilding a CMake project.
- Switching compilers, build types or install prefixes.
- A stale cache produces confusing errors after the environment changed.

## Procedure

1. Prefer presets: `cmake --list-presets`, then `cmake --preset <name>`; otherwise configure explicitly:
   `cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/usr/local`.
2. Build and test: `cmake --build build -j"$(nproc)"` then `ctest --test-dir build --output-on-failure`.
3. Inspect cache when stuck: `cmake -LA -N build | grep -i <var>`; change with `-DVAR=value` and re-configure.
4. After toolchain or dependency changes, wipe the build dir and re-configure from scratch rather than fighting the cache.
5. Install staged: `DESTDIR=/tmp/stage cmake --install build` to review the file layout before touching the system.

## Pitfalls

- In-source builds (`cmake .`) pollute the tree; always use `-B build`.
- `CMAKE_BUILD_TYPE` only applies to single-config generators (Makefiles/Ninja); multi-config IDE generators need `--config`.
- Finding packages (`find_package`) honors `CMAKE_PREFIX_PATH`; point it at staged dependency installs.

## Verification

- Fresh `build/` from a clean configure passes `cmake --build` and `ctest` with no failures; `DESTDIR` staging shows the expected layout.
