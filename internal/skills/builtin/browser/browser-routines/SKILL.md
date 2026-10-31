---
name: browser-routines
description: Record, parametrize and replay click and form routines with per-step assertions.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_routine
    - browser_snapshot
    - browser_act
    display:
      name_en: Browser routines
      name_es: Rutinas de navegador
      desc_en: Record, parametrize and replay click and form routines with per-step assertions.
      desc_es: Grabar y reproducir rutinas con aserciones.
---
# Browser routines

Use this skill for browser work that repeats. A recorded, parametrized routine with per-step assertions replays reliably where a fresh improvisation flakes.

## When to use

- A browser flow runs more than twice.
- Handing a flow to another agent or a schedule.
- Flaky manual browser steps need stabilizing.

## Procedure

1. Record the flow once, slowly, capturing references and waits.
2. Parametrize inputs; assert the expected state after each step.
3. Replay on a copy first; fix references that shifted.
4. Version the routine and note the site version it was recorded against.
5. Schedule or hand off only after a clean replay.

## Output

A versioned routine with assertions and a verified replay.

## Pitfalls

- Unasserted routines fail silently halfway.
- Sites change; re-verify routines on schedule.
