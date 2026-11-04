# Autonomous delivery from an approved client specification

This contract belongs to daemon chat and the `default` tool profile. The CLI,
evaluation profiles, embedded tool definitions, orchestration contracts and
frozen skills are unchanged.

When a user commands a complete build with an attached specification (including
a recent user attachment), the daemon creates an implementer delegation directly.
The conversational model cannot insert a project-registration question or refuse
the build before handoff. Unscoped work uses the existing daemon-managed Git
workspace provisioner. The card carries the recent user request, document IDs,
full-spec criteria, UI quality skill and an evidence contract. The worker receives
the attachment text and available originals through the existing document handoff.
It chooses defaults, records assumptions and has no `ask_user` tool. An initial
work-started acknowledgement is allowed; the verified report follows in chat.
A document approval alone receives a deterministic acknowledgement without a
question and does not start a build. Final successful reports are generated from
verified evidence, with the verified preview link appended by the existing output
pipeline.

## Public previews

Authorization is derived deterministically from user request text, never from
attachment text, assistant output, a child goal or `confirmed:true`. An explicit
public or Cloudflare link request authorizes both static publication and exposure
of the application's existing localhost HTTP port. No second confirmation is
required. Without that request, the `public_preview` tool rejects publication
before starting a tunnel. The captured policy follows queued and nested children.
Recent user turns from the same attached-spec build can carry authorization; a
newer refusal or local-only request revokes it, and a new attachment starts a new
authorization boundary. Authorized child registries explicitly share the daemon
preview manager, independently of deferred-tool keyword ranking.

Publication requires a successful HTTP GET of the local app and public URL. Empty
bodies, unsuccessful status codes and recognizable tunnel/application error pages
are rejected. When the local page has a title, the public title must match. The
verified URL is stored only after these checks and is appended to the final chat
report with expiry metadata. This is a reachability and basic identity check;
automated app/browser tests still verify actual features. Cloudflare Quick Tunnel
availability remains an external prerequisite.

## Waiting and user limits

Daemon `subagent` definitions add `await`. Running-child `status` calls from
coordinators also block until progress, a state change, cancellation or a ten-minute
wait timeout. The daemon checks state internally at 200 ms intervals; the model
does not repeatedly poll. Immediate status remains available for root user status
requests. The wait is a reporting interval, not a child execution deadline.

Delegated chat builds use a 10,000-step emergency guard with no default wall-clock,
token or dollar cap. Coordinator-supplied deadlines and small card budgets cannot
create user urgency. Explicit textual limits such as a number of minutes/hours or
a spending limit enable the corresponding bounds. A user deadline is captured
once and retained by nested children, so delegation cannot restart its clock.
The task service likewise has no four-hour default deadline and honors explicit
step limits. Evaluation task profiles retain their existing admission limits.
Existing parent budget clamping remains authoritative.

The daemon rejects messages/redirects telling children to hurry, finish immediately,
stop expanding work or stop making changes when the user set no time/cost limit.
Messages that ask for missing features or verification remain available. English
and Spanish instruction forms are covered. This deterministic wording filter is
not a semantic classifier for every possible paraphrase.

## Login and user prompts

The zero-question policy follows every nested worker, including screenshot
children. `request_credentials` returns `status: denied` immediately in these
runs, with guidance rather than a form. It checks this policy before saved-login
lookup, conversation notifications, callbacks or the ten-minute credential wait.
A workspace app must use its own demo credentials from README, seed data or
`.env.example`, typed directly with `browser_act`. Non-secret demo credentials
are not user secrets and do not need a `secret://` reference.

Loopback previews in a delegated workspace are treated as that workspace's app.
Successful `exec` receipts from running sessions also record local server URLs;
subsequent `exec_io` receipts must belong to those sessions. These server hosts
and ports follow descendants, so a screenshot child recognizes the implementer's
server, including a private-address alias on the same port. Literal private
addresses without a server receipt do not by themselves establish ownership.
Unrelated public origins are never classified as workspace apps through DNS or
by sharing a port number. Interactive workspace-app credential requests also
return demo-login guidance; ordinary interactive local and third-party logins
retain secure forms and saved-login reuse.

Browser login and OTP hints read the current tab URL, including redirects, through
the daemon browser adapter. Own-app hints explicitly direct the worker to its
README/seed credentials. External delivery logins may use existing authorized
references; without them the worker must continue with alternatives. Interactive
external hints keep the secure-form instructions. Unknown or ambiguous tab
origins do not receive own-app advice. Loaded login forms remain observable and
successful form actions retain their acknowledgement, allowing direct demo login.

The daemon tool runner denies question/confirmation tools before dispatch during
zero-question delivery. It also replaces structured confirmation-required results,
capability input requests and denied adapters' user-approval instructions with
local auto-denial guidance. Actionable browser login/OTP states use the login
policy above. Hidden `ask_user` calls and agenda confirmation nodes also deny
without publishing or waiting. The worker must use an authorized alternative,
record a default or skip the blocked operation; it cannot fabricate confirmation.
Every automatic denial appends a `user_prompt_denied` journal entry with the tool,
decision and policy guidance only. Prompt arguments and credentials are excluded.
Normal engine tool intent/result entries remain intact. A journal error leaves
the prompt denied. Interactive conversations retain their question and
confirmation behavior.

## Completion evidence

Autonomous workers receive the full `app-ui-quality` skill and must create
`delivery-evidence.json` in the workspace root:

```json
{
  "requirements": [
    {
      "id": 1,
      "feature": "Implemented behavior",
      "files": ["src/example.js"],
      "check": "node verify-api.js"
    }
  ],
  "screens": [
    {
      "name": "login",
      "desktop": "screenshots/login-desktop.png",
      "mobile": "screenshots/login-mobile.png",
      "reviewed": true
    }
  ],
  "formatters": [
    {"check": "prettier --check .", "files": ["src/example.js"]}
  ],
  "ui": {"check": "node check-browser.mjs", "report": "browser-report.json"},
  "data_delivery": {"check": "node verify-delivery-data.mjs", "report": "data-delivery-report.json"},
  "database_totals_check": "node verify-api.js",
  "demo_data_check": "node verify-api.js",
  "assumptions": ["Document chosen defaults here and in README.md"]
}
```

The example shows one row; the real manifest must include all source requirements
and screens. `delivery_check` reports missing evidence. The completion gate checks:

- Numeric requirement labels in extracted source text, including numbered Markdown
  and table rows, determine the required consecutive IDs (up to 100). Every ID
  needs a feature, existing nonempty source files and a successful executed check.
  Unavailable numbered source text blocks completion and is retried from the library.
- Checks must match commands observed by the daemon with exit code zero. Background
  commands count only after an owned `exec_io` session supplies the successful exit.
  A later failing execution invalidates that command's previous success. Referenced
  source files modified after their check require another successful execution.
- A nonempty README and recorded assumptions must exist. Separate manifest fields
  identify an executed database-total reconciliation test and realistic demo-data
  consistency check. The same comprehensive test suite may satisfy multiple rows.
- Screenshots must be real PNG/JPEG images at 1440×900 and 390×844, with review
  declared and a successful `inspect_image` receipt after the latest image change.
  The daemon explicitly grants the local read-only inspector to autonomous workers. Login/dashboard are required. Detected inventory, sales, purchases,
  suppliers, customers and stock-movement modules require their list/form screens.
  Screen names use `inventory-list`, `inventory-form`, and equivalent module names.
- Source readability, formatter receipts and per-control browser evidence must
  pass the [delegated code quality contract](delegated-code-quality.md). Every
  screen has a discovered control inventory; each control has an executed
  assertion proving its labelled effect. Notice-only controls block completion.
- Publication is blocked until implementation evidence passes. If a public link was
  requested, completion also requires a verified preview owned by the delivery run.

An incomplete final answer (including a question or an explicit finish call) becomes
an ordinary local `delivery_check` tool round. This returns the missing evidence to
the worker while retaining engine step, journal and budget accounting. A skeleton
cannot pass by claiming completion in prose. Ordinary delegated tasks retain their
existing gate.

The daemon checks evidence structure, file containment, image dimensions and actual
process exits and screenshot inspection calls. The worker remains responsible for meaningful test assertions and
visual inspection; these cannot be inferred from filenames or a review boolean.
The UI skill requires coherent product categories, plausible local names and
identifier formats, ledger-derived inventory and dashboard totals, SVG icons,
right-aligned tabular money, a full-height sidebar and mobile overflow review.
The director's real scenario remains the end-to-end acceptance check; ordinary
Go tests use fakes and never call paid services.

## Isolated verification and clean delivery data

Every automated test, API check and mutating browser check uses isolated storage:
a temporary file, in-memory database or separate `DB_FILE`/`DATABASE_URL`.
Verification must never write to the delivered application's storage. After all
referenced checks and final source edits, the worker resets/reseeds the delivered
data or proves that the complete persisted dataset equals a fresh seed. It then
captures the delivered app with read-only browsing before publication/completion.

The required `data_delivery` check generates this report and exits successfully:

```json
{
  "delivery_storage": "demo-store",
  "seed_fingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "delivered_fingerprint": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "verification": [
    {"check": "node verify-api.js", "storage": "temporary-store"},
    {"check": "node check-browser.mjs", "storage": "temporary-store"}
  ]
}
```

Fingerprints are lowercase SHA-256 digests of a canonical export of the complete
persisted dataset, compared with a fresh seed. Document any nondeterministic
metadata excluded; hashing seed source files alone is insufficient. Storage
identities are canonical and credential-free; the worker resolves path/URL aliases
and symlinks. Never record connection credentials. Every requirement, totals,
demo and browser check command must have its actual isolated storage recorded.

The gate rejects absent reports/receipts, equal or missing storage identities,
missing/duplicate verification commands, unequal or malformed fingerprints, failed
executions and proof older than verification or source/report changes. Both
publication and completion use this gate. Rerunning verification requires new seed
proof. The daemon validates structure and observed exits; correct storage routing
and full-dataset fingerprint computation remain the verifier's responsibility.
`delivery_data_test.go` covers this contract with temporary workspaces and fake
execution receipts, without running a delivered application or paid services.

## Dashboard coherence evidence

The `data_delivery` report also requires `dashboard` observations from read-only
browser assertions against the clean delivered app, in that same executed check:

```json
{
  "dashboard": {
    "labels": [
      {"label": "Activity this month", "selector": "#activity-kpi"},
      {"label": "Daily activity history", "selector": "#history-heading"}
    ],
    "panels": [
      {"selector": "#attention-panel", "count": 0, "alert_visible": false, "empty_visible": true}
    ],
    "charts": [{
      "selector": "#history-chart",
      "seeded": [{"date": "2026-01-01", "value": 12}, {"date": "2026-01-02", "value": 18}],
      "rendered": [{"date": "2026-01-01", "value": 12}, {"date": "2026-01-02", "value": 18}]
    }]
  }
}
```

Inventory all KPI/section labels, count-based panels and history charts. Titles
must identify the measure and time scope. The gate rejects empty/duplicate labels
(ignoring case/spacing), missing/reused selectors and omitted inventories. Panels
need explicit counts and alert/empty visibility; zero requires a calm empty state
without an alert. Use `panels: []` when none exist.

Chart arrays cover the full displayed date range in ascending ISO date order.
Rendered dates and numeric values must equal independent fresh-seed aggregations
in the same units, with at least two populated days. A single-day spike, missing
dates or mismatched values blocks delivery. Verify persisted data still equals
the seed after read-only browsing. Review final desktop/mobile screenshots for
these conditions. The gate checks recorded observations; label meaning, complete
DOM discovery and independent seed aggregation remain the worker's responsibility.
The current `app-ui-quality` checklist carries the same requirements; frozen
skills are unchanged. `delivery_dashboard_test.go` uses fake observations for
label collisions, zero/positive counts, empty states and history/spike regressions.

## Backend verification

`delivery_policy_test.go` covers explicit link detection, denied unrequested
publication, authorized port publication and rejection of successful error pages.
`delegation_limits_test.go` covers blocking waits, cancellation/timeouts, hurry
rejection, user limits, generous budgets and the actual daemon runner's await path.
`autonomous_delivery_test.go` covers automatic workspace/delegation without a
conversational question, all 18 requirement IDs, observed check receipts, background
exits, screenshot validation and continuation after incomplete final replies.
`delegated_preview_test.go` exercises the rebuilt child registry, inherited user
authorization, shared preview manager, port publication and final chat URL.
`delivery_prompts_test.go` covers delivery versus interactive credential requests,
local versus external sites, immediate denial without events/callbacks/pending
forms, generic question and confirmation tools, confirmation-required results,
CAPTCHA blocks, hidden questions and agenda confirmations. The browser tests
cover login/OTP hints for all form operations, redirects and ambiguous origins.
`workspace_apps_test.go` covers inherited server receipts, private-address aliases,
unowned sessions and conversation isolation. `delivery_prompts_integration_test.go`
runs a real delegated implementer with a screenshot child against a fake endpoint:
both loopback and external credential requests return immediately, publish no
credential form or channel question, and retain denial and tool-result journal
entries. Journal-failure tests verify that prompts stay denied.
Existing delegation, preview lifecycle and evaluation-prefix hash tests stay active.

Run Go checks with low priority, `-p 2`, and memory-capped test scopes as specified
for the shared machine. No web sources are changed by this milestone.

Verification on 2026-11-04 (Task B3):

| Check | Result |
| --- | --- |
| Memory-capped repository-wide race suite, nested screenshot prompts and focused delivery regressions | Passed |
| Serialized engine prefix, embedded surface/profile pins and frozen skill baselines | Passed |
| `go vet -p 2 ./...` | Passed |
| `go build -p 2 ./...`, including Darwin and Windows | Passed |
| Repository formatting and bare-goroutine check | Passed |
| Staticcheck for daemon | Passed |
| Repository-wide staticcheck | Seven existing findings outside this task's scope |

The existing staticcheck findings are ST1005 diagnostics in
`internal/googleapi/tools.go` at lines 193, 216 and 218 (five findings), and unused
functions in `internal/robotlab/requirements_nondarwin.go` at lines 9 and 12.
Neither file differs from the starting revision. No web changes or paid calls
were needed for these checks; the director's real client-spec scenario is not
claimed as executed here.

Verification on 2026-11-04 (Task B4):

| Check | Result |
| --- | --- |
| Focused daemon/skills race suite: delivery, handoff, isolated storage, clean seed, dashboard coherence and frozen skill baselines | Passed |
| Missing dashboard evidence blocks the complete-build gate | Passed |
| Engine serialized/startup/twenty-turn prefix, context prefix, evaluation surface/profile hashes | Passed |
| `go vet -p 2 ./...` | Passed |
| `go build -p 2 ./...`, including Darwin and Windows | Passed |
| Changed Go files formatted, whitespace and bare-goroutine checks | Passed |
| Current UI skill validation | Passed |
| Frozen evaluation path diff against `b0dda17e` | Empty |

Race tests used fake reports/receipts and temporary workspaces, under
`systemd-run --user --scope -q -p MemoryMax=6G`, `nice -n 19` and `-p 2`.
Vet and builds also used low priority and package parallelism two. No paid calls,
live delegated builds or delivered application databases were used.
