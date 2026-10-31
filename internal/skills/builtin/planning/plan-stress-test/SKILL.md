---
name: plan-stress-test
description: 'Challenge a plan before executing it: fragile assumptions, hidden dependencies, edge cases
  and risks, with concrete proposed changes.'
metadata:
  excalibur:
    group: planning
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - flow_simulate
    display:
      name_en: Plan stress test
      name_es: Prueba de esfuerzo del plan
      desc_en: 'Challenge a plan before executing it: fragile assumptions, hidden dependencies, edge cases
        and risks, with concrete proposed changes.'
      desc_es: Cuestionar un plan antes de ejecutarlo.
---
# Plan stress test

Use this skill as a pre-mortem: attack the plan before reality does. The output is not criticism but concrete changes that make the plan survive contact with edge cases and failures.

## When to use

- A plan is finished and execution has not started.
- The plan touches irreversible actions, concurrency, or external systems.
- A previous similar plan failed in production.

## Procedure

1. List every assumption the plan silently makes and mark the fragile ones.
2. Hunt for hidden dependencies: ordering, shared files, external services, timing.
3. Enumerate edge cases: empty inputs, huge inputs, failures of each dependency.
4. Propose concrete changes: added checks, reordered steps, fallbacks, smaller batches.
5. Record the accepted changes back into the plan via task_db.

## Output

A list of findings with severity and the resulting plan edits; the plan either changes or the risks are explicitly accepted.

## Pitfalls

- Criticism without a proposed change is noise; every finding needs a fix.
- Do not stress-test forever; one pass with the top risks is enough.
