---
name: create-tool
description: Create specialized tools as packages with manifest, implementation and tests, following 13.9.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - tool_package
    - exec
    display:
      name_en: Create tool
      name_es: Crear herramientas
      desc_en: Create specialized tools as packages with manifest, implementation and tests, following
        13.9.
      desc_es: Crear herramientas como paquetes con manifiesto y tests.
---
# Create tool

Use this skill to add a missing capability as a proper tool package. Manifest, implementation, and tests travel together, so the tool registers cleanly and stays verified.

## When to use

- No existing tool covers a needed operation.
- A script deserves a schema, validation, and reuse.
- Packaging work for the catalog or the store.

## Procedure

1. Define the contract: name, description written to be found, schema, effects, idempotency.
2. Write the manifest and the implementation with validated arguments and normalized results.
3. Add tests covering happy path, input errors, and cancellation.
4. Register through the package flow and verify discovery via search_tools.
5. Document leases and resource keys when the tool touches shared state.

## Output

A tool package with manifest, implementation, tests, and catalog registration.

## Pitfalls

- No tool without input validation and a normalized result.
- Effects need idempotency keys; pure reads need honest classification.
