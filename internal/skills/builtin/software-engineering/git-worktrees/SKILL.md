---
name: git-worktrees
description: Work with branches and worktrees to develop in parallel without interference, and clean them
  up when done.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - worktree
    - exec
    display:
      name_en: Git worktrees
      name_es: Ramas y worktrees
      desc_en: Work with branches and worktrees to develop in parallel without interference, and clean
        them up when done.
      desc_es: Trabajar con ramas y worktrees en paralelo y limpiarlas.
---
# Git worktrees

Use this skill whenever parallel or delegated work shares one repository. One worktree per task branch keeps trees, builds, and test runs isolated from each other and from the user.

## When to use

- Delegating work that touches the same repository.
- Running parallel experiments on one codebase.
- Any task work in a git project.

## Procedure

1. Create one worktree per task on its own branch, registered with the harness.
2. Run builds and tests inside the worktree, never in the user's checkout.
3. Keep the branch focused: one task, one branch, coherent commits.
4. Integrate through the queue; the delegator decides the merge.
5. Delete the worktree and its branch right after integration.

## Output

Isolated worktrees per task, integrated through the queue, cleaned up afterwards.

## Pitfalls

- Never work in the user's active checkout for delegated tasks.
- Stale worktrees are litter; remove them the same day.
