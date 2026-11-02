---
name: data-charts
description: Render reproducible bar and line charts from tabular data with chart_render.
metadata:
  excalibur:
    group: diagrams
    version: 1.1.0
    platforms:
    - linux
    tools:
    - chart_render
    - spreadsheet_ops
    display:
      name_en: Data charts
      desc_en: Render reproducible bar and line charts from tabular data with chart_render.
---
# Data charts

## When to use

Compare categories or show trends from tabular data.

## Procedure

1. Use bars for category comparisons or lines for trends. Keep axes honest and identify the data source and any uncertainty.
2. Call `chart_render` with `kind`, `title`, labels in `x`, and named numeric `series`. The built-in chart renderer supports SVG.
3. Inspect labels and readability at the delivery size.
4. Deliver the chart artifact's `Path` and `SourcePath`; the tool saves the chart and its JSON specification, including the data. Omit `output` to save under `artifacts/`.

## Output

A rendered chart and reproducible specification in `Result.Artifacts`.

## Pitfalls

- The diagram tool's PNG/PDF support does not imply the chart backend supports those formats.
- Explain truncated axes and identify sample sizes when they affect interpretation.
