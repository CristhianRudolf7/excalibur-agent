---
name: release-management
description: Prepare releases with checks, gradual rollout, tracking and rollback.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - github
    display:
      name_en: Release management
      name_es: Gestión de lanzamientos
      desc_en: Prepare releases with checks, gradual rollout, tracking and rollback.
      desc_es: Preparar lanzamientos con despliegue gradual y reversión.
---
# Release management

Use this skill to ship without drama. A release is a checklist — checks, rollout, monitoring, rollback — executed the same way whether it is a patch or a major.

## When to use

- Cutting any release to users.
- Rolling out a risky change.
- A previous release had to be yanked.

## Procedure

1. Freeze scope and run the full check chain on the exact release artifact.
2. Roll out gradually with health and business metrics watched at each stage.
3. Track adoption and errors per stage; halt on regression, not on optimism.
4. Keep the rollback one command away and tested before the rollout.
5. Write release notes from merged changes and close the loop with users.

## Output

A released version with staged rollout evidence, monitoring, and notes.

## Pitfalls

- Releasing the untested artifact is not a release; test the exact bits.
- No rollback plan means no rollout.
