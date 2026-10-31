---
name: codebase-visualization
description: Graphical representations of the structure and relations of large repositories from the code
  graph.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    - diagram_render
    display:
      name_en: Codebase visualization
      name_es: Visualización de código
      desc_en: Graphical representations of the structure and relations of large repositories from the
        code graph.
      desc_es: Representaciones gráficas de repositorios grandes.
---
# Codebase visualization

Use this skill to see a large codebase at a glance. Structure and dependency pictures from the code graph orient faster than any README on repos that outgrew prose.

## When to use

- Onboarding onto a large repository.
- Finding layering violations or tangled modules.
- Planning a restructuring.

## Procedure

1. Extract modules and dependencies from the code graph at the chosen granularity.
2. Render with clustering by layer or domain; highlight cycles and hubs.
3. Filter noise: generated code, vendored trees, and test fixtures dimmed or hidden.
4. Annotate the surprises: unexpected dependencies and God modules.
5. Refresh after restructures; dated maps mislead.

## Output

A codebase map with cycles, hubs, and annotated surprises.

## Pitfalls

- Unfiltered graphs are hairballs; aggregate and filter.
- Maps age with the code; refresh or retire.
