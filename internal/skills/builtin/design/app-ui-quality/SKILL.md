---
name: app-ui-quality
description: Make a web app or business system UI look designed and polished, not generic; review screenshots before finishing.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - browser_snapshot
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
3. **Lay out like an operational app.** Left sidebar (logo or company name, grouped navigation with simple line icons such as lucide), a top bar with page title, search and user menu, and content in a max-width container with generous padding. On phones (390 px) the sidebar collapses into a drawer and tables become cards; never cause horizontal page scroll.
4. **Icons from a real icon set only.** Use `lucide-react` (React) or the `lucide` package / inline SVG; never Unicode symbols (◫ ▦ ♙ ↗), emoji or icon fonts that may be missing, which render as empty boxes on many systems.
5. **Build a small component set and reuse it:** button (primary, secondary, ghost, danger), input / select with labels and inline errors, badge, card, data table, modal or drawer, toast, empty state, skeleton. Every page uses these, never ad-hoc styles.
6. **Make data screens excellent:**
   - Tables: sticky header, left-aligned text and right-aligned numbers, money formatted with the currency (`S/ 1,234.50`), zebra or hover rows, status as small badges, row actions grouped at the end, search and filters above, pagination or "load more" below, and a friendly empty state with a call to action.
   - Forms: one column on narrow screens and two on wide ones, labels above inputs, helper text, validation messages under the field, a primary action that states the result ("Registrar venta"), totals summarised in a side card (subtotal, IGV, total).
   - Dashboard: 3–4 KPI cards with a label, a big tabular number and a small comparison; one main chart; ranked lists (top products) and alert lists (low stock) with badges.
7. **Charts must look designed:** use a chart library already in the stack (Recharts, Chart.js, ECharts) or hand-made SVG; accent color for the main series and grays for the rest, light gridlines, no 3D, no rainbow palettes, readable axis labels, tooltips with formatted values, rounded bar tops, and an empty state when there is no data.
8. **Finish states and motion:** loading skeletons, disabled and busy buttons, success toasts after saves, confirmation before deletes, focus rings for keyboard users, and short transitions (150–200 ms) that respect `prefers-reduced-motion`.
9. **Use realistic content.** Seed data with plausible local names, products, prices and dates so every screen looks alive, including transaction history (e.g. sales and purchases spread over the last 30 days) so KPIs and charts are not zero on first start; never leave "Lorem ipsum", "Test 1" or empty dashboards.
10. **Review with screenshots before you finish.** Start the app and capture every main screen at 1440×900 and 390×844 (use the browser tools if available, otherwise headless Chrome, e.g. `google-chrome --headless=new --screenshot=out.png --window-size=1440,900 URL`, or a Playwright script). Look at each image and fix what you see: misalignment, crowded areas, inconsistent spacing, weak contrast, overflowing text, unstyled native controls, broken mobile layout. Repeat until every screen passes the checklist below.

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
- The dashboard shows non-zero KPIs and a populated chart on a fresh start.
- Money and quantities are right-aligned, formatted and tabular.
- Every list has search, an empty state and consistent row actions; every form shows inline validation and a clear primary action.
- No horizontal scroll at 390 px; keyboard focus is visible; contrast is WCAG AA.
- Mention in the final report the visual direction you chose and where the screenshots are.
