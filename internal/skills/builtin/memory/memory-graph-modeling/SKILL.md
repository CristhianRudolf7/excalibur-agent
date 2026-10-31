---
name: memory-graph-modeling
description: Represent, update and query the graph of people, interactions and their context respecting
  scopes, and adapt relation types to the user's domain (10.4).
metadata:
  excalibur:
    group: memory
    version: 1.0.0
    platforms:
    - linux
    tools:
    - memory_search
    - memory_write
    display:
      name_en: Memory graph modeling
      name_es: Modelado del grafo de memoria
      desc_en: Represent, update and query the graph of people, interactions and their context respecting
        scopes, and adapt relation types to the user's domain (10.4).
      desc_es: Representar y consultar el grafo de personas e interacciones por ámbitos.
---
# Memory graph modeling

Use this skill when the user's world must be modeled: people, projects, commitments, and how they relate. The graph answers 'who promised what to whom' without leaking across scopes.

## When to use

- Recording people, roles, commitments, or relationships.
- Answering questions that join facts across interactions.
- Adapting the relation vocabulary to the user's domain.

## Procedure

1. Identify entities (people, orgs, projects) and link each fact to its scope; never mix scopes in one write.
2. Use the domain's relation types; extend them only when the existing set cannot express the fact.
3. Write atomic facts with source and validity; supersede, never silently overwrite.
4. Query by traversal from the known entity, then filter by scope in SQL.
5. Deduplicate on write: check for an existing equivalent before adding.

## Output

Graph writes with scope, source, and validity; query answers with their paths.

## Pitfalls

- Scopes are a SQL filter, not a suggestion; cross-scope leaks are bugs.
- No fact without source and validity window.
