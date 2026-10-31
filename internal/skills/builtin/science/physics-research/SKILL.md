---
name: physics-research
description: Methods, units, derivations and numerical tools in physics.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - exec
    display:
      name_en: Physics research
      name_es: Investigación en física
      desc_en: Methods, units, derivations and numerical tools in physics.
      desc_es: Métodos, unidades y herramientas numéricas en física.
---
# Physics research

Use this skill for physics with units and derivations intact. Dimensional checks, documented derivations, and validated numerics catch most errors before they propagate.

## When to use

- Physics modeling or calculation is needed.
- Checking derivations or numerical results.
- Choosing numerical methods for a problem.

## Procedure

1. Fix units and conventions explicitly before any calculation.
2. Derive symbolically where possible; check dimensions at each step.
3. Validate numerics against analytic limits and convergence tests.
4. Compare with literature values or experiments when available.
5. Document derivations so errors are findable.

## Output

Physics results with units, derivations, and validated numerics.

## Pitfalls

- Unit errors dominate; check dimensions every step.
- Unconverged numerics are guesses with decimals.
