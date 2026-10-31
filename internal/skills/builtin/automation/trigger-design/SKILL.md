---
name: trigger-design
description: Define event triggers (email, messages, webhooks, files, calendar) with filters and deduplication
  (21.3).
metadata:
  excalibur:
    group: automation
    version: 1.0.0
    platforms:
    - linux
    tools:
    - trigger_rules
    - schedule
    display:
      name_en: Trigger design
      name_es: Diseño de disparadores
      desc_en: Define event triggers (email, messages, webhooks, files, calendar) with filters and deduplication
        (21.3).
      desc_es: Definir disparadores por eventos con filtros.
---
# Trigger design

Use this skill to start work on events without drowning in them. Precise filters plus deduplication mean each trigger fires exactly when it should — once.

## When to use

- Work must start on email, messages, webhooks, files, or calendar events.
- Duplicate or spurious automation runs appear.
- Designing scheduled versus event-driven starts.

## Procedure

1. Define the event source and the exact conditions that qualify.
2. Add filters narrowing to actionable events; test them against history.
3. Deduplicate with idempotency keys so retries and replays fire once.
4. Set quiet hours, rate limits, and failure handling per trigger.
5. Log every firing with its cause for audit.

## Output

Triggers with filters, dedup, limits, and auditable firings.

## Pitfalls

- Unfiltered triggers spam; test filters on history first.
- No dedup means double effects on every retry.
