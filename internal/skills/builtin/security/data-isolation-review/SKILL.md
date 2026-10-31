---
name: data-isolation-review
description: Review data isolation between users, tenants and scopes.
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    - grep
    display:
      name_en: Data isolation review
      name_es: Revisión de aislamiento de datos
      desc_en: Review data isolation between users, tenants and scopes.
      desc_es: Revisar el aislamiento entre usuarios e inquilinos.
---
# Data isolation review

Use this skill wherever one store serves many parties. Isolation must hold in queries, caches, logs, and exports — one missing filter leaks everything.

## When to use

- Building multi-user or multi-tenant features.
- Adding caches, search, exports, or analytics over user data.
- A scope or tenant boundary is crossed by new code.

## Procedure

1. List every data access path: queries, caches, indexes, logs, exports.
2. Verify the scope or tenant filter on each path, in SQL where it matters.
3. Test as a second tenant: attempt to read, list, and export another's data.
4. Check background jobs and aggregations for cross-tenant mixing.
5. Review error messages and logs for leaked identifiers or content.

## Output

An isolation verdict per path with cross-tenant test evidence.

## Pitfalls

- One unfiltered path voids all filtered ones; enumerate completely.
- Logs and errors are data paths too.
