---
name: cross-compilation
description: "Build for another OS or CPU with cross toolchains, sysroots and portable checks."
metadata:
  excalibur:
    group: build
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Cross-compilation
      name_es: Compilación cruzada
      desc_en: "Build for another OS or CPU with cross toolchains, sysroots and portable checks."
      desc_es: "Compilar para otro SO o CPU con toolchains cruzados y sysroots."
---
# Cross-compilation

Use this skill when the deliverable runs on a different OS or CPU than the build machine. Isolate host tools from target code early; most failures come from mixing them.

## When to use

- Building Linux binaries for ARM/AArch64/macOS/Windows from an x86_64 Linux host.
- A configure or CMake probe tries to RUN target code on the host.
- Verifying portability without owning the target hardware.

## Procedure

1. Prefer the easy wins first: Go (`GOOS=darwin GOARCH=arm64 go build`), Rust (`rustup target add <triple>` + `cargo build --target <triple>`).
2. For C/C++ install a cross toolchain (`gcc-aarch64-linux-gnu`, `mingw-w64` for Windows) and point the build at it: `CC=aarch64-linux-gnu-gcc`, CMake `CMAKE_SYSTEM_NAME` + `CMAKE_SYSROOT`, autotools `--host=aarch64-linux-gnu`.
3. Never run target binaries during the build; set cross modes explicitly (`CMAKE_CROSSCOMPILING`, `CONFIGURE --host`, pkg-config sysroot wrappers).
4. Verify without hardware: `file ./app` must name the target arch/OS; run userspace checks under `qemu-<arch> ./app` when available (see `qemu-virtual-machines`).
5. Keep host-only codegen tools (protoc, code generators) built natively and invoked as separate host steps.

## Pitfalls

- cgo and native extensions defeat "pure" cross builds; disable cgo or cross-build the C parts too.
- Probing `sizeof` or endianness by running code silently tests the HOST; cross files must hardcode target answers.
- Windows targets need `.exe` suffixes and different path/library conventions; test the artifact layout, not just the compile.

## Verification

- `file` reports the target triple; the test suite runs green under QEMU or on real target hardware; no host-arch objects leak in (`find` + `file` over the staging dir).
