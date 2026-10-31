---
name: dependency-security-review
description: Review dependencies and supply chain with known-vulnerability sources.
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - exec
    display:
      name_en: Dependency security review
      name_es: Revisión de dependencias
      desc_en: Review dependencies and supply chain with known-vulnerability sources.
      desc_es: Revisar dependencias y cadena de suministro.
---
# Dependency security review

Use this skill to keep the supply chain boring. Known-vulnerability lookup plus provenance checks on what you install stops most supply-chain incidents before they start.

## When to use

- Adding or upgrading a dependency.
- Advisories mention the stack in use.
- Auditing a project's full tree.

## Procedure

1. Inventory direct and transitive dependencies with pinned versions.
2. Check each against known-vulnerability sources; record the source and date.
3. Triage: patch, upgrade, replace, or accept with owner and expiry.
4. Verify provenance for critical packages: source, signatures, maintainers.
5. Automate the check so the next advisory pages you, not surprises you.

## Output

A dependency verdict with advisories, triage decisions, and monitoring.

## Pitfalls

- Transitive dependencies count; audit the tree, not the manifest.
- Accepted risk needs an owner and an expiry, or it is forever.
