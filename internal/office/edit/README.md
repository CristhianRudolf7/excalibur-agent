# Guarded Office edits

`edit.File(ctx, path, expectedSHA256, operations)` reads the original once,
checks its hash, edits an in-memory package, validates a re-opened serialized
package, checks the original hash again, and atomically replaces the file.
Any failed operation aborts the whole batch. The result has `sha256`, `summary`,
`anchors` and `issues`. A hash mismatch includes `current_sha256` in its error.
The tool executor serializes calls using the canonical file resource key.

Operations are JSON objects matching the exported `Operation` type:

| Format | Action | Fields |
| --- | --- | --- |
| DOCX | replace_text | anchor or match, match, text, all (default false) |
| DOCX | insert_paragraph | anchor or match, text, style, position (before/after; default after) |
| DOCX | delete_paragraph | anchor or match |
| DOCX | append_table | rows, header |
| XLSX | set_cell | sheet, cell (the creation Cell spec) |
| XLSX | add_sheet | new_sheet (the creation Sheet spec), or name |
| XLSX | rename_sheet | sheet, name |
| XLSX | delete_rows | sheet, start, count (1-based) |
| PPTX | replace_text | slide or anchor, match, text, all |
| PPTX | set_notes | slide, text |
| PPTX | duplicate_slide | slide (copy inserted immediately after it) |
| PPTX | delete_slide | slide |
| PPTX | move_slide | slide, to (final 1-based position) |

Word anchors are native `w14:paraId` values, with `paragraph:N` fallback for
paragraphs without IDs. Presentation anchors are `slide:N/shape:ID`. Spreadsheet
anchors are `Sheet!A1`. Re-read after edits: positional anchors can change.
A match selecting multiple paragraphs is rejected for insert/delete. Replacement
is paragraph-local and supports split runs; the inserted text inherits the first
participating run's formatting. Unknown parts and unrelated XML bytes survive.
Only compressed ZIP representation and ZIP metadata may change.

Row deletion supports simple sheets. It rejects any shared/array formula or table
in the sheet, plus merges, data validation, conditional formatting and drawings.
It updates row/cell coordinates and simple A1 formula references, including
qualified references from other sheets; it does not support external/structured
references or INDIRECT. There is no formula calculation or cached formula output.
Renaming sheets updates explicitly qualified formula references.
Slide deletion removes the slide from presentation order; unreferenced original
parts remain to preserve opaque content. Duplicates reuse media and layouts,
but receive independent speaker notes. No tracked-change editing, charts or
pivots are provided. Rendering and visual layout verification are outside scope.
Setting a cell inside an existing shared/array formula range is rejected to
preserve the formula group's structure. The cell's existing style and unrelated
attributes survive ordinary value/formula updates.
Text replacement is bounded to 100,000 matches per paragraph; larger edits fail
without writing. XML patches are applied in a single pass so numerous matches do
not repeatedly copy the entire part.
