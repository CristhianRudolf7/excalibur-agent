---
name: diagram-rendering
description: Choose notation (Mermaid, PlantUML, Graphviz, SVG, draw.io), render with diagram_render and
  visually check the result before delivering.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    display:
      name_en: Diagram rendering
      name_es: Renderizado de diagramas
      desc_en: Choose notation (Mermaid, PlantUML, Graphviz, SVG, draw.io), render with diagram_render
        and visually check the result before delivering.
      desc_es: Elegir notación, renderizar y comprobar visualmente.
---
# Diagram rendering

Use this skill for every diagram. Right notation for the job, rendered output, and a visual check before delivery — syntax errors caught by eyes, not by users.

## When to use

- Any diagram is needed.
- Choosing between diagram notations.
- A rendered diagram looks wrong.

## Procedure

1. Pick the notation fitting the diagram and the team's toolchain.
2. Write the source with layout hints: direction, grouping, and labels that read.
3. Render and inspect the image yourself; fix overlaps, cut labels, and crossings.
4. Iterate until the diagram reads at the delivery size.
5. Deliver source plus render so the diagram stays editable.

## Output

A checked rendered diagram with its editable source.

## Pitfalls

- Undelivered-at-size checks hide unreadable diagrams; view at final size.
- Never deliver a diagram you have not looked at.
