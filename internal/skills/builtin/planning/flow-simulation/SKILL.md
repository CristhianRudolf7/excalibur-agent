---
name: flow-simulation
description: Formalize a solution as a canonical flow and review its scenarios with the path simulator
  (21.6) before implementing.
metadata:
  excalibur:
    group: planning
    version: 1.0.0
    platforms:
    - linux
    tools:
    - flow_simulate
    - task_db
    display:
      name_en: Flow simulation
      name_es: Simulación de flujos
      desc_en: Formalize a solution as a canonical flow and review its scenarios with the path simulator
        (21.6) before implementing.
      desc_es: Formalizar una solución como flujo canónico y revisar sus escenarios.
---
# Flow simulation

Use this skill when the solution is a process with branches: approvals, retries, timeouts, user decisions. Simulating the canonical flow exhaustively finds dead ends and loops before any code exists.

## When to use

- The solution is a workflow, state machine, or multi-step procedure.
- Branches, retries, approvals, or timeouts interact.
- The cost of a runtime surprise is high.

## Procedure

1. Formalize the flow as canonical nodes: steps, conditions, waits, approvals, compensations.
2. Enumerate scenarios up to the simulator limit, declaring the coverage achieved.
3. Run flow_simulate and collect path descriptors and narrations.
4. Review each failing or uncovered path: fix the flow, not the simulator.
5. Record the accepted flow and its coverage as plan evidence.

## Output

A reviewed canonical flow with simulated paths, coverage statement, and fixes applied before implementation.

## Pitfalls

- Never simulate the implementation; simulate the canonical flow first.
- Uncovered paths are findings, not leftovers; justify or cover each.
