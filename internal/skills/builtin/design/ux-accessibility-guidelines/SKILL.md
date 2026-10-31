---
name: ux-accessibility-guidelines
description: Apply user experience, accessibility (WCAG), layout, distribution and interaction guidelines.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_snapshot
    - browser_devtools
    display:
      name_en: UX accessibility guidelines
      name_es: Pautas de UX y accesibilidad
      desc_en: Apply user experience, accessibility (WCAG), layout, distribution and interaction guidelines.
      desc_es: Aplicar pautas de UX, accesibilidad y distribución.
---
# UX accessibility guidelines

Use this skill to make interfaces usable by everyone. WCAG-grounded layout, interaction, and feedback rules prevent the exclusion bugs that redesigns later.

## When to use

- Designing or reviewing any user-facing flow.
- Accessibility conformance is required.
- Users report confusion or cannot complete tasks.

## Procedure

1. Apply the WCAG floor: perceivable, operable, understandable, robust — with concrete checks per principle.
2. Design interactions with visible focus, keyboard paths, and reduced-motion support.
3. Keep layout predictable: hierarchy, spacing, and feedback for every action.
4. Verify in the browser: snapshot, keyboard-only pass, and a screen-reader skim of key flows.
5. Record residual gaps with severity instead of hiding them.

## Output

A UX review against WCAG with verified fixes and recorded gaps.

## Pitfalls

- Accessibility is acceptance criteria, not polish for later.
- Test with the keyboard; mouse-only testing hides half the barriers.
