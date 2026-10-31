---
name: visio-diagrams
description: Read and create Visio diagrams (VSDX).
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_read
    - document_create
    display:
      name_en: Visio diagrams
      name_es: Diagramas de Visio
      desc_en: Read and create Visio diagrams (VSDX).
      desc_es: Leer y crear diagramas de Visio.
---
# Visio diagrams

Use this skill for VSDX diagrams that must stay editable in Visio. Shapes, connectors, and stencils preserved — not flattened pictures.

## When to use

- Reading or creating Visio files.
- Diagrams must round-trip through Visio.
- Migrating diagrams to or from VSDX.

## Procedure

1. Read the diagram: pages, shapes, connectors, and stencil sources.
2. Edit with proper shapes and glued connectors, not loose lines.
3. Keep corporate stencils and styles applied consistently.
4. Verify the file opens in Visio with layout intact.
5. Export renditions for reviewers without Visio.

## Output

Editable VSDX with proper shapes and connectors.

## Pitfalls

- Unglued connectors drift; always glue to connection points.
- Verify in real Visio, not just in the file bytes.
