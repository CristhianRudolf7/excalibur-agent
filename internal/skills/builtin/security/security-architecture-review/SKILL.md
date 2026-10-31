---
name: security-architecture-review
description: Analyze the architecture, inputs and trust boundaries of a system.
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    display:
      name_en: Security architecture review
      name_es: Revisión de arquitectura de seguridad
      desc_en: Analyze the architecture, inputs and trust boundaries of a system.
      desc_es: Analizar arquitectura, entradas y límites de confianza.
---
# Security architecture review

Use this skill before auditing code. Mapping trust boundaries, entry points, and data flows shows where attacks enter and which components must not fail — the audit plan follows the map.

## When to use

- Starting a security review of a system.
- Designing authentication, tenancy, or data flows.
- Threat modeling a new feature.

## Procedure

1. Draw the trust boundaries: what runs where, who controls each side.
2. List every entry point and what untrusted input it accepts.
3. Trace sensitive data from entry to storage; mark where it crosses boundaries.
4. Name the crown jewels and the paths that reach them.
5. Turn the map into an audit plan ordered by exposure.

## Output

A boundary map with entry points, data paths, and an ordered audit plan.

## Pitfalls

- Code review without a boundary map audits at random.
- Trust assumptions must be written, not felt.
