---
name: video-quality-check
description: Detect missing files, black screens, out-of-frame texts and audio problems before exporting.
metadata:
  excalibur:
    group: creative-tools
    version: 1.0.0
    platforms:
    - linux
    tools:
    - video_timeline_ops
    - vision_extract
    display:
      name_en: Video quality check
      name_es: Control de calidad de vídeo
      desc_en: Detect missing files, black screens, out-of-frame texts and audio problems before exporting.
      desc_es: Detectar fallos antes de exportar.
---
# Video quality check

Use this skill as the pre-export gate. Missing media, black frames, cropped text, and audio faults caught here cost minutes; caught by viewers they cost reputation.

## When to use

- Before any export or publish.
- After conforming, relinking, or version changes.
- A previous export shipped a defect.

## Procedure

1. Verify every media file is linked and at full resolution.
2. Scan for black frames, frozen frames, and gaps in video and audio.
3. Check all text for safe framing at the delivery aspect ratio.
4. Listen through on meters and ears: levels, sync, dropouts, hum.
5. Block the export on any finding; re-check after fixes.

## Output

A QC verdict with findings fixed or the export blocked.

## Pitfalls

- Skipped QC ships defects; gate every export.
- Meters plus ears; neither alone suffices.
