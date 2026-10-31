---
name: er-diagrams
description: Entity-relationship diagrams of databases.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    - userdb
    display:
      name_en: ER diagrams
      name_es: Diagramas entidad-relación
      desc_en: Entity-relationship diagrams of databases.
      desc_es: Diagramas entidad-relación de bases de datos.
---
# ER diagrams

Use this skill to draw data models. Entities, keys, and cardinalities on one page expose modeling mistakes before they become migrations.

## When to use

- Designing or documenting a schema.
- A data model needs review.
- Relations confuse the team.

## Procedure

1. List entities with their keys and essential attributes.
2. Draw relations with exact cardinalities, not vague lines.
3. Mark constraints the diagram implies: uniqueness, optionality, cascades.
4. Check the diagram against the real schema or migration set.
5. Keep one diagram per bounded context; split monsters.

## Output

An ER diagram with keys, cardinalities, and constraints, schema-checked.

## Pitfalls

- Vague cardinalities design bugs; be exact.
- Diagrams divorced from migrations diverge; sync them.
