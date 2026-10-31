---
name: progress-updates
description: 'Report work status in plain language: what was done, what is missing, what is needed from
  the user, and when to report again.'
metadata:
  excalibur:
    group: communication
    version: 1.0.0
    platforms:
    - linux
    tools:
    - status
    display:
      name_en: Progress updates
      name_es: Informes de avance
      desc_en: 'Report work status in plain language: what was done, what is missing, what is needed from
        the user, and when to report again.'
      desc_es: Contar el estado de los trabajos en lenguaje sencillo.
---
# Progress updates

Use this skill whenever the user asks 'how is it going' or a long task needs a checkpoint. Plain language, no jargon: done, missing, blocked, next report time.

## When to use

- The user asks for status on background work.
- A task is blocked waiting for the user or a third party.
- A scheduled checkpoint arrives.

## Procedure

1. Read the live snapshot: states, current step, last tool line via status.
2. Say what was done since the last update, in one or two lines.
3. Say what is missing and what, if anything, you need from the user.
4. Give the next report time and keep it.

## Output

A short plain-language update: done, missing, needs, next report.

## Pitfalls

- No jargon or internal IDs unless the user asked for detail.
- Never promise a time you do not control; give the next checkpoint instead.
