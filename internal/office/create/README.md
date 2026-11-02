# Office creation specs

`create.Build(format, Spec, ImageLoader)` returns an OPC package. The format is
`docx`, `xlsx` or `pptx`. All text is literal text and escaped as XML. Images
must be PNG or JPEG, at most 16 MiB and 20,000 pixels per dimension. The tool
supplies a loader that confines image paths to the workspace. No macros run.

DOCX example:

```json
{"title":"Report","page":{"size":"A4","margin_twips":1440},"blocks":[
  {"kind":"heading","level":1,"text":"Results"},
  {"kind":"paragraph","runs":[{"text":"Important","bold":true},{"text":" code","code":true}]},
  {"kind":"bullet","level":0,"text":"First item"},
  {"kind":"numbered","text":"First step"},
  {"kind":"table","header":true,"rows":[["Name","Value"],["A","12"]]},
  {"kind":"page_break"},
  {"kind":"image","image":"figure.png"}
]}
```

Headings accept levels 1–3. Lists accept levels 0–8. Runs accept `text`,
`bold`, `italic`, and `code`. Paragraphs can name a `style`; the built-in styles
are Normal, Title and Heading1–Heading3. `text` is shorthand for one run.
Page size is A4 (default) or Letter; margins are uniform in twips (1440 default).
Images use a fixed 6 by 4 inch box. Tables use equally sized cells.

XLSX example:

```json
{"sheets":[{"name":"Data","freeze_first_row":true,"column_widths":[24,16],"cells":[
  {"ref":"A1","value":"Name","header":true},
  {"ref":"B2","type":"number","value":12.5},
  {"ref":"C2","type":"bool","value":true},
  {"ref":"D2","type":"date","value":"2026-11-02"},
  {"ref":"E2","formula":"SUM(B2:B10)"}
]}]}
```

Cell types are string (default), number, bool and date (ISO date or RFC3339).
Formulas have no cached result; Excel must recalculate on open. Column widths
are in character units. Cell references are uppercase A1 references.

PPTX example:

```json
{"slides":[
  {"kind":"title","title":"Quarterly report","subtitle":"November"},
  {"kind":"bullets","title":"Results","bullets":[{"text":"Growth","level":0},{"text":"Details","level":1}],"notes":"Speaker notes"},
  {"kind":"two_column","title":"Comparison","left":[{"text":"Before"}],"right":[{"text":"After"}]},
  {"kind":"image","title":"Diagram","image":"figure.png"},
  {"kind":"table","title":"Metrics","rows":[["Metric","Value"],["Sales","12"]]}
]}
```

Decks are 16:9 with their own blank layout, master and theme. Any slide can
have `notes`. The top-level `title` optionally prepends a title slide.
Bullet levels are 0–8. Tables must have rectangular nonempty rows.

These are basic editable files. There is no tracked-change editing, chart or
pivot creation, formula evaluation, rendering, or visual layout verification.

Specs are bounded to 2,000 Word blocks, 100 sheets, 500 slides, 20,000 total
cells/list items and 4 MiB of text. Packages are limited to 4,096 parts and
64 MiB of total uncompressed data. XML depth is capped at 256; DTDs are rejected.
Combined embedded image bytes are additionally capped at 32 MiB before parts
accumulate in memory.
