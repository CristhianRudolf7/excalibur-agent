---
name: word-documents
description: Create, read and edit Word documents (DOCX) preserving styles, tables, comments and change
  tracking.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_read
    - document_create
    - document_edit
    display:
      name_en: Word documents
      name_es: Documentos Word
      desc_en: Create, read and edit Word documents (DOCX) preserving styles, tables, comments and change
        tracking.
      desc_es: Crear, leer y editar DOCX con estilos y control de cambios.
---
# Word documents

Use this skill for DOCX work that must survive round-trips. Styles, tables, comments, and tracked changes are preserved — the document stays editable by its human owners.

## When to use

- Creating, reading, or editing Word files.
- Review workflows with comments or tracked changes.
- Reports that must match corporate templates.

## Procedure

1. Read the document structure first: styles, sections, tables, existing comments.
2. Edit through structured operations preserving styles; never paste raw formatting over them.
3. Keep change tracking on during review edits; resolve comments explicitly.
4. Verify tables, numbering, and cross-references after every edit.
5. Deliver with tracked changes visible when the user must approve.

## Output

DOCX files with styles, tables, and review marks intact.

## Pitfalls

- Style overrides by direct formatting rot templates; use styles.
- Unresolved comments ship confusion; resolve or reply to each.
