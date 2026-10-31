---
name: workflow-authoring
description: Design durable workflows with nodes, conditions, waits, approvals and compensations, and
  test them dry without effects (21.4).
metadata:
  excalibur:
    group: automation
    version: 1.0.0
    platforms:
    - linux
    tools:
    - workflow
    - task_db
    display:
      name_en: Workflow authoring
      name_es: Diseño de workflows
      desc_en: Design durable workflows with nodes, conditions, waits, approvals and compensations, and
        test them dry without effects (21.4).
      desc_es: Diseñar workflows durables y probarlos sin efectos.
---
# Workflow authoring

Use this skill for processes that must survive interruptions. Explicit nodes, waits, approvals, and compensations — tested dry first — make workflows durable instead of hopefully-sequential scripts.

## When to use

- A multi-step process must resume after crashes.
- Approvals or waits gate the process.
- Failures need defined compensations.

## Procedure

1. Model nodes, conditions, waits, and approvals with explicit state.
2. Define compensations for every effectful node before wiring the happy path.
3. Test dry without effects; enumerate paths up to the simulator limit.
4. Review the failure paths with the same care as the main flow.
5. Version the workflow; migrate instances deliberately.

## Output

A versioned durable workflow, dry-tested with compensations.

## Pitfalls

- Effects without compensations strand instances on failure.
- Dry-test before enabling; live debugging corrupts state.
