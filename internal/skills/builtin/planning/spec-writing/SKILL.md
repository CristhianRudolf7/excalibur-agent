---
name: spec-writing
description: Write the specification — goals, R# requirements, structure, tests and limits, plus screens,
  components, interactions and states for UIs — as task_db operations.
metadata:
  excalibur:
    group: planning
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    display:
      name_en: Spec writing
      name_es: Redacción de especificaciones
      desc_en: Write the specification — goals, R# requirements, structure, tests and limits, plus screens,
        components, interactions and states for UIs — as task_db operations.
      desc_es: Redactar la especificación como operaciones de task_db.
---
# Spec writing

Use this skill to turn an agreed proposal into an executable specification stored in the database, not in prose. Every requirement gets an ID, every behavior a test, every screen a state list.

## When to use

- A proposal was accepted and work is about to be planned.
- The task spans more than a few steps or more than one agent.
- An interface is involved and its states must be pinned down.

## Procedure

1. Write the goals in one paragraph: what changes for whom when this is done.
2. Record each requirement as R# via task_db: functional, non-functional, or constraint, with priority must/should/could.
3. Describe the structure: modules or screens, components, interactions, and every state each can be in.
4. Attach tests and limits to each requirement: how it will be checked and what is out of scope.
5. Validate the spec graph: no dangling references, no requirement without a check.

## Output

A complete spec in task_db: goals, R# requirements with priorities, structure, tests, limits, and UI states where applicable.

## Pitfalls

- Never keep requirements only in chat; if it is not in task_db it does not exist.
- Never write a requirement without a verification method.
