---
name: quality-criteria
description: Define the quality criteria and project constraints (performance, security, accessibility,
  style, compatibility) as verifiable criteria with their checking method.
metadata:
  excalibur:
    group: planning
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    display:
      name_en: Quality criteria
      name_es: Criterios de calidad
      desc_en: Define the quality criteria and project constraints (performance, security, accessibility,
        style, compatibility) as verifiable criteria with their checking method.
      desc_es: Definir los criterios de calidad como criterios verificables.
---
# Quality criteria

Use this skill to make 'good' measurable before work starts. Each quality expectation becomes a criterion with a concrete check: a budget, a test, an inspection rule, or a metric query.

## When to use

- Starting a project or a task with quality expectations.
- Performance, security, accessibility, or compatibility constraints apply.
- Review disagreements keep recurring about what 'done well' means.

## Procedure

1. List the applicable dimensions: performance, security, accessibility, style, compatibility.
2. For each, write a criterion with a number or a binary check (budget, threshold, rule).
3. Assign each criterion a checking method: command, test, metric, or inspection.
4. Record criteria via task_db linked to their requirements.
5. Confirm the criteria are realistic for the budget: cut or renegotiate the rest.

## Output

Verifiable quality criteria in task_db, each with a method that the completion gate can execute.

## Pitfalls

- Avoid vague criteria like 'fast' or 'clean'; attach a number or a rule.
- Do not add criteria nobody will check; each must have an owner and a method.
