---
name: sql-database-design
description: Design user-tailored databases (tables, relations, constraints and migrations) for userdb.
metadata:
  excalibur:
    group: research
    version: 1.0.0
    platforms:
    - linux
    tools:
    - userdb
    display:
      name_en: SQL database design
      name_es: Diseño de bases de datos SQL
      desc_en: Design user-tailored databases (tables, relations, constraints and migrations) for userdb.
      desc_es: Diseñar bases de datos a medida para userdb.
---
# SQL database design

Use this skill for the user's own databases. Requirements-driven tables, real constraints, and planned migrations keep userdb reliable as it grows.

## When to use

- A user needs structured storage for their data.
- Spreadsheets outgrow their purpose.
- Designing the schema behind an app or analysis.

## Procedure

1. Model the domain: entities, relations, and cardinalities from real examples.
2. Create tables with keys and constraints that enforce the rules, not just describe them.
3. Plan migrations for every later change; no destructive surprises.
4. Seed with representative data and verify the queries the user actually needs.
5. Document the schema where its users will find it.

## Output

A userdb database with constrained schema, migrations, and docs.

## Pitfalls

- Constraints enforce; comments merely suggest.
- Design from the queries needed, not from abstract purity.
