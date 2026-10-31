---
name: call-graph-analysis
description: Find how functions relate and call each other, with code_graph and lsp.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    - lsp
    display:
      name_en: Call graph analysis
      name_es: Análisis de grafo de llamadas
      desc_en: Find how functions relate and call each other, with code_graph and lsp.
      desc_es: Analizar llamadas y relaciones entre funciones.
---
# Call graph analysis

Use this skill when a change's blast radius is unclear. Callers, callees, and dependency paths show exactly what a modification can affect and which tests cover it.

## When to use

- Estimating the impact of a change.
- Tracing a bug through layers of calls.
- Finding dead code or circular dependencies.

## Procedure

1. Locate the target symbol with lsp and list its callers and callees via code_graph.
2. Walk the paths that matter for the task; prune branches that cannot be reached with the given inputs.
3. Identify the tests covering each affected edge.
4. Flag cycles, God callers, and seams where an interface would isolate the change.
5. Report the impact set before editing.

## Output

The impact set: affected callers/callees, paths, covering tests, and structural warnings.

## Pitfalls

- Static graphs miss dynamic dispatch; confirm hot paths by reading the code.
- Do not trust a partial graph; check the index is fresh.
