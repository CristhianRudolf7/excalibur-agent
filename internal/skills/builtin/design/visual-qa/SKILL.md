---
name: visual-qa
description: Visually review the interface in the browser and at different screen sizes, identify problems
  and fix them.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_snapshot
    - browser_emulate
    - device_screen_capture
    display:
      name_en: Visual QA
      name_es: Control visual de calidad
      desc_en: Visually review the interface in the browser and at different screen sizes, identify problems
        and fix them.
      desc_es: Revisar visualmente la interfaz en varios tamaños.
---
# Visual QA

Use this skill as the visual counterpart of tests: look at the real interface on real sizes and fix what eyes catch. Layout bugs hide from every automated check except this one.

## When to use

- Before shipping any visual change.
- After responsive or cross-browser work.
- Users report 'it looks broken' without details.

## Procedure

1. Open the real interface in the browser, not a mockup or screenshot.
2. Walk every changed screen at small and large sizes; capture each.
3. Log each defect with a capture, the viewport, and the expected rendering.
4. Fix, re-capture, and compare; keep the captures as evidence.
5. Check reduced-motion and high-contrast modes where the product claims them.

## Output

A visual pass with captures, defects fixed, and before/after evidence.

## Pitfalls

- Mockup approval is not visual QA; test the real build.
- One viewport is not coverage; check small and large.
