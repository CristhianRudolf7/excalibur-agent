---
name: data-protection-compliance
description: Review personal data processing against data protection rules (e.g. GDPR).
metadata:
  excalibur:
    group: compliance
    version: 1.0.0
    platforms:
    - linux
    tools:
    - grep
    - document_read
    - task_db
    display:
      name_en: Data protection compliance
      name_es: Protección de datos
      desc_en: Review personal data processing against data protection rules (e.g. GDPR).
      desc_es: Revisar el tratamiento de datos personales (p. ej. RGPD).
---
# Data protection compliance

Use this skill to check data handling against protection rules. Lawful basis, minimization, rights, and retention reviewed concretely — guidance, not legal advice.

## When to use

- Processing personal data in a feature or migration.
- A DPA, DPIA, or rights request appears.
- Retention or cross-border transfers change.

## Procedure

1. Inventory the personal data: what, whose, where it flows and rests.
2. Check lawful basis and purpose limitation per processing activity.
3. Verify rights handling: access, erasure, portability within deadlines.
4. Review retention and deletion against the schedule; check transfers and safeguards.
5. Record gaps with owners; escalate what needs counsel.

## Output

A protection review with gaps, owners, and escalation notes.

## Pitfalls

- This is guidance, not legal advice; escalate doubts to counsel.
- Undocumented processing is non-compliant processing.
