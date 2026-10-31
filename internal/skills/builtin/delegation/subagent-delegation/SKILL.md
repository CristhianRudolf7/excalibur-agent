---
name: subagent-delegation
description: Group related requirements and assign each set to a subagent with a complete delegation card
  (15.4), decide when to parallelize (15.3) and organize work in branches or worktrees.
metadata:
  excalibur:
    group: delegation
    version: 1.0.0
    platforms:
    - linux
    tools:
    - subagent
    - worktree
    - task_db
    display:
      name_en: Subagent delegation
      name_es: Delegación a subagentes
      desc_en: Group related requirements and assign each set to a subagent with a complete delegation
        card (15.4), decide when to parallelize (15.3) and organize work in branches or worktrees.
      desc_es: Delegar conjuntos de requisitos a subagentes con ficha completa.
---
# Subagent delegation

Use this skill when work splits across agents. Good delegation is a complete card per subagent — goal, scope, checks, and limits — plus an explicit parallelization decision and isolated work areas.

## When to use

- A task breaks into independent requirement groups.
- Specialized profiles fit different parts of the work.
- Parallel execution could shorten the critical path.

## Procedure

1. Group related requirements so each subagent owns a coherent slice with its own checks.
2. Write a complete delegation card per subagent: goal, scope, inputs, checks, budget, and report format.
3. Decide parallel versus sequential from dependencies and shared resources, not from optimism.
4. Give each subagent an isolated area: branch or worktree in git, leases otherwise.
5. Track progress via the snapshot; integrate through the queue, never by direct pushes.

## Output

Delegation cards issued, areas isolated, and an integration order recorded.

## Pitfalls

- No delegation without acceptance checks on the card.
- Mandatory instructions travel as fixed card instructions, never as suggested skills.
