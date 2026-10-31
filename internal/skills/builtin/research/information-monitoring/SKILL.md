---
name: information-monitoring
description: Set up tracking of topics, sources and regions following the user's preferences and learn
  from their corrections ('I do not care about this', 'search this source better').
metadata:
  excalibur:
    group: research
    version: 1.0.0
    platforms:
    - linux
    tools:
    - monitor_ops
    - trigger_rules
    display:
      name_en: Information monitoring
      name_es: Seguimiento de información
      desc_en: Set up tracking of topics, sources and regions following the user's preferences and learn
        from their corrections ('I do not care about this', 'search this source better').
      desc_es: Monitorizar temas aprendiendo de las correcciones.
---
# Information monitoring

Use this skill for ongoing awareness. Monitored topics with learned preferences deliver signal; every correction tunes what arrives next instead of being argued with.

## When to use

- The user wants to follow a topic, source, or region.
- Briefings include noise the user keeps skipping.
- Coverage gaps appear ('why didn't I hear about X').

## Procedure

1. Configure sources, regions, and topics from the user's stated interests.
2. Deliver digests with why-each-item-matters in one line.
3. Treat every correction as training: adjust filters, sources, and ranking.
4. Review misses explicitly: what should have matched but did not.
5. Keep the monitor's own configuration visible for audit.

## Output

A tuned monitor with digests and a visible, correctable configuration.

## Pitfalls

- Corrections are training data; apply them, don't debate them.
- Silent coverage gaps are the failure mode; review misses.
