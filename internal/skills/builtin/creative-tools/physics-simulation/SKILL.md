---
name: physics-simulation
description: Define, run and interpret physical simulations with the simulation app.
metadata:
  excalibur:
    group: creative-tools
    version: 1.0.0
    platforms:
    - linux
    tools:
    - simulation_ops
    display:
      name_en: Physics simulation
      name_es: Simulación física
      desc_en: Define, run and interpret physical simulations with the simulation app.
      desc_es: Definir, ejecutar e interpretar simulaciones físicas.
---
# Physics simulation

Use this skill to answer 'what happens if' with a model instead of a prototype. Defined scenarios, checked parameters, and interpreted results beat both guessing and overbuilding.

## When to use

- A physical behavior needs predicting.
- Testing scenarios too costly or slow in reality.
- Validating a design decision with numbers.

## Procedure

1. Define the scenario: geometry, materials, loads, and boundary conditions.
2. Check parameters and units twice; most sim errors are unit errors.
3. Run with mesh or resolution adequate to the question asked.
4. Interpret against expectations and a sanity case before trusting.
5. Report setup, results, and limits together.

## Output

Simulation results with setup, interpretation, and stated limits.

## Pitfalls

- Unit errors dominate; verify units explicitly.
- A simulation without a sanity case is a colorful guess.
