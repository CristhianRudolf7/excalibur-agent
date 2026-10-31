---
name: environment-setup
description: 'Prepare the environment: detect interpreters and executables, install dependencies and check
  PATH before invoking tools.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Environment setup
      name_es: Preparación del entorno
      desc_en: 'Prepare the environment: detect interpreters and executables, install dependencies and
        check PATH before invoking tools.'
      desc_es: Detectar intérpretes, instalar dependencias y comprobar PATH.
---
# Environment setup

Use this skill before running anything in a fresh environment. Confirming interpreters, dependencies, and PATH up front turns mysterious failures into a two-minute checklist.

## When to use

- Working in a fresh container, machine, or worktree.
- Tools fail with 'not found' or version errors.
- Dependencies may be missing or stale.

## Procedure

1. Detect the OS, architecture, and available interpreters and package managers.
2. Install project dependencies with the project's own manifests; record versions.
3. Verify PATH resolution for every binary the task needs.
4. Run the cheapest project check to prove the environment works.
5. Record the working setup in the project profile so the next run skips this.

## Output

A verified environment: detected tools, installed dependencies, PATH confirmed, proof check green.

## Pitfalls

- Never install globally what belongs to the project.
- Do not proceed on a red proof check; fix the environment first.
