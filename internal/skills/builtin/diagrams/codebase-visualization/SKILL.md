---
name: codebase-visualization
description: Visualize repository structure and dependencies from the real code graph.
metadata:
  excalibur:
    group: diagrams
    version: 1.1.0
    platforms:
    - linux
    tools:
    - code_diagram
    - code_graph
    display:
      name_en: Codebase visualization
      desc_en: Repository maps from indexed code, with bounded module and call details.
---
# Codebase visualization

Call `code_diagram` with a repository or subdirectory `path`. It indexes current
code without model calls or an LSP server and returns source and rendered artifacts.

- Start with `level: overview` for weighted package imports. Large maps also
  return up to six module details in the same artifact group; explain them together.
- Use `level: module` and a directory/import-path `focus` for files and dependencies.
- Use `level: symbols` with a directory or symbol `focus` for definitions, calls,
  and implementation relationships present in the graph.
- Use `level: flow`, an entry function `focus`, and `depth` (default 3) for call paths.
  Calls are static potential targets, not an observed execution trace. Use an exact
  symbol ID from `code_graph` when the name is ambiguous.

Keep `max_nodes` near 40. Nodes beyond the limit appear as `+N more`; narrow `path`
or `focus` and split further when details remain dense or the index is partial.
Choose `format: graphviz` for larger maps, `mermaid` for compact ones. Set
`direction`, `include_tests`, and `output` (`svg`, `png`, or `pdf`) as needed.
Review the rendered artifacts before explaining dependencies or suggesting changes.
