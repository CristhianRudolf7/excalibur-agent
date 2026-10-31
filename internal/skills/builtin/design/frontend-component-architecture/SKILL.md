---
name: frontend-component-architecture
description: Design the component breakdown and state management of an interface.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    display:
      name_en: Frontend component architecture
      name_es: Arquitectura de componentes
      desc_en: Design the component breakdown and state management of an interface.
      desc_es: Diseñar la descomposición en componentes y el estado.
---
# Frontend component architecture

Use this skill before building a complex UI. A deliberate component tree with explicit state ownership prevents prop-drilling spaghetti and duplicated state bugs.

## When to use

- Starting a sizable frontend feature.
- State bugs or prop chains multiply.
- Refactoring a tangled component tree.

## Procedure

1. Decompose the UI into components with single responsibilities and clear props.
2. Assign each piece of state one owner; derive the rest, never duplicate it.
3. Isolate side effects at boundaries with explicit loading and error states.
4. Check the tree against the code graph for reusable siblings before inventing new ones.
5. Document the state map so future changes know where each fact lives.

## Output

A component tree with owned state, boundaries, and a state map.

## Pitfalls

- Duplicated state desynchronizes; derive, don't copy.
- Side effects scattered through leaves become untestable.
