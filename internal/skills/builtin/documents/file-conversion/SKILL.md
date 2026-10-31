---
name: file-conversion
description: Choose the right conversion between formats with file_convert and check that the result preserves
  content and structure.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - file_convert
    display:
      name_en: File conversion
      name_es: Conversión de archivos
      desc_en: Choose the right conversion between formats with file_convert and check that the result
        preserves content and structure.
      desc_es: Elegir la conversión adecuada y comprobar el resultado.
---
# File conversion

Use this skill when content must change format. Choosing the supported pair deliberately and verifying content and structure afterwards prevents silent data loss.

## When to use

- Converting between document or data formats.
- Choosing between several conversion paths.
- Validating a converted deliverable.

## Procedure

1. Check the converter manifest for supported pairs before promising anything.
2. Pick the pair preserving the most structure: styles, tables, formulas, metadata.
3. Convert and diff: content completeness plus structural spot-checks.
4. Fix or re-route when elements drop; document accepted losses.
5. Deliver with a conversion note: path used and verified.

## Output

A converted file with a verification note and accepted losses.

## Pitfalls

- Unverified conversion is data loss with confidence.
- Document what did not survive; do not hide it.
