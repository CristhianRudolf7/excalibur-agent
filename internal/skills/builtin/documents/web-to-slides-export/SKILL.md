---
name: web-to-slides-export
description: Convert web designs (HTML, SVG) into editable PPTX and check visual fidelity and which elements
  stay editable.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_create
    - browser_snapshot
    display:
      name_en: Web to slides export
      name_es: Exportar web a diapositivas
      desc_en: Convert web designs (HTML, SVG) into editable PPTX and check visual fidelity and which
        elements stay editable.
      desc_es: Convertir diseños web en PPTX editables.
---
# Web to slides export

Use this skill to move web designs into editable decks. Fidelity checks plus an editability report tell the user exactly what survived the conversion.

## When to use

- A web mockup must become a PPTX.
- Designers need editable slides from HTML/SVG.
- Checking what a conversion preserved.

## Procedure

1. Capture the source rendering as the fidelity reference.
2. Convert with structure mapping: text stays text, shapes stay shapes.
3. Compare renderings side by side; log every visual deviation.
4. Report editability per element class: what is text, what became image.
5. Fix the deviations that matter; document the rest as known limits.

## Output

An editable PPTX with a fidelity and editability report.

## Pitfalls

- Image-only slides are a failed export; maximize real text and shapes.
- Report limits honestly instead of hiding rasterized parts.
