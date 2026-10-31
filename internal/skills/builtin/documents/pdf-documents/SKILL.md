---
name: pdf-documents
description: Read, create, edit, merge, split and fill PDF forms, and redact sensitive content.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_read
    - document_create
    - pdf_ops
    display:
      name_en: PDF documents
      name_es: Documentos PDF
      desc_en: Read, create, edit, merge, split and fill PDF forms, and redact sensitive content.
      desc_es: Leer, crear, editar y rellenar PDF, y redactar contenido.
---
# PDF documents

Use this skill for PDF operations end to end. Extraction with anchors, forms filled correctly, and redactions that actually remove content — not just cover it.

## When to use

- Reading, creating, or editing PDFs.
- Filling or validating forms.
- Redacting sensitive content before sharing.

## Procedure

1. Extract with page, section, and paragraph anchors for citation.
2. Fill forms field by field; validate required fields and formats.
3. Merge and split preserving order, metadata, and accessibility tags where present.
4. Redact by removing content plus its metadata, then verify extraction yields nothing.
5. Confirm the final PDF opens and prints as intended.

## Output

PDFs created, edited, or redacted with verification.

## Pitfalls

- Visual black boxes are not redaction; verify by extraction.
- Check form validation rules before submitting.
