---
name: diagram-rendering
description: Render Mermaid, D2, PlantUML, Graphviz, SVG or draw.io diagrams and inspect the result before delivery.
metadata:
  excalibur:
    group: diagrams
    version: 1.1.0
    platforms:
    - linux
    tools:
    - diagram_render
    display:
      name_en: Diagram rendering
      desc_en: Render Mermaid, D2, PlantUML, Graphviz, SVG or draw.io diagrams and inspect the result before delivery.
---
# Diagram rendering

## When to use

Create or revise a diagram with editable source and a rendered image.

## Procedure

1. Choose a notation supported by the local toolchain: Mermaid (`mmdc`), D2 (`d2`), Graphviz (`dot`), PlantUML (`plantuml` or Java with its jar), draw.io (`drawio`), or SVG.
2. Call `diagram_render` with `source`, `dialect`, and `output_format` (`svg`, `png`, or `pdf`). Set `output` for a specific destination; otherwise files are saved under `artifacts/`. An optional `title` captions the artifact.
3. Fix reported `line N: message` errors and render again. If a binary is missing, follow the install hint or choose an available dialect. Remote rendering requires an explicitly configured self-hosted `EXCALIBUR_KROKI_URL`.
4. Inspect the rendered image at its delivery size. Correct overlaps, clipped labels, and crossings before delivery.
5. Deliver the artifact's `Path` and `SourcePath`. The tool saves both files and reports the renderer or converter used; SVG source is kept separately from sanitized SVG output.

## Output

A checked diagram and its editable source, referenced by `Result.Artifacts`.

## Pitfalls

- Missing renderers produce actionable errors; a source-text placeholder is not a rendered diagram.
- PNG/PDF require native renderer support or an installed SVG converter. Do not assume a format worked without checking the result.
