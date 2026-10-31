---
name: task-skill-matching
description: Choose the pertinent skills and working rules for each subtask and suggest at most 2-3 in
  the delegation card.
metadata:
  excalibur:
    group: delegation
    version: 1.0.0
    platforms:
    - linux
    tools:
    - search_skills
    - load_skill
    - search_tools
    display:
      name_en: Task skill matching
      name_es: Asignación de skills a subtareas
      desc_en: Choose the pertinent skills and working rules for each subtask and suggest at most 2-3
        in the delegation card.
      desc_es: Elegir las skills pertinentes de cada subtarea, máximo 2-3.
---
# Task skill matching

Use this skill when writing delegation cards. Each subtask gets the 2-3 skills that actually help it — matched to its content, not to its title — so the subagent starts focused instead of drowning in catalog.

## When to use

- Writing a subagent delegation card.
- A subtask needs domain rules the base prompt lacks.
- Reviewing why a subagent picked the wrong approach.

## Procedure

1. Read the subtask's acceptance checks, not just its title.
2. Search the index for skills matching the checks; compare candidates by content.
3. Pick at most 2-3; prefer small focused skills over exhaustive ones.
4. Write them as suggestions on the card; fixed rules go as instructions.
5. After the run, record whether each skill helped, per model.

## Output

A card with at most 2-3 suggested skills plus the mandatory instructions separated.

## Pitfalls

- More than 3 suggestions degrades results; cut ruthlessly.
- Suggestions are optional for the subagent; instructions are not.
