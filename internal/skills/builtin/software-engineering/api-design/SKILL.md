---
name: api-design
description: Design APIs and module boundaries with clear contracts, error handling and input validation.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    display:
      name_en: API design
      name_es: Diseño de APIs
      desc_en: Design APIs and module boundaries with clear contracts, error handling and input validation.
      desc_es: Diseñar APIs con contratos claros y validación.
---
# API design

Use this skill when defining or changing an interface between modules, services, or clients. A contract written first — resources, errors, validation — prevents integration bugs that no test plan catches later.

## When to use

- Creating or versioning an API or module boundary.
- Clients misuse an interface repeatedly.
- Error handling differs per endpoint.

## Procedure

1. Define resources and operations with names from the domain, not from the database.
2. Fix the error contract: codes, shapes, and which failures are retryable.
3. Validate every input at the boundary; reject unknown fields explicitly.
4. Version deliberately: additive changes first, breaking changes with migration.
5. Document with examples that actually run against the implementation.

## Output

A contract-first API: resources, errors, validation, versioning, and runnable examples.

## Pitfalls

- No boundary without an error contract.
- Examples must run; dead examples are worse than none.
