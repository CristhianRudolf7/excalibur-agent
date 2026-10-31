---
name: package-registry-search
description: Consult package registries (crates.io, npm, PyPI) for versions, licenses and maintenance.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - web_read
    display:
      name_en: Package registry search
      name_es: Búsqueda en registros de paquetes
      desc_en: Consult package registries (crates.io, npm, PyPI) for versions, licenses and maintenance.
      desc_es: Consultar registros para versiones y licencias.
---
# Package registry search

Use this skill before adding a dependency. Registry metadata — versions, licenses, maintenance signals — answers whether a package is safe to marry.

## When to use

- Evaluating a new dependency.
- Checking version history or license.
- Assessing maintenance health.

## Procedure

1. Look up the package in its registry: versions, dates, license declared.
2. Check maintenance: release cadence, open issues, maintainer responsiveness.
3. Review dependents and downloads as adoption signals, not proofs.
4. Confirm the license text, not just the registry label.
5. Record version, license, and health with the adoption decision.

## Output

A dependency verdict with version, license, and maintenance evidence.

## Pitfalls

- Registry labels err; read the license file.
- Abandoned packages with known CVEs are rejections, not risks.
