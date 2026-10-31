---
name: ci-cd-automation
description: Automate continuous integration and deployment with verifications and failure handling.
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
      name_en: CI/CD automation
      name_es: Automatización de CI/CD
      desc_en: Automate continuous integration and deployment with verifications and failure handling.
      desc_es: Automatizar integración y despliegue con verificaciones.
---
# CI/CD automation

Use this skill to make every change prove itself before it ships. Pipelines run the checks, guard the branches, and handle failures the same way every time — including rollbacks.

## When to use

- Setting up or fixing a pipeline.
- Deploys are manual or inconsistent.
- Failures reach users that checks should have caught.

## Procedure

1. Run the full verification chain on every change: build, tests, checks, gates.
2. Protect branches: no merge without green checks and required reviews.
3. Deploy progressively where possible, with health checks and automatic halt.
4. Define failure handling: notifications, rollback path, and who decides.
5. Keep pipelines fast by caching and parallelizing; slow pipelines get bypassed.

## Output

A pipeline that verifies, guards, deploys, and recovers — with timing to prove it is used.

## Pitfalls

- Manual deploys beside an automated pipeline defeat it; remove the bypass.
- A pipeline nobody watches is a rumor; alert on its failures.
