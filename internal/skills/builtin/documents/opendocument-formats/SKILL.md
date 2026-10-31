---
name: opendocument-formats
description: Work with OpenDocument formats (ODT, ODS, ODP).
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_read
    - document_create
    - file_convert
    display:
      name_en: OpenDocument formats
      name_es: Formatos OpenDocument
      desc_en: Work with OpenDocument formats (ODT, ODS, ODP).
      desc_es: Trabajar con ODT, ODS y ODP.
---
# OpenDocument formats

Use this skill for ODT, ODS, and ODP files. Native handling preserves open-format structure instead of degrading through lossy conversions.

## When to use

- Reading or writing OpenDocument files.
- LibreOffice-based workflows.
- Converting to or from open formats.

## Procedure

1. Read the document natively, preserving styles and structure.
2. Edit with the format's own features, not DOCX-isms.
3. Convert only at boundaries, checking fidelity after each conversion.
4. Validate the result opens in LibreOffice as intended.
5. Prefer staying in OpenDocument when the workflow allows.

## Output

OpenDocument files handled natively with verified conversions.

## Pitfalls

- Round-trips through proprietary formats degrade; minimize them.
- Verify in LibreOffice, not just by file size.
