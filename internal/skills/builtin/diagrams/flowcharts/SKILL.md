---
name: flowcharts
description: Flow and process diagrams, including BPMN.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    display:
      name_en: Flowcharts
      name_es: Diagramas de flujo
      desc_en: Flow and process diagrams, including BPMN.
      desc_es: Diagramas de flujo y procesos, incluido BPMN.
---
# Flowcharts

Use this skill for processes with decisions. Explicit branches, loops, and end states turn tribal procedure into a checkable picture.

## When to use

- Documenting a process or procedure.
- A flow has branches people argue about.
- BPMN semantics are required.

## Procedure

1. Fix the start, the end states, and the decision points first.
2. Draw every branch to its end; no arrows into the void.
3. Mark loops with their exit condition and owners per step where it matters.
4. Use BPMN properly when the audience requires it: events, gateways, pools.
5. Walk the diagram with a real case before delivering.

## Output

Complete flowcharts with all branches resolved.

## Pitfalls

- Branches without ends are unfinished thinking.
- Walk a real case through; theory flows hide gaps.
