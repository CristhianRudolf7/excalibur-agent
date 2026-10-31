---
name: sequence-diagrams
description: Sequence diagrams of interactions between actors and systems.
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
      name_en: Sequence diagrams
      name_es: Diagramas de secuencia
      desc_en: Sequence diagrams of interactions between actors and systems.
      desc_es: Diagramas de secuencia entre actores y sistemas.
---
# Sequence diagrams

Use this skill for interactions over time. Ordered messages with return values and error paths show what actually happens between actors — the dynamics static diagrams miss.

## When to use

- Documenting protocols or call sequences.
- Debugging interaction bugs across services.
- Designing multi-party flows.

## Procedure

1. List the actors and the trigger that starts the sequence.
2. Order messages with parameters and returns; number them.
3. Draw the error and timeout paths, not just the happy one.
4. Ground participants and calls in the code graph.
5. Check the sequence against a real trace when one exists.

## Output

A complete sequence with happy and failure paths.

## Pitfalls

- Happy-path-only sequences document wishes.
- Ground calls in code or traces, not imagination.
