---
name: state-diagrams
description: Lifecycle and state machine diagrams.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    display:
      name_en: State diagrams
      name_es: Diagramas de estados
      desc_en: Lifecycle and state machine diagrams.
      desc_es: Diagramas de ciclo de vida y máquinas de estados.
---
# State diagrams

Use this skill for anything with a lifecycle. Enumerated states with labeled transitions and guards make impossible states visible — and therefore preventable.

## When to use

- Designing lifecycles: tasks, orders, runs, subscriptions.
- Invalid-state bugs recur.
- A state machine needs documenting.

## Procedure

1. Enumerate all states including failure and terminal ones.
2. Label every transition with event and guard; no unlabeled arrows.
3. Mark the initial state and every terminal state explicitly.
4. Check reachability: every state reachable, every state escapable or terminal by design.
5. Verify the diagram against the implementation's actual transitions.

## Output

A complete state machine with guarded transitions, verified against code.

## Pitfalls

- Unreachable states are dead logic; reachable-by-accident states are bugs.
- Guards unwritten are guards unchecked.
