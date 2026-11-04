# Delegated code quality

The daemon's autonomous delivery gate checks maintainability before completion
or publication. This extends the existing delivery evidence contract in the
`default` profile. The implementer brief and current UI quality skill carry the
same guidance. Frozen evaluation instructions and tool contracts are unchanged.

## Readable source

The daemon scans the workspace independently of the requirement file list. For
hand-written source, tests and styles it rejects:

- Lines longer than 200 characters outside comments, string literal bodies and
  literal numeric/string data arrays.
- Average code line length above 100 characters, ignoring blank/comment/data lines.
- More than two top-level semicolon-terminated statements on a line. Loop header
  separators and statements inside blocks do not count toward this threshold.
- Source files longer than 600 physical lines, or larger than the 4 MiB scan limit.

Refusals include `file:line` so the worker can format or split the offending code.
The lexical scan preserves template interpolation code and diagnostic line
numbers. It is a conservative heuristic, not a full language parser or a judgment
of identifier quality. The brief requires descriptive names, small functions,
backend route/data/seed modules and frontend components. The file size bound
helps enforce modularity without imposing a particular framework layout.

Dependency, vendor, build, coverage, virtual-environment and generated directories
are excluded. Non-source assets, data files and lockfiles are excluded by source
extension. Standard generated headers (`Code generated ... DO NOT EDIT.` or
`@generated` in an opening comment) exclude generated source. Source symlinks are
rejected. All traversal and reads use the confined workspace root.

Workers apply the project's formatter output, using a standard offline formatter
when none is configured, and record commands and every covered source file:

```json
{
  "formatters": [
    {"check": "prettier --check .", "files": ["src/example.js"]}
  ]
}
```

A formatter must have a successful daemon-observed process exit after the latest
source edit and target a compatible source file. Direct prettier, gofmt, black,
ruff format, rustfmt, clang-format and shfmt invocations are recognized. Simple
package.json scripts can wrap these. Use `npx --no-install` for a local formatter;
network installation, shell chains and commands merely mentioning a formatter
are rejected. `gofmt -w` is supported; `gofmt -l` alone exits successfully even
when code needs formatting and is insufficient. Version/help and diff-only
invocations do not satisfy formatting. Project configuration remains authoritative
when the formatter runs. This gate observes execution and coverage declarations;
it does not independently reimplement each formatter or its ignore rules.

## Backend checks

`delivery_source_test.go` exercises exact thresholds, literals and comments,
template interpolation, data arrays, module boundaries, exclusions, files omitted
from the requirement matrix, real command recognition, project formatter scripts,
incompatible targets, failed receipts and stale receipts. Tests use temporary
workspaces and fake tool results; they make no paid calls.

## Executed control evidence

Every reviewed screen needs a browser-generated inventory and a successful
assertion for each discovered control. The browser suite enumerates native and
ARIA interactive elements, forms and contenteditable elements, including the
union of controls revealed in conditional states. It verifies disabled states
and how controls become enabled. The report keeps inventory and assertions
separate so missing checks cannot silently disappear from a list of successes.

Add the executed command and report path to the delivery manifest:

```json
{"ui":{"check":"node check-browser.mjs","report":"browser-report.json"}}
```

The report schema is:

```json
{
  "runner": "playwright",
  "screens": [{
    "name": "example",
    "url": "http://127.0.0.1:5308/example",
    "discovered": 1,
    "controls": [{
      "id": "query",
      "label": "Search",
      "selector": "input[type=search]",
      "kind": "search"
    }]
  }],
  "checks": [{
    "screen": "example",
    "control": "query",
    "action": "fill with a selective query",
    "effect": "rows",
    "target": "tbody tr",
    "before": ["alpha", "beta"],
    "after": ["alpha"],
    "passed": true
  }]
}
```

The runner is playwright, puppeteer or webdriver. Screen names are unique slugs.
Discovered counts must match enumerated controls; IDs are unique within a screen.
Every control needs a label, selector and supported kind. Each assertion must
reference an inventoried control and include the action, target and a successful
observed before/after effect. Missing screens, failed assertions, unknown controls,
unsupported kinds, malformed reports and unchanged state block delivery.

| Control kind | Required observed state |
| --- | --- |
| search, filter | `rows` arrays shrink for a selective query/filter |
| field | `form` objects with `values` or `errors` maps change |
| form | `form` state changes, or `records` change after submission/read-back |
| detail | `detail` becomes a visible rendered `record` object |
| link | `route` strings change |
| add-item, remove-item | `items` arrays grow/shrink in the labelled direction |
| button, submit | rows, items, records, form, detail, route or boolean dialog state changes |

Message, notice and toast effects never pass. Echoing the query into a form field
cannot satisfy search/filter evidence. A flat API response cannot satisfy a
rendered detail assertion. Record state uses arrays/objects; detail state uses
`{"visible":true,"record":{...}}`; form state uses `{"values":{...}}` or
`{"errors":{...}}`. Arrays of row identities/values come from the live DOM;
record assertions use persisted read-back results. Assertions must test the
control's label, rather than capture an unrelated changing element.

Forms implying multiple items must use variable-length controls, declare the
form inventory entry `repeatable:true`, and associate add/remove entries with
its ID using `form`. The gate requires successful checks proving both growth and
shrinkage, as well as the form assertion. The brief requires adding beyond the
initially rendered rows and verifying saved items after reload.

The daemon requires a successful observed process exit, checks that the report
was not edited after it, and invalidates browser evidence after any hand-written
source edit. The report must be regenerated after the latest source edit. Browser
assertion semantics, DOM discovery completeness and accurate classification of
multiple-item forms remain the suite author's responsibility: the daemon validates
structured evidence and execution receipts; it does not itself drive the browser
or infer every possible UI state from source. Screenshot review is still required
separately. These checks make missing and ineffective declared controls actionable
without claiming that self-reported JSON independently proves browser behavior.

`delivery_ui_test.go` covers selective filters, unchanged/notice-only controls,
rendered details versus flat responses, dynamic item counts, input and validation
state, record persistence evidence, route changes, missing inventories/checks,
duplicate/unknown controls, failed receipts and source/report freshness. Existing
delivery tests include the new evidence in their fake complete build fixture.

## Durable numbering

The implementer brief and current UI quality skill require document numbers to
come from a database sequence or `MAX(existing_number) + 1` inside the same
transaction as record insertion. Never use `count+1` or collection length: row
deletion leaves gaps and can make a later insertion collide with an existing
number. Internal identifiers should use database-generated primary keys.

`MAX+1` allocation must be serialized using database-appropriate locking/isolation,
or retry the entire transaction on a uniqueness conflict. Allocation and insertion
remain atomic through rollback. A durable sequence is preferred when numbers
must never be reused; gaps are permitted unless explicitly forbidden. Workers
verify creation after deletions, concurrent creation and rollback behavior. This
is implementation and verification guidance, rather than a static SQL classifier.
The autonomous handoff tests confirm this guidance reaches the worker's brief.

## Verification on 2026-11-04

Tests ran under `systemd-run --user --scope -q -p MemoryMax=6G`, with
`nice -n 19`, race detection and Go package parallelism limited to two.
Builds, vet and staticcheck also ran at low priority with package parallelism two.

| Check | Result |
| --- | --- |
| Full daemon and skills race suites | Passed |
| Engine serialized prefix audits, twenty-turn stability and startup guidance | Passed |
| Embedded/evaluation profile hash pins and frozen skill baselines | Passed |
| Focused source, formatter, control report, repeatable form and handoff regressions | Passed |
| `go vet -p 2 ./...` | Passed |
| `go build -p 2 ./...`, Linux, Darwin and Windows | Passed |
| Repository gofmt, whitespace and bare-goroutine checks | Passed |
| Staticcheck for daemon and skills | Passed |
| Frozen evaluation path diff against the starting revision | Empty |
| Repository-wide staticcheck | Seven existing findings outside the changed packages |

The existing staticcheck findings are five ST1005 diagnostics in
`internal/googleapi/tools.go` at lines 193, 216 and 218, and two unused functions
in `internal/robotlab/requirements_nondarwin.go` at lines 9 and 12. Both files are
unchanged from the starting revision. No paid calls or live delegated browser
builds were executed; the gate regressions use fake receipts and browser reports.
