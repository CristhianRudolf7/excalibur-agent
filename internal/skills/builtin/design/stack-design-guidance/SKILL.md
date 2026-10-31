---
name: stack-design-guidance
description: Design recommendations for the technology in use (component system, framework, platform).
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - web_search
    - source_search
    display:
      name_en: Stack design guidance
      name_es: Guía de diseño por tecnología
      desc_en: Design recommendations for the technology in use (component system, framework, platform).
      desc_es: Recomendaciones de diseño según la tecnología usada.
---
# Stack design guidance

Use this skill to fit design to the stack instead of fighting it. Component systems, platform conventions, and framework limits shape what is cheap, fast, and idiomatic.

## When to use

- Starting UI work on a specific stack or platform.
- Designs keep clashing with framework constraints.
- Choosing between native and cross-platform patterns.

## Procedure

1. Identify the stack's component system and platform conventions.
2. Prefer native patterns and shipped components over custom rebuilds.
3. Check the framework's performance and compatibility limits before committing to effects.
4. Document the stack-specific adaptations of the design direction.
5. Verify the result in the real target, not just in mockups.

## Output

Stack-fitted design guidance with documented adaptations.

## Pitfalls

- Custom rebuilds of shipped components inherit their bugs.
- Mockup-only designs surprise on real devices.
