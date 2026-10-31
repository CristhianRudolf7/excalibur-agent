---
name: elf-binary-analysis
description: "Inspect ELF binaries statically: headers, symbols, strings and linked libraries."
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: ELF binary analysis
      name_es: Análisis de binarios ELF
      desc_en: "Inspect ELF binaries statically: headers, symbols, strings and linked libraries."
      desc_es: "Inspeccionar binarios ELF: cabeceras, símbolos, strings y librerías."
---
# ELF binary analysis

Use this skill to understand an unknown Linux binary without running it: what it is, what it needs, and what interesting strings it contains.

## When to use

- Triaging an unfamiliar binary (built where, for what arch, static or dynamic).
- Finding hardcoded paths, URLs, credentials or version banners.
- Checking hardening (PIE, RELRO, stack protector, stripping) before deeper work.

## Procedure

1. Identify: `file ./prog` (arch, static/dynamic, stripped?) and `readelf -h ./prog` (class, machine, entry point).
2. Map dependencies: `ldd ./prog` (or `readelf -d` for static-view NEEDED entries); missing libs explain loader failures (see `linking-errors`).
3. List symbols: `nm -D` for dynamic symbols, `nm` for full tables when unstripped; `strings -n 8 ./prog | grep -iE 'https?://|passwd|key|/etc|/tmp'` for embedded artifacts.
4. Check hardening: `checksec --file=./prog` when available, else `readelf -lW` (GNU_RELRO, GNU_STACK) and `readelf -d | grep BIND_NOW`.
5. Disassemble targeted functions only: `objdump -d --disassemble=<sym>` or `objdump -M intel -d` slices; never dump megabytes blindly.

## Pitfalls

- Running the binary is dynamic analysis, not static: sandbox first (throwaway VM/container) and never as root.
- Stripped binaries lose names but keep strings and imports; behavior hypotheses still need dynamic confirmation.
- `strings` output is suggestive, not conclusive: an embedded URL may be dead code or a default.

## Verification

- File type, arch, linkage and hardening recorded; interesting strings catalogued with offsets; hypotheses phrased as testable dynamic checks.
