---
name: migration-planning
description: Plan migrations and the retirement of obsolete features with reversible steps.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - worktree
    display:
      name_en: Migration planning
      name_es: Planificación de migraciones
      desc_en: Plan migrations and the retirement of obsolete features with reversible steps.
      desc_es: Planificar migraciones con pasos reversibles.
---
# Migration planning

Use this skill for migrations and removals. Reversible steps with a rollback defined up front turn a scary cutover into a routine sequence anyone can abort safely.

## When to use

- Migrating data, APIs, or infrastructure.
- Retiring a feature or an old path.
- Downtime or data loss is possible.

## Procedure

1. Inventory what moves: data, callers, configs, and who owns each.
2. Sequence reversible steps; each step defines its own rollback.
3. Keep old and new paths running in parallel where possible, with a switch.
4. Test the rollback, not just the roll-forward.
5. Retire the old path only after the new one proves itself in use.

## Output

A sequenced migration plan where every step is reversible and tested.

## Pitfalls

- A migration without a tested rollback is a gamble.
- Never retire the old path on launch day.
