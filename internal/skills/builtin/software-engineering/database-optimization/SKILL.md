---
name: database-optimization
description: 'Optimize databases: indexes, queries, schemas and execution plans.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - userdb
    - exec
    display:
      name_en: Database optimization
      name_es: Optimización de bases de datos
      desc_en: 'Optimize databases: indexes, queries, schemas and execution plans.'
      desc_es: Optimizar índices, consultas, esquemas y planes.
---
# Database optimization

Use this skill when the database is the bottleneck. Execution plans show what the engine really does; indexes, query shape, and schema follow the plan, not folklore.

## When to use

- Queries are slow or time out.
- Adding indexes 'to be safe' is proposed.
- Schema changes are on the table.

## Procedure

1. Capture the slow query with its actual execution plan and row counts.
2. Fix the query shape first: filters, joins, pagination, N+1 patterns.
3. Add or remove indexes to match the plan; verify the plan changed.
4. Consider schema or partitioning changes only when queries and indexes are exhausted.
5. Load-test the result with production-like data volume.

## Output

Faster queries with plans, index changes, and load-test evidence.

## Pitfalls

- Index folklore without a plan is guesswork.
- Test with realistic volume; tiny fixtures hide the problem.
