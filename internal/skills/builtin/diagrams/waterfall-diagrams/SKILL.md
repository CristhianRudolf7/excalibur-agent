---
name: waterfall-diagrams
description: Waterfall diagrams showing duration and overlap of processes.
metadata:
  excalibur:
    group: diagrams
    version: 1.0.0
    platforms:
    - linux
    tools:
    - diagram_render
    - metrics_query
    display:
      name_en: Waterfall diagrams
      name_es: Diagramas de cascada
      desc_en: Waterfall diagrams showing duration and overlap of processes.
      desc_es: Diagramas de cascada con duración y solapamiento.
---
# Waterfall diagrams

Use this skill to show where time goes in layered processes. Duration bars with overlaps reveal the long pole and the idle gaps optimization should target.

## When to use

- Analyzing latency across stages.
- Showing resource or time breakdowns.
- Justifying an optimization target.

## Procedure

1. Measure each stage's duration from real data, not estimates.
2. Draw bars to scale with overlaps where work runs parallel.
3. Highlight the long pole and the idle gaps between stages.
4. Annotate what each stage waits on.
5. Re-measure after changes; keep the chart as evidence.

## Output

A scaled waterfall with measured stages and identified long pole.

## Pitfalls

- Estimated bars mislead; measure each stage.
- Overlaps drawn wrong invert the conclusion; verify parallelism.
