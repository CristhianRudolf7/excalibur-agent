---
name: security-reporting
description: Write reports distinguishing confirmed, pending-validation and discarded findings, with severity
  and recommendation.
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - document_create
    display:
      name_en: Security reporting
      name_es: Informes de seguridad
      desc_en: Write reports distinguishing confirmed, pending-validation and discarded findings, with
        severity and recommendation.
      desc_es: Redactar informes con hallazgos confirmados y pendientes.
---
# Security reporting

Use this skill to deliver security results people can act on. Three buckets — confirmed, pending, discarded — with severity and a concrete fix each, turn an audit into a work plan.

## When to use

- Closing a security review or hunt.
- Findings must reach developers or leadership.
- Tracking remediation across findings.

## Procedure

1. Sort findings into confirmed, pending validation, and discarded, with evidence links.
2. Rate severity by impact times exploitability in this deployment, not in theory.
3. Write one concrete recommendation per finding: fix, workaround, or accepted risk with owner.
4. Order the report by severity; lead with what burns now.
5. Track remediation to closure; re-verify fixes before marking them done.

## Output

A report with bucketed findings, severities, recommendations, and remediation tracking.

## Pitfalls

- Severity without deployment context misleads; rate what is deployed.
- Discarded findings stay listed with reasons.
