---
name: financial-modeling
description: Build financial models and projections in spreadsheets.
metadata:
  excalibur:
    group: finance
    version: 1.0.0
    platforms:
    - linux
    tools:
    - spreadsheet_ops
    - exec
    display:
      name_en: Financial modeling
      name_es: Modelos financieros
      desc_en: Build financial models and projections in spreadsheets.
      desc_es: Construir modelos y proyecciones en hojas de cálculo.
---
# Financial modeling

Use this skill for projections that decide money. Explicit assumptions, separated inputs, and scenario analysis make a model a decision tool instead of a formatted guess.

## When to use

- Projecting revenue, costs, or runway.
- Comparing financial scenarios.
- A decision needs numbers with ranges.

## Procedure

1. Separate inputs, calculations, and outputs; no constants buried in formulas.
2. State every assumption with its source or rationale.
3. Build base, upside, and downside scenarios with sensitivity on key drivers.
4. Check the model's arithmetic and logic with test cases.
5. Present ranges and drivers, never false-precision point estimates.

## Output

A scenario model with stated assumptions and sensitivity.

## Pitfalls

- Point estimates lie; present ranges.
- Buried constants corrupt; separate inputs.
