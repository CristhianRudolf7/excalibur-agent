---
name: spreadsheets
description: Create, read and edit Excel (XLSX), Google Sheets and CSV spreadsheets, with formulas, formats,
  pivot tables and charts; also the skill of the spreadsheet app.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - spreadsheet_ops
    - document_read
    - document_create
    display:
      name_en: Spreadsheets
      name_es: Hojas de cálculo
      desc_en: Create, read and edit Excel (XLSX), Google Sheets and CSV spreadsheets, with formulas,
        formats, pivot tables and charts; also the skill of the spreadsheet app.
      desc_es: Crear y editar XLSX, Sheets y CSV con fórmulas y gráficos.
---
# Spreadsheets

Use this skill for spreadsheet work that must compute correctly. Formulas audited, formats consistent, and pivots reproducible — numbers you can defend.

## When to use

- Building or fixing XLSX, Sheets, or CSV files.
- Formulas, pivots, or charts are involved.
- Acting as the spreadsheet app's skill.

## Procedure

1. Read the workbook map: sheets, ranges, named ranges, existing formulas.
2. Write formulas with explicit references; audit precedents before trusting totals.
3. Keep formats and validation consistent; no hardcoded constants inside formulas.
4. Rebuild pivots and charts from the data after structural edits.
5. Verify by recalculating and spot-checking totals against sources.

## Output

Working spreadsheets with audited formulas and verified totals.

## Pitfalls

- Hardcoded numbers inside formulas hide breakage; use cells.
- Verify totals against sources, not against themselves.
