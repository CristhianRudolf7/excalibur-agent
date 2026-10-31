---
name: code-simplification
description: Simplify code without changing its behavior, with the tests as the safety net.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - lsp
    - exec
    display:
      name_en: Code simplification
      name_es: Simplificación de código
      desc_en: Simplify code without changing its behavior, with the tests as the safety net.
      desc_es: Simplificar sin cambiar el comportamiento, con tests como red.
---
# Code simplification

Use this skill to reduce complexity that slows every future change. Each simplification keeps behavior identical, proven by the suite staying green throughout.

## When to use

- Code is hard to follow but covered by tests.
- Duplication or dead paths accumulate.
- Before extending a tangled module.

## Procedure

1. Confirm the covering tests pass before touching anything.
2. Simplify one thing at a time: remove dead code, collapse duplication, flatten nesting.
3. Re-run the suite after each move; red means revert that move.
4. Keep the diff reviewable: behavior-preserving changes only, no features mixed in.
5. Stop when the next simplification risks behavior or exceeds its value.

## Output

Simpler code with identical behavior and a green suite throughout.

## Pitfalls

- No behavior change may hide inside a simplification.
- Without covering tests, write them first.
