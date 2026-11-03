# Settings

`/settings?section=general|profile|agents|usage|skills|passwords` uses the typed JSON-RPC
client and TanStack Query. Language and theme preferences are local to this
browser; profile information remains read-only. The sidebar Settings and
Metrics entries open this route.

Usage requests `usage.summary` with UTC bounds for 24 hours, 7 days or 30 days.
ECharts is loaded only when nonempty chart data exists. A data table provides
an accessible alternative. Cards, chart, model/tool breakdowns and recent runs
use server values, with explicit empty and retry states.

The catalog uses the INT-2 `skills.groups`, `skills.list`, `skills.get`,
`skills.update`, `skills.set_state` and `skills.delete` contract. Group names
follow the UI language. Filtering is local over the returned catalog and rows
are virtualized. The existing Markdown renderer and lazy Monaco module power
skill details and editing. Mutations roll back optimistic cache updates on
failure. Invalid-parameter diagnostics remain alongside the unsaved editor.
Builtin edits explain the user override; builtins offer disable instead of
delete. Other deletions require confirmation.

Verification: `catalog.test.ts` covers grouping and combined filters;
`e2e/settings.spec.ts` mocks RPC and exercises persistence, usage, filtering,
Markdown, editing/diagnostics, builtin overrides/disable, deletion, rollback,
empty/offline states and a 390px viewport. No production data is mocked.
The optional search/browser settings section is omitted.

Shared analytics and catalog rules live in `src/styles/redesign/settings.css`.
`features/settings/modern.css` provides the scoped Settings layout and controls
using the app's semantic blue palette, surfaces, radii and motion tokens.
Desktop uses a section sidebar; mobile uses horizontally scrollable pills.
Both layouts retain the animated indicator. Arrow keys, Home and End move
navigation focus, and Enter opens the focused section. The sections array
remains the hook for adding account connections; `.settings-card` and
`.settings-connection` provide shared styling for connection cards.
Artifact auto-open and automatic time zone detection use labeled switches.
Profile reserves space for its loading skeleton, and Passwords renders metadata
skeletons while loading. English and Spanish descriptions accompany controls.
Language, appearance and usage ranges use keyboard-accessible segmented buttons.
The navigation indicator slides between sections. Usage renders stacked token
bars, a model donut, tool proportions and an ink cost card; ECharts reads CSS
chart tokens and observes theme and reduced-motion changes. Empty chart
silhouettes are decorative and never represent activity. KPI count-ups and
entrances stop under reduced motion.

Catalog category chips include counts. Its active-skills summary reflects the
whole catalog while search, source, state and category filters narrow the cards.
Virtual rows contain two cards on desktop and one on smaller screens. Action
menus are available on hover, keyboard focus and touch; direct edit/delete
preserves diagnostics, override warnings and confirmation. Browser tests also
capture light screenshots at 1440×900 and 390×844 and verify dark chart updates.

`Settings.test.tsx` covers switch semantics, keyboard operation, preference
persistence, translated labels and profile loading. Playwright checks every
section at 1440 × 900 and 390 × 844 in both themes, page overflow, mobile and
desktop indicator alignment, active text contrast, reduced motion and stable
profile loading height. Before/after audit screenshots live outside git in the
settingsui status notes directory.
