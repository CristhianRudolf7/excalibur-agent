---
name: test-planning
description: Derive from the acceptance criteria the tests and checks (unit, integration, end-to-end)
  and register them as checks of the task.
metadata:
  excalibur:
    group: testing
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - exec
    display:
      name_en: Test planning
      name_es: Planificación de pruebas
      desc_en: Derive from the acceptance criteria the tests and checks (unit, integration, end-to-end)
        and register them as checks of the task.
      desc_es: Derivar los tests de los criterios y registrarlos como checks.
---
# Test planning

Use this skill to translate acceptance criteria into a check plan before running anything. Each criterion gets at least one executable check at the right level; nothing relies on eyeballing.

## When to use

- Criteria exist and the check set is unclear.
- Deciding what to test at unit, integration, or end-to-end level.
- Reviewing whether a task is actually verifiable.

## Procedure

1. For each acceptance criterion, name the observable behavior to check.
2. Assign a level per check: unit for logic, integration for seams, end-to-end for user-visible flows.
3. Prefer the cheapest level that truly exercises the criterion.
4. Register every check in task_db linked to its criterion.
5. Flag criteria with no executable check: they must be rewritten or explicitly accepted.

## Output

A check plan in task_db: every criterion covered by at least one executable check.

## Pitfalls

- Manual 'verify by looking' is not a check; make it executable or flag it.
- One criterion, at least one check: gaps are findings.
