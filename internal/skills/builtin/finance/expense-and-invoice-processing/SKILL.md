---
name: expense-and-invoice-processing
description: Extract data from invoices and receipts, record and reconcile them.
metadata:
  excalibur:
    group: finance
    version: 1.0.0
    platforms:
    - linux
    tools:
    - vision_extract
    - spreadsheet_ops
    display:
      name_en: Expense and invoice processing
      name_es: Gastos y facturas
      desc_en: Extract data from invoices and receipts, record and reconcile them.
      desc_es: Extraer datos de facturas, registrar y conciliar.
---
# Expense and invoice processing

Use this skill to turn paper and PDFs into clean books. Extracted, verified, reconciled records keep expenses deductible and audits boring.

## When to use

- Invoices or receipts pile up.
- Expenses need recording and reconciling.
- Preparing for tax or reimbursement.

## Procedure

1. Extract vendor, date, lines, tax, and total per document.
2. Verify extraction against the original; fix misreads immediately.
3. Record with categories and link to the source document.
4. Reconcile against statements; investigate every mismatch.
5. Archive originals where they can be retrieved for audit.

## Output

Reconciled expense records linked to source documents.

## Pitfalls

- Unverified extraction invents money; check every total.
- Unreconciled books hide errors; reconcile fully.
