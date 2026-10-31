---
name: frontend-best-practices
description: 'Framework best practices for frontends: conventions, load and render performance, bundling
  and compatibility.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_snapshot
    - browser_devtools
    display:
      name_en: Frontend best practices
      name_es: Buenas prácticas de frontend
      desc_en: 'Framework best practices for frontends: conventions, load and render performance, bundling
        and compatibility.'
      desc_es: Buenas prácticas de frameworks de frontend.
---
# Frontend best practices

Use this skill for frontend work. Conventions plus measured load and render performance keep interfaces fast on real devices instead of fast on the developer's machine.

## When to use

- Building or fixing UI code.
- Load or interaction feels slow.
- Bundle size or compatibility complaints appear.

## Procedure

1. Follow the framework's component and state conventions; no parallel patterns.
2. Measure before optimizing: snapshot the page and read network and render timings.
3. Fix the bottleneck the numbers show: payload, render passes, or blocking scripts.
4. Check the result on a small and a large viewport plus one constrained device profile.
5. Record before/after timings as evidence.

## Output

Convention-following UI with measured performance evidence.

## Pitfalls

- No optimization without a measurement first.
- Developer-machine timings are not evidence.
