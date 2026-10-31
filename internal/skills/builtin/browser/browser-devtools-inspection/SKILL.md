---
name: browser-devtools-inspection
description: Inspect elements, console, network requests and performance of a page with developer tools.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_devtools
    - browser_snapshot
    display:
      name_en: Browser devtools inspection
      name_es: Inspección con DevTools
      desc_en: Inspect elements, console, network requests and performance of a page with developer tools.
      desc_es: Inspeccionar elementos, consola, red y rendimiento.
---
# Browser devtools inspection

Use this skill to diagnose web behavior with evidence. Console errors, network traces, and performance profiles name the culprit instead of guessing from symptoms.

## When to use

- A page misbehaves and the cause is unknown.
- Performance or loading problems need numbers.
- Verifying what a page actually sends and receives.

## Procedure

1. Snapshot the page, then read console errors first — they name suspects.
2. Trace the network: failed requests, payloads, timing, and caching.
3. Profile the slow interaction: script, layout, paint, and network splits.
4. Inspect the exact element and its computed state for rendering bugs.
5. Report the chain from evidence to cause with the captured traces.

## Output

A diagnosis with console, network, and profile evidence.

## Pitfalls

- Symptoms mislead; traces decide.
- Test the failing configuration, not an easier one.
