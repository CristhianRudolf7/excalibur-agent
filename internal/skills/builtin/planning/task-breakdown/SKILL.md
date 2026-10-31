---
name: task-breakdown
description: Split the specification into small verifiable tasks with acceptance criteria, dependencies,
  milestones and the minimum content of a micro plan.
metadata:
  excalibur:
    group: planning
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - subagent
    display:
      name_en: Task breakdown
      name_es: Descomposición en tareas
      desc_en: Split the specification into small verifiable tasks with acceptance criteria, dependencies,
        milestones and the minimum content of a micro plan.
      desc_es: Dividir la especificación en tareas pequeñas y verificables.
---
# Task breakdown

Use this skill to convert a spec into an executable task tree. Small tasks with acceptance criteria can be delegated, verified, and integrated independently; large ones cannot.

## When to use

- A spec exists and work must be scheduled or delegated.
- A task keeps growing or keeps failing its gate.
- Parallel work by subagents is planned.

## Procedure

1. Split each requirement into tasks of at most a few hours, each with acceptance criteria.
2. Order tasks by dependency: blocks versus informs; the plan must stay acyclic.
3. Define milestones: groups of tasks that unlock a demo or a decision.
4. Write each micro plan with its minimum content: goal, steps, checks, and done condition.
5. Assign coherent requirement groups to subagents per the delegation skill.

## Output

A task tree in task_db with criteria, dependencies, milestones, and micro plans ready to execute or delegate.

## Pitfalls

- No task without acceptance criteria; 'work on X' is not a task.
- Keep the dependency graph acyclic; validate before executing.
