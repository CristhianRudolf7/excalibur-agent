---
name: dependency-management
description: 'Update dependencies safely: read the changes, adjust the code and verify with the tests.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - source_search
    display:
      name_en: Dependency management
      name_es: Gestión de dependencias
      desc_en: 'Update dependencies safely: read the changes, adjust the code and verify with the tests.'
      desc_es: Actualizar dependencias con seguridad y tests.
---
# Dependency management

Use this skill for upgrades. Reading the changelog, adjusting to breaking changes deliberately, and verifying with the suite keeps updates boring — which is the goal.

## When to use

- Upgrading a dependency, minor or major.
- Security advisories affect the tree.
- The lockfile drifts from manifests.

## Procedure

1. Read the changelog for breaking changes and new requirements.
2. Upgrade in dependency order, one major at a time; keep the lockfile consistent.
3. Adjust the code to the new APIs; remove shims for removed behavior.
4. Run the full suite plus the dependency's own smoke checks.
5. Pin versions and record the reasons for held-back upgrades.

## Output

Updated dependencies with changelog review, adjustments, and green verification.

## Pitfalls

- Never upgrade blindly to 'latest' before reading the changes.
- One major at a time; combined jumps hide the culprit.
