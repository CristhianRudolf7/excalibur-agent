---
name: test-driven-development
description: Write the tests first, check they fail for the pending behavior, then implement what is needed
  to pass.
metadata:
  excalibur:
    group: testing
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - lsp
    display:
      name_en: Test-driven development
      name_es: Desarrollo guiado por pruebas
      desc_en: Write the tests first, check they fail for the pending behavior, then implement what is
        needed to pass.
      desc_es: Escribir primero los tests, verlos fallar e implementar.
---
# Test-driven development

Use this skill when implementing behavior with clear expectations. Failing-first tests define done before code exists, prevent over-implementation, and leave a regression net behind.

## When to use

- Implementing a requirement with observable behavior.
- Fixing a bug that needs a regression test.
- Refactoring code that lacks a safety net.

## Procedure

1. Write the smallest test expressing the pending behavior; run it and watch it fail for the right reason.
2. Implement the minimum that makes it pass; no extra behavior, no scaffolding.
3. Run the test plus its neighbors; keep everything green.
4. Refactor under the net, re-running after each move.
5. Register the tests as checks of the task criteria.

## Output

Failing-first tests, a minimal implementation, and a green suite.

## Pitfalls

- A test that passes before the fix proves nothing; always see it fail.
- Do not write the implementation and the test in the same step.
