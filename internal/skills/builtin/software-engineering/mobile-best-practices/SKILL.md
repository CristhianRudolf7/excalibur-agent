---
name: mobile-best-practices
description: 'Mobile development best practices: lifecycle, permissions, local storage, performance and
  store publishing.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools: []
    display:
      name_en: Mobile best practices
      name_es: Buenas prácticas móviles
      desc_en: 'Mobile development best practices: lifecycle, permissions, local storage, performance
        and store publishing.'
      desc_es: Buenas prácticas de desarrollo móvil.
---
# Mobile best practices

Use this skill for mobile work. Lifecycle discipline, minimal permissions, offline-tolerant storage, and store rules decide whether an app ships and stays installed.

## When to use

- Building or fixing a mobile app.
- Handling permissions, background work, or local data.
- Preparing a store submission.

## Procedure

1. Respect the platform lifecycle: save and restore state across kills and rotations.
2. Request the minimum permissions, with an explanation at the point of use.
3. Make storage offline-tolerant: queue, sync, and resolve conflicts explicitly.
4. Profile on a low-end device: startup, memory, and battery come first.
5. Check store rules (privacy, content, metadata) before every submission.

## Output

Lifecycle-correct, permission-minimal mobile code ready for store review.

## Pitfalls

- No permission without an in-context explanation.
- Never assume connectivity; design offline-first.
