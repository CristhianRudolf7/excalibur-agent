---
name: data-flow-diagrams
description: Data flow diagrams.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    display:
      name_en: Data flow diagrams
      name_es: Diagramas de flujo de datos
      desc_en: Data flow diagrams.
      desc_es: Diagramas de flujo de datos.
---
# Data flow diagrams

Use this skill to show where data moves and transforms. Sources, stores, processes, and flows at consistent levels reveal missing inputs and unguarded crossings.

## When to use

- Mapping data movement through a system.
- Privacy or compliance needs the data picture.
- Integrations multiply and nobody sees the whole.

## Procedure

1. List external sources and sinks first; everything crosses them.
2. Draw processes with their inputs and outputs labeled by content.
3. Show stores with what persists and for how long.
4. Mark trust and scope crossings explicitly.
5. Balance levels: every flow into a process decomposes consistently.

## Output

Leveled data flows with sources, stores, and crossings marked.

## Pitfalls

- Unlabeled flows hide the important part: what moves.
- Crossings unmarked are leaks waiting.
