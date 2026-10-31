---
name: class-diagrams
description: Class diagrams and other structural UML diagrams.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    - code_graph
    display:
      name_en: Class diagrams
      name_es: Diagramas de clases
      desc_en: Class diagrams and other structural UML diagrams.
      desc_es: Diagramas de clases y UML estructural.
---
# Class diagrams

Use this skill for structural overviews. Classes with members and relations at the right abstraction show design shape without drowning in method lists.

## When to use

- Documenting object structure.
- Reviewing a design's coupling.
- Onboarding onto an OO codebase.

## Procedure

1. Show the classes that matter with key members, not every getter.
2. Draw relations by kind: association, aggregation, inheritance, dependency.
3. Mark interfaces and where implementations vary.
4. Derive the diagram from the code graph; prune generated noise.
5. Keep it to one subsystem per diagram.

## Output

Structural UML grounded in code, at reviewable size.

## Pitfalls

- Method-list diagrams are code in boxes; abstract up.
- Generated noise unpruned buries the design.
