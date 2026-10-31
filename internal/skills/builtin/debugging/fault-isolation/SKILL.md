---
name: fault-isolation
description: Locate and isolate the fault (commit bisection, case reduction), fix it, and add a regression
  test that stops it from returning.
metadata:
  excalibur:
    group: debugging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - revert_edit
    - workspace_snapshot
    display:
      name_en: Fault isolation
      name_es: Aislamiento de fallos
      desc_en: Locate and isolate the fault (commit bisection, case reduction), fix it, and add a regression
        test that stops it from returning.
      desc_es: Localizar y aislar el fallo, corregirlo y añadir test de regresión.
---
# Fault isolation

Use this skill to shrink a failure to its smallest container: the commit, the input, the component. Isolation turns a mystery into a one-line fix with a test that locks it out forever.

## When to use

- A regression appeared between known-good and known-bad states.
- A large input or repository hides a small trigger.
- The fix must be proven minimal.

## Procedure

1. Bisect commits between good and bad until one commit remains; verify by toggling it.
2. Reduce the triggering case: halve inputs while the failure persists.
3. Fix at the isolated point; keep the change minimal and reversible.
4. Add a regression test that fails without the fix and passes with it.
5. Snapshot before risky edits so any step can be undone.

## Output

The isolated fault, the minimal fix, and a regression test.

## Pitfalls

- A fix without a regression test is a wish.
- Isolate before fixing; a fix to the wrong layer returns.
