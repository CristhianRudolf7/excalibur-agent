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
{"theme":{"accent":"#6366F1","style":"light"},"slides":[
  {"kind":"title","title":"Quarterly report","subtitle":"November 2026"},
  {"kind":"bullets","title":"Results","bullets":[{"text":"Growth","level":0},{"text":"Details","level":1}],"notes":"Speaker notes"},
  {"kind":"two_column","title":"Comparison","left":[{"text":"Before"},{"text":"Manual checks","level":1}],"right":[{"text":"After"},{"text":"Automated checks","level":1}]},
  {"kind":"image","title":"Product preview","image":"screenshot.png","caption":"The new workspace"},
  {"kind":"table","title":"Metrics","rows":[["Metric","Value"],["Sales","12"]]},
  {"kind":"section","title":"What comes next","subtitle":"From evidence to action"},
  {"kind":"cards","title":"Key facts","cards":[{"title":"Tests","text":"Passing across the suite","value":"38"},{"title":"Platforms","text":"One consistent experience","value":"3"}]},
  {"kind":"quote","title":"Customer perspective","quote":"A simpler way to work.","attribution":"Pilot customer"},
  {"kind":"title","title":"Ready for the next step","subtitle":"Agree on the pilot launch"}
]}
```

Decks are 16:9 (12192000 × 6858000 EMU), with their own blank layout, master
and theme. Existing specs receive the light navy/indigo design automatically:
Calibri headings and body with common sans-serif substitution in LibreOffice,
neutral backgrounds, consistent margins, accent bars and slide numbers. Title
slides have a dark background. Content titles are 34 pt, body text 21–24 pt,
and title-slide headings 54 pt, with 1.25 line spacing. Text scales down when
necessary; content that cannot fit is rejected rather than silently overflowing.
Move excessive detail into notes or split it across slides.

The optional top-level `theme` accepts `accent` (`#RRGGBB`) and `style` (`light`
or `dark`, default `light`). Either field can be omitted. Accent text and table
headers adapt for readable contrast, including very pale accent colors.
The top-level `title` optionally prepends a title slide. Any slide can have
`notes`. All text remains literal and XML-escaped.

| Kind | Fields and behavior |
| --- | --- |
| `title` | `title`, optional `subtitle`; large white heading on navy. |
| `bullets` | `bullets`: text/level objects, levels 0–8; hanging indents and smaller muted sub-bullets. |
| `two_column` | `left`/`right`: text/level arrays in two rounded cards; the first level-0 item in each column becomes its bold accent heading. |
| `image` | `image`: workspace PNG/JPEG; fitted inside a bordered area with original aspect ratio. Optional `caption` below it. |
| `table` | `rows`: rectangular nonempty rows; the first row is a white-on-accent header, followed by striped rows with proportional column widths and cell padding. The table is vertically centered. |
| `section` | Required nonblank `title`, optional `subtitle`; large heading on the accent background. |
| `cards` | Required `cards` array of 2–4 objects, each with nonblank `title` and `text`; optional `value` displays a large metric. |
| `quote` | Required nonblank `quote`, optional `attribution`; `title` labels the slide. |

Use one idea per slide and 3–5 bullets of at most 10 words each. Start with a
title slide. Use `cards` for key facts or metrics, `section` between parts, and
`table` for steps or comparisons. Include useful screenshots/images when
available, such as browser captures of a web app. End with a clear closing slide
and put explanation and supporting detail in speaker notes.

The title and main body stay first and second in shape order; their existing
`document_edit` IDs remain 2 and 3 (`slide:1/shape:2` and `slide:1/shape:3`).
For columns/cards, shape 3 is the first column/card; the others follow it.
Decorations come after content. Image and table bodies retain ID 3 as well;
text replacement supports text shapes, while table/image bodies are read as
structured content by `document_read`.

These are editable files. There is no tracked-change editing, chart or pivot
creation, formula evaluation, rendering, or automatic visual layout verification.

Specs are bounded to 2,000 Word blocks, 100 sheets, 500 slides, 20,000 total
cells/list items and 4 MiB of text. Packages are limited to 4,096 parts and
64 MiB of total uncompressed data. XML depth is capped at 256; DTDs are rejected.
Combined embedded image bytes are additionally capped at 32 MiB before parts
accumulate in memory.
