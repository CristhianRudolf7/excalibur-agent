---
name: google-docs
description: Create, read and edit Google Docs documents, including comments and suggestions.
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
      name_en: Google Docs
      name_es: Documentos de Google
      desc_en: Create, read and edit Google Docs documents, including comments and suggestions.
      desc_es: Crear, leer y editar Google Docs con comentarios.
---
# Google Docs

Use this skill for Google Docs collaboration. Suggestions mode, comments, and shared structure let humans review while the agent drafts.

## When to use

- Drafting or editing Docs with collaborators.
- Review cycles with suggestions and comments.
- Exporting Docs content faithfully.

## Procedure

1. Confirm the exact document and the collaborators' expectations.
2. Draft or edit with suggestions on when changes need review.
3. Reply to comments; never silently resolve a human's question.
4. Preserve headings, tables, and structure for navigation and export.
5. Confirm sharing state before and after sensitive edits.

## Output

Docs drafts with suggestions handled and structure preserved.

## Pitfalls

- Silent comment resolution erases questions; reply first.
- Check sharing before touching sensitive documents.
