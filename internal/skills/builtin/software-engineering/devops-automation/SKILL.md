---
name: devops-automation
description: 'Automate infrastructure and operations: infrastructure as code, containers and operation
  scripts.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: DevOps automation
      name_es: Automatización DevOps
      desc_en: 'Automate infrastructure and operations: infrastructure as code, containers and operation
        scripts.'
      desc_es: Automatizar infraestructura como código y operaciones.
---
# DevOps automation

Use this skill to make environments reproducible and operations repeatable. Declared infrastructure, built containers, and scripted runbooks replace tribal knowledge and snowflake machines.

## When to use

- Provisioning or changing infrastructure.
- Containerizing an application.
- An operation is done by hand more than twice.

## Procedure

1. Declare infrastructure as code; no manual console changes outside it.
2. Build minimal reproducible container images with pinned bases.
3. Script operations as idempotent runbooks with checks, not as wiki prose.
4. Review automation like code: tests for scripts, diffs for infra changes.
5. Verify by rebuilding from scratch, not by inspecting the live machine.

## Output

Reproducible infrastructure, containers, and runbooks verified by rebuild.

## Pitfalls

- Manual fixes to declared infrastructure drift; put them in code.
- Unpinned bases rebuild into surprises.
