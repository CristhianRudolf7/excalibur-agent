---
name: create-skill
description: Create or improve skills following the Agent Skills spec, with their evaluation cases.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - skill_manage
    - package_publish
    display:
      name_en: Create skill
      name_es: Crear skills
      desc_en: Create or improve skills following the Agent Skills spec, with their evaluation cases.
      desc_es: Crear o mejorar skills con casos de evaluación.
---
# Create skill

Use this skill to turn proven know-how into a reusable skill. Spec-valid format plus evaluation cases that prove the skill helps — because unfiltered self-made skills hurt on average.

## When to use

- A procedure works and should be reused by agents.
- Improving an existing skill that underperforms.
- Publishing a skill to the store.

## Procedure

1. Write SKILL.md to the spec: matching name, 1-1024 description, group, version, tools.
2. Keep the body focused: one capability, concrete steps, real pitfalls.
3. Write evaluation cases: tasks where the skill should change the outcome.
4. Validate format and run the cases; revise until the skill demonstrably helps.
5. Publish only at the user's explicit request, with version and pin.

## Output

A spec-valid skill with evaluation cases and measured benefit.

## Pitfalls

- Unexpanded mega-skills backfire; one focused capability per skill.
- Never publish without evaluation; generation without filtering degrades results.
