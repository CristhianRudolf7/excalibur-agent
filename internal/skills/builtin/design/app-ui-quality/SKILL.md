---
name: app-ui-quality
description: Make a web app or business system UI look designed and polished, not generic; review screenshots before finishing.
metadata:
  excalibur:
    group: design
    version: 1.3.0
    platforms:
    - linux
    tools:
    - exec
    - browser_snapshot
    - inspect_image
    display:
      name_en: App UI quality
      name_es: Calidad visual de aplicaciones
      desc_en: Make a web app or business system UI look designed and polished, not generic.
      desc_es: Hacer que la interfaz de una aplicación o sistema se vea diseñada y pulida.
---
# App UI quality

Use this skill whenever you build or substantially change the user interface of a web application, admin panel, dashboard or business system (ERP, inventory, sales, bookings, CRM). Working features with a careless interface read as unfinished. The goal is an interface a real client would be proud to show: calm, consistent, readable and clearly designed for this product, without the generic "AI template" look.

## When to use

- The request asks to build a web app, system, panel, dashboard or site, from a brief, TDR or spec.
- You are about to write the first screen, layout or component of a frontend.
- An existing app works but looks plain, crowded or generic.

## Procedure

1. **Pick a direction before coding.** Write three lines in your plan: the product's tone (e.g. "calm, trustworthy, operational"), one accent color tied to the client or domain, and the type pair. Avoid the defaults every generator produces: purple-to-blue gradients, glowing glassmorphism cards, neon on dark, emoji icons, rainbow badges, centered hero text on an admin tool.
2. **Set design tokens first** (CSS variables or a theme file) and use only them:
   - Palette: neutral background and surfaces (near-white `#f7f8f6`-like and pure white cards, or a soft dark theme), text in two strengths (primary near-black, secondary gray), borders one step darker than surfaces, ONE brand accent plus semantic success / warning / danger used only for status.
   - Type: one well-made sans family (Inter, Manrope, IBM Plex Sans, Source Sans 3 via a local or CDN font) with a real scale, e.g. 12 / 14 / 16 / 20 / 24 / 32 px; weights 400, 500, 600 only; tabular numbers (`font-variant-numeric: tabular-nums`) for money and quantities.
   - Spacing on a 4 px grid (4, 8, 12, 16, 24, 32, 48); radius 6–10 px, consistent everywhere; one subtle shadow level for raised surfaces.
3. **Lay out like an operational app.** Left sidebar (logo or company name, grouped navigation with simple line icons such as lucide), a top bar with page title, search and user menu, and content in a max-width container with generous padding. On phones (390 px) the sidebar collapses into a drawer and tables become cards; never cause horizontal page scroll. The desktop shell must stretch to the full viewport and full document height: use a grid/flex shell with `min-height: 100dvh` and a stretching sidebar, including on long lists.
4. **Icons from a real icon set only.** Use `lucide-react` (React) or the `lucide` package / inline SVG; never Unicode symbols (◫ ▦ ♙ ↗), emoji or icon fonts that may be missing, which render as empty boxes on many systems.
5. **Build a small component set and reuse it:** button (primary, secondary, ghost, danger), input / select with labels and inline errors, badge, card, data table, modal or drawer, toast, empty state, skeleton. Every page uses these, never ad-hoc styles.
6. **Make data screens excellent:**
   - Tables: sticky header, left-aligned text and right-aligned numbers, money formatted with the currency (`S/ 1,234.50`), zebra or hover rows, status as small badges, row actions grouped at the end, search and filters above, pagination or "load more" below, and a friendly empty state with a call to action.
   - Forms: one column on narrow screens and two on wide ones, labels above inputs, helper text, validation messages under the field, a primary action that states the result ("Registrar venta"), totals summarised in a side card (subtotal, IGV, total).
   - Dashboard: 3–4 KPI cards with a label, a big tabular number and a small comparison; one main chart; ranked lists (top products) and alert lists (low stock) with badges.
7. **Charts must look designed:** use a chart library already in the stack (Recharts, Chart.js, ECharts) or hand-made SVG; accent color for the main series and grays for the rest, light gridlines, no 3D, no rainbow palettes, readable axis labels, tooltips with formatted values, rounded bar tops, and an empty state when there is no data.
8. **Finish states and motion:** loading skeletons, disabled and busy buttons, success toasts after saves, confirmation before deletes, focus rings for keyboard users, and short transitions (150–200 ms) that respect `prefers-reduced-motion`.
9. **Use realistic content.** Seed data with plausible local names, products, prices and dates so every screen looks alive, including transaction history (e.g. sales and purchases spread over the last 30 days) so KPIs and charts are not zero on first start; never leave "Lorem ipsum", "Test 1" or empty dashboards. Use plausible business and person names, never numbered placeholders such as "Customer Demo 1". Products must belong to coherent categories (canned tuna belongs to canned food, not beverages). Use realistic local identifier formats where applicable (RUC: 11 digits; DNI: 8 digits) and document that identifiers are synthetic demo data. Seed opening stock, purchases, sales and movements as a consistent ledger; calculate on-hand stock from that ledger. Calculate dashboard KPIs and charts from those same persisted transactions and date/status filters, using integer minor currency units or exact decimals. Never hardcode dashboard totals. Run a test that independently recomputes dashboard sales, purchases, stock and chart totals from the database and compares the API results, including discounts, tax and cancelled transactions where supported.
10. **Review with screenshots before you finish.** Start the app and capture every main screen at 1440×900 and 390×844 (use the browser tools if available, otherwise headless Chrome, e.g. `google-chrome --headless=new --screenshot=out.png --window-size=1440,900 URL`, or a Playwright script). Inspect every saved image with `inspect_image` (and browser screenshots when available) and fix what you see: misalignment, crowded areas, inconsistent spacing, weak contrast, overflowing text, unstyled native controls, broken mobile layout. Repeat until every screen passes the checklist below.

## Full-spec delivery evidence

For an approved client specification, proceed without clarification or confirmation: choose reasonable defaults and record them as assumptions in the README. Never drop modules because an internal budget feels short. Only an explicit user time/cost limit permits changing delivery scope or pacing.

Create a requirement matrix before coding: every source requirement number, implemented feature/files, and an executed API test or browser check. Cover login, inventory, sales, purchases, suppliers, customers, stock movements, dashboard, demo data, tests and README wherever required. A menu link, empty screen, mocked total or login/dashboard skeleton is not module completion. Review the matrix against every page of the source before finishing.

For autonomous daemon deliveries, follow the `delivery-evidence.json` contract supplied in the delegation card. Execute the referenced checks to exit code zero, inspect each screenshot with `inspect_image` so the daemon records an inspection receipt, and call `delivery_check` to resolve remaining gaps. Keep evidence current after fixes. Include both list and form screenshots for each required module, with keyboard focus, numeric alignment, icon rendering, sidebar height and mobile overflow explicitly reviewed. Publish only when the user explicitly requested a public link; that request authorizes port mode as well as static previews without a second question. Verify the public URL serves the application before reporting it.

## Clean delivery data and dashboard coherence

Run automated tests, API checks, verification scripts and mutating browser checks
against isolated storage: a temporary file, in-memory database or a separate
`DB_FILE`/`DATABASE_URL`. Never point verification at the delivered app's storage.
After the final source edit and all verification, reset/reseed the delivered data
or compare its complete persisted dataset with a fresh seed. Capture final
screenshots from that clean app using read-only browsing. Rerunning verification
requires fresh clean-seed proof before publication or completion.

For daemon deliveries, include the required `data_delivery` check/report from the
delegation card: actual canonical, credential-free storage identities for every
referenced verification command and matching SHA-256 fingerprints of canonical
persisted seed and delivered data. Hash the dataset, not the seed source file;
document exclusions for nondeterministic metadata. Follow the card's `dashboard`
report schema using read-only browser assertions against the clean delivered app:

- Inventory every KPI card and section label with its DOM selector. Labels must
  be unique and specific about the measure and time scope; repeated generic
  titles make different values impossible to interpret.
- Inventory every count-based panel. When the count is zero, use a calm empty
  state and hide attention/warning styling. Check the count and actual alert/empty
  visibility; use an explicit empty inventory if no such panels exist.
- Compare every rendered date bucket and value with an independent aggregation
  from the fresh seed across the full displayed range. Seed plausible history
  over multiple populated days. Reject a single-day spike from verification
  writes, even when headline totals reconcile. Inspect desktop/mobile screenshots
  for chart distribution and labels as well as visual styling.

Recheck that the persisted delivered data still equals the seed after read-only
browser assertions. Missing or stale isolation, seed or dashboard evidence blocks
the daemon's publication and completion gate.

## Every control must work

Inventory every screen and its interactive controls from the live DOM: native
buttons, inputs, selects, textareas, links and forms, ARIA interactive roles and
contenteditable elements. Include details, dialogs, conditional controls and
states where disabled controls become enabled. Execute a browser assertion for
each discovered control against the effect described by its label.

Search must reduce visible rows for a selective query. Date and other filters
must change the result set. A notice, toast or echoed input does not prove the
control works. Exercise validation and submissions, read saved records back after
reload, and open detail views using the actual endpoint response shape. Multiple
items imply variable-length forms: add beyond the initial rows, remove rows,
submit and verify persisted items. A fixed set of item slots is insufficient.

For daemon delivery, add `ui` with the exact executed browser `check` command and
JSON `report` path to `delivery-evidence.json`. Generate the report in that run,
using playwright, puppeteer or webdriver. Follow the delegation card's schema:
each screen records its URL, discovered count and control inventory; each check
references a control and records action, target, effect, observed before/after
state and `passed`. Declare multiple-item form controls `repeatable:true`, with
`add-item` and `remove-item` controls referencing their form ID. Every discovered
control needs an executed, successful state assertion; every reviewed screen needs
an inventory. Rerun after code or report changes. Screenshot review alone cannot
prove functionality.

## Maintainable source

Write source for the person who will maintain it: descriptive state and function
names, normal formatting, small functions, and clear responsibilities. Separate
backend routes, data access and seed setup; split frontend screens into components.
Keep hand-written source files at most 600 lines. Outside literals and literal data
tables, keep lines at most 200 characters, average code line length at most 100,
and at most two top-level statements per line. Include styles and tests.

Use the project formatter and apply its output. If none is configured, use an
offline standard formatter such as prettier, gofmt or black. Never hand-minify code.
For daemon delivery, record successful formatter commands and their covered files
in the `formatters` array of `delivery-evidence.json`. Reformat after every edit
and resolve every `file:line` readability refusal before finishing.

## Durable identifiers

Allocate document numbers from a database sequence or `MAX(existing_number) + 1`
inside the transaction that inserts the record. Never use `count+1` or collection
length: deletions leave gaps and can produce duplicates. Use database-generated
primary keys for internal IDs. With `MAX+1`, serialize allocation using suitable
locking/isolation or retry the whole transaction after a uniqueness conflict.
Keep allocation and insertion atomic, verify rollback, and test creation after
deletion and concurrent creation. Use a durable sequence if numbers must never
be reused. Document any chosen numbering policy.

## Pitfalls

- Shipping default browser styles (unstyled tables, raw `<select>`, blue underlined links) because the logic took all the time. Reserve time for the interface from the start.
- Too many colors, font sizes or radii; inconsistency reads as low quality faster than plainness does.
- Gradients, glows and oversized hero sections on an internal tool; operational apps should feel calm and dense, not like a landing page.
- Centered narrow forms floating on empty pages; align content to the layout grid.
- Charts with default library colors and no number formatting.
- Forgetting the 390 px layout until the end.
- Delivering only part of the brief: every module in the spec needs its screens (list, form, detail) with the same quality; check the requirement list before finishing.

## Verification

- Screenshots of login, dashboard, every list page and every form exist at desktop and phone sizes, and you looked at them.
- Only design tokens are used for color, spacing, radius and type; one accent color.
- Icons come from one icon set; no Unicode-symbol or emoji icons, no empty "tofu" boxes in screenshots.
- The dashboard shows non-zero KPIs and a populated chart on a fresh start; an executed database test independently reconciles totals and stock with seeded transactions.
- Verification uses isolated storage; the delivered dataset equals the clean seed after all checks, with executed `data_delivery` evidence.
- KPI and section labels are unique and specific; zero-count panels show calm empty states with no warning.
- Every chart matches the full seeded history across multiple populated days; final screenshots contain no verification spike.
- Demo names are plausible, categories are coherent, and local identifier formats pass an executed check.
- Every source requirement maps to working code and an executed API/browser check; no module is represented only by navigation.
- The sidebar reaches the bottom on short and long desktop pages; screenshots confirm this.
- Money and quantities are right-aligned, formatted and tabular.
- Every list has search, an empty state and consistent row actions; every form shows inline validation and a clear primary action.
- No horizontal scroll at 390 px; keyboard focus is visible; contrast is WCAG AA.
- Mention in the final report the visual direction you chose and where the screenshots are.
