---
name: image-processing-cli
description: "Resize, convert and inspect images from the command line with ImageMagick and friends."
metadata:
  excalibur:
    group: media
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Image processing CLI
      name_es: Procesar imágenes por CLI
      desc_en: "Resize, convert and inspect images from the command line with ImageMagick and friends."
      desc_es: "Redimensionar, convertir e inspeccionar imágenes con ImageMagick."
---
# Image processing CLI

Use this skill for batch image work: thumbnails, format conversion, metadata and contact sheets. Script it; never hand-edit dozens of files.

## When to use

- Resizing or converting images in bulk.
- Reading dimensions, EXIF or color profiles before processing.
- Stripping metadata or normalizing orientation.

## Procedure

1. Inspect: `identify -verbose img.jpg | head -40` (dimensions, depth, EXIF orientation, profile); `exiftool img.jpg` for full metadata.
2. Convert and resize: `magick in.png -resize 800x600\> -quality 85 out.jpg` (`\>` shrinks only, never enlarges); batch with `mogrify -path thumbs -thumbnail 256x256 *.jpg` (writes to a separate dir).
3. Normalize orientation: `-auto-orient` before resizing; strip metadata with `-strip` when publishing (keeps color intent via `-sRGB` when needed).
4. Compare and verify: `magick compare -metric RMSE a.png b.png diff.png` (exit 0 means identical); eyeball the contact sheet `magick montage *.jpg -tile 4x -geometry +2+2 sheet.jpg`.
5. Preserve originals: write to a new directory, never `mogrify` in place on the only copy.

## Pitfalls

- `mogrify` overwrites in place; one wrong flag destroys the set — use `-path` output dirs.
- Resizing without `-auto-orient` bakes the EXIF rotation wrongly on some viewers.
- JPEG re-saves lose quality each round; keep a lossless master (PNG/TIFF) and derive JPEGs.

## Verification

- `identify` confirms target dimensions and format; the montage eyeball passes; originals untouched in their directory.
