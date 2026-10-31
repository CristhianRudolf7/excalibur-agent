---
name: incremental-changes
description: Implement in small verifiable reversible steps, with verification and commit in each step.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - file_history
    display:
      name_en: Incremental changes
      name_es: Cambios incrementales
      desc_en: Implement in small verifiable reversible steps, with verification and commit in each step.
      desc_es: Implementar en pasos pequeños, verificables y reversibles.
---
# Incremental changes

Use this skill for any non-trivial change. Small steps with a check and a commit each keep every intermediate state working and every mistake reversible in seconds.

## When to use

- Implementing a feature or fix longer than one edit.
- Touching critical or poorly covered code.
- A big-bang change failed before.

## Procedure

1. Slice the work so each step leaves the tree building and tests green.
2. Verify each step with its check before moving on; commit when green.
3. Keep steps reversible: snapshots and history make undo cheap.
4. Stop and reassess at the first red step instead of piling on.
5. Record each step's check as task evidence.

## Output

A chain of small verified commits, each green, each reversible.

## Pitfalls

- No step without its check; 'it should work' is not verification.
- Red means stop and fix, never continue on top.
