---
name: design-audit
description: Evaluate visual hierarchy, accessibility, device adaptation and performance of an existing
  interface.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_snapshot
    - browser_devtools
    display:
      name_en: Design audit
      name_es: Auditoría de diseño
      desc_en: Evaluate visual hierarchy, accessibility, device adaptation and performance of an existing
        interface.
      desc_es: Evaluar jerarquía, accesibilidad y adaptación de una interfaz.
---
# Design audit

Use this skill to assess an interface you did not design. A structured audit across hierarchy, accessibility, adaptation, and performance produces a prioritized fix list instead of opinions.

## When to use

- Taking over or reviewing an existing product.
- Planning a redesign with limited budget.
- Quality complaints lack specifics.

## Procedure

1. Walk the key flows and score hierarchy: does the eye land where the job is.
2. Audit accessibility against WCAG: keyboard, focus, contrast, labels.
3. Check adaptation: small screens, large screens, slow networks.
4. Measure performance: load, interaction, and payload per key screen.
5. Rank findings by user impact times fix cost; report with captures.

## Output

A prioritized audit with evidence and a fix order.

## Pitfalls

- Audit the built product, not its screenshots.
- Rank by impact times cost, not by personal taste.
