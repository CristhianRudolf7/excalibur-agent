---
name: interface-design
description: Design web interfaces, applications and dashboards for their needs.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - design_canvas_ops
    - browser_snapshot
    display:
      name_en: Interface design
      name_es: Diseño de interfaces
      desc_en: Design web interfaces, applications and dashboards for their needs.
      desc_es: Diseñar interfaces web, aplicaciones y paneles.
---
# Interface design

Use this skill to design screens that serve their job. Needs-first layout — hierarchy, states, feedback — beats decoration that users must work around.

## When to use

- Designing web, app, or dashboard screens.
- An existing interface confuses its users.
- A prototype must become a buildable design.

## Procedure

1. List the screen's jobs and rank them; the top job owns the hierarchy.
2. Lay out states explicitly: loading, empty, error, and success for each region.
3. Design feedback for every action: what the user sees while it runs and after.
4. Verify the design in the browser at small and large sizes.
5. Hand off with tokens, components, and behavior notes, not just pictures.

## Output

Needs-driven screens with states, feedback, and a buildable handoff.

## Pitfalls

- Every region needs its empty and error state.
- Decoration that hides function is a defect.
