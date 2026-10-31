---
name: software-license-compliance
description: Review code and dependency licenses, compatibility between licenses and attribution obligations.
metadata:
  excalibur:
    group: compliance
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - grep
    - exec
    display:
      name_en: Software license compliance
      name_es: Licencias de software
      desc_en: Review code and dependency licenses, compatibility between licenses and attribution obligations.
      desc_es: Revisar licencias, compatibilidad y atribución.
---
# Software license compliance

Use this skill to keep the codebase shippable. License inventory, compatibility checks, and attribution fulfilled — before distribution forces the issue.

## When to use

- Adding dependencies with unfamiliar licenses.
- Preparing a release or distribution.
- Copyleft or commercial-use questions arise.

## Procedure

1. Inventory all licenses: code, dependencies, assets, generated parts.
2. Check compatibility for the distribution model: internal, SaaS, or shipped.
3. Fulfill attribution and notice obligations exactly as licenses demand.
4. Flag copyleft or commercial restrictions for an explicit decision.
5. Record the license posture with the release.

## Output

A license review with compatibility verdict and attributions done.

## Pitfalls

- Assumed-compatible licenses bite at distribution; check early.
- Attribution skipped is infringement; fulfill it.
