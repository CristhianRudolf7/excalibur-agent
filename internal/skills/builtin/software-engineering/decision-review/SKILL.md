---
name: decision-review
description: Critically review important decisions during development and contrast doubts with evidence.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    display:
      name_en: Decision review
      name_es: Revisión de decisiones
      desc_en: Critically review important decisions during development and contrast doubts with evidence.
      desc_es: Revisar decisiones importantes con evidencia.
---
# Decision review

Use this skill at decision points that are expensive to reverse. A structured challenge — options, evidence, reversibility — catches weak reasoning while it is still cheap to change course.

## When to use

- Choosing architecture, dependencies, or data models.
- Two reasonable options split the team or the evidence.
- A decision made quickly now feels fragile.

## Procedure

1. State the decision, its context, and what makes it hard to reverse.
2. List the live options with their concrete trade-offs, not slogans.
3. Confront each doubt with evidence: measurements, prototypes, cited docs.
4. Prefer the reversible option when evidence ties.
5. Record the decision with motive and discarded alternatives.

## Output

A reviewed decision with options, evidence, and recorded rationale.

## Pitfalls

- Decisions by loudest voice are not reviewed; demand evidence.
- Record the losers too, or they will be relitigated.
