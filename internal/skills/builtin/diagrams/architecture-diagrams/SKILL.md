---
name: architecture-diagrams
description: System and component architecture diagrams (including the C4 model).
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
      name_en: Architecture diagrams
      name_es: Diagramas de arquitectura
      desc_en: System and component architecture diagrams (including the C4 model).
      desc_es: Diagramas de arquitectura y componentes (modelo C4).
---
# Architecture diagrams

Use this skill to draw how the system is built. C4 levels — context, containers, components — each answer different questions; drawing the right level keeps the diagram useful.

## When to use

- Documenting or deciding system structure.
- Onboarding on a complex system.
- Reviews need a shared structural picture.

## Procedure

1. Choose the C4 level the question needs; do not mix levels in one diagram.
2. Derive containers and components from the code graph, not from memory.
3. Show dependencies with direction and protocol; mark async and trust boundaries.
4. Keep the diagram to one message; split by level or subsystem otherwise.
5. Review against the code; update when the code moves.

## Output

Level-correct architecture diagrams grounded in the code.

## Pitfalls

- Mixed-level diagrams confuse; one level per diagram.
- Stale architecture diagrams mislead; refresh with the code.
