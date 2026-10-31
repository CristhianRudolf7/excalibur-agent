---
name: archive-compression-formats
description: "Pack, unpack and inspect tar, zip, 7z and compressed formats without destroying data."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Archives and compression
      name_es: Archivos y compresión
      desc_en: "Pack, unpack and inspect tar, zip, 7z and compressed formats without destroying data."
      desc_es: "Empaquetar e inspeccionar tar, zip, 7z y comprimidos sin perder datos."
---
# Archives and compression

Use this skill for `.tar.gz`, `.zip`, `.7z` and friends. List before extracting, extract into a fresh directory, and pick the format deliberately.

## When to use

- Unpacking a downloaded archive or inspecting its contents.
- Creating distributable archives of build outputs or datasets.
- Choosing between gzip, xz, zstd and zip.

## Procedure

1. Identify and list first: `file data.tar.gz`, then `tar tzf data.tar.gz | head -20` or `unzip -l data.zip | head -30` — never extract blind.
2. Extract into a fresh dir: `mkdir out && tar xzf ../data.tar.gz -C out` (same with `unzip data.zip -d out`, `7z x data.7z -oout`); confirm no absolute paths or `..` entries before extracting as root.
3. Create reproducibly: `tar czf release.tar.gz -C src .` for gzip speed, `tar cJf` (xz, smallest) or `tar --zstd -cf` (zstd, fast modern default); `zip -r out.zip dir/` for Windows-friendly sharing.
4. Handle singles: `gzip -d file.gz` / `gunzip`, `xz -d`, `zstd -d`; keep the original with `-k` when it must survive.
5. Verify with `tar tzf`/`unzip -l` listings and checksums (`sha256sum`) recorded next to the artifact.

## Pitfalls

- Tarbombs (archives without a top-level dir) pollute the cwd; the fresh-dir habit prevents them.
- Absolute-path entries overwrite system files when extracted as root; inspect with `tar tvf` first.
- Compressing already-compressed media (mp4, jpg) wastes time for ~0% gain; store those instead.

## Verification

- Listing shows the expected tree, extraction reproduces byte-identical files (`diff -r` against source), and checksums match the published values.
