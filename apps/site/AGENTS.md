# AGENTS.md: public site

Instructions for coding agents working in `apps/site`, the public landing
page and documentation. The root `AGENTS.md` still applies: code,
identifiers, comments and commit messages are in English.

## Scope

- Work only inside `apps/site`. Do not modify Go code, `apps/web`,
  `packages/contracts` or any connector.
- The site is fully static. It never calls the daemon and needs no login.
- The architecture document is never copied, quoted or summarized here.
  Public documentation describes what Excalibur does for its users, in a
  natural product voice. It does not describe how the project is
  developed, evaluated or tuned. The owner-approved landing may show the
  Terminal-Bench result and the high-level parts diagram.
- Document only what exists. Planned features are labeled as planned.

## Structure

- `src/pages/index.astro`: landing page at `/`.
- `src/content/docs/docs/`: English documentation at `/docs`.
- `src/content/docs/es/docs/`: Spanish documentation at `/es/docs`.
  This locale is the one exception to the English-only Markdown rule,
  because the public site is bilingual. Every English page should have a
  Spanish counterpart with the same path.
- `src/components/`: Astro components for the landing. Its behavior uses
  TypeScript modules; it has no React islands.
- `src/styles/global.css`: Tailwind CSS v4 plus Starlight integration and
  brand tokens. Components use tokens, never raw hex colors.
- Documentation images go through `astro:assets`. Landing artwork is inline SVG.

## Verification

Run from `apps/site` before reporting work as done:

```sh
pnpm check   # astro check (TypeScript 6) and Prettier
pnpm build   # static build, search index and sitemap
```

Use production preview on the assigned port for browser verification. See the
Landing engine commands and PID ownership rules below.

References: https://docs.astro.build and https://starlight.astro.build.

Node.js lives in `~/.local/opt/node/bin` on the development machine.
Prepend it to `PATH` if `node` is not found.

## Landing engine

The landing is built with Astro components and small TypeScript modules. It has
no hydrated framework components. GSAP and ScrollTrigger are bundled locally.
Use native scrolling; only `Machine` may pin (maximum 200 viewport heights).
Do not add runtime dependencies, smooth scrolling, canvas, raster artwork, or
another animation library.

### Stage coordinates and the rail

`components/landing/Stage.astro` accepts `id`, `height` (default `1024`), `class`
and `label`. At widths of 1024px or more its canvas is 1536 units wide, centered,
and limited to 1536 CSS pixels. Position HTML with `class="canvas-item"` and
`style="--x:60;--y:200;--w:1300"`. Set headline `--size` in canvas units.
The `.stage` container maps positions and type through `cqw`. Below 1024px,
write a normal stacked layout in the section's own CSS; canvas positioning is
inactive. Decorative SVG lives in the same coordinate system.

`parts/SectionLine.astro` accepts `path`, `height`, `mobilePath`, `mobileHeight`.
Optional `mobileTickX/Y` set a safe static clock position for phone fallbacks.
It renders both responsive SVGs, the normalized `pathLength="1"` main paths,
eight preview dots, pen and timestamp. Named `desktop` and `mobile` slots accept
secondary SVG artwork. All line drawings use tokens and non-scaling strokes. The engine compensates
dash lengths for the SVG screen transform at refresh; do not override its dash
styles or the painted endpoint will diverge from the pen on scaled canvases.
Every desktop path must start at `(218, 0)` and end at `(218, height)`.
Hero starts in the sword instead. The final Extensions implementation ends in
the closing sword. Keep Stages flush: no margins or borders that break the rail.
For custom SVG use `.desktop-line` / `.mobile-line`, `[data-line-path]`,
`[data-pen]`, eight `[data-preview]` circles and `TimeTick`.

### Motion API

Every section exports exactly `init(section: HTMLElement)` from its owned script,
returning a cleanup function. The registry in `scripts/landing.ts` discovers
`scripts/sections/*.ts` automatically by anchor name, imports them within one
viewport, and cleans up on page exit. No registry edit is needed.

```ts
import { createSectionLine } from '../line'
import { reveal, drawPath, steppedHop, countUp } from '../line/helpers'

export function init(section: HTMLElement) {
  const line = createSectionLine(section, {
    start: 'top 50%',
    end: 'bottom 50%',
  })
  line.onReach(0.42, () => reveal(section.querySelector('[data-reveal]')))
  return () => line.destroy()
}
```

`onReach` accepts a paused GSAP animation or a factory returning one. Prefer
factories: the engine invokes them when the pen reaches the fraction,
plays forward, and reverses when scrolling back. A fraction may also be
`{ desktop: 0.42, phone: 0.31 }` when the responsive paths differ. Keep factory queries scoped to
the section and the active responsive SVG. The helpers return paused animations:
`reveal(target, vars?)` changes opacity; `drawPath(paths)` normalizes secondary
paths and draws their dash offset; `steppedHop(highlight, targets)` moves a
highlight by transforms; `countUp(element, value, decimals?, suffix?)` updates
only changed digits. Use tabular mono digits. Never animate layout properties.

Factories are recreated on refresh with their playhead and direction preserved,
so helper measurements stay current without frame-time reads.
The engine owns the section's one scroll timeline (`line.timeline`), lookup table
of 361 path points (built only at init/refresh), timestamp clock, responsive GSAP
contexts, 0.25 scrub and cleanup. No per-frame DOM measurements. `onReach`
animations pause outside the viewport. Pen and preview are hidden at path
boundaries, so adjacent sections never show duplicate pens. Native scroll
interrupts the hero load drawing and hands control to the scroll timeline. Use `pin: true, pinDistance: 150` only in
Machine; distances are viewport percentages, capped at 200. Reduced motion has
a complete final state and no ScrollTrigger or pin. The hero uses `origin: '.wordmark-sword'` to join the actual sword tip on
refresh, including phone widths between design canvases. This adapts the first
curve only; the remaining path geometry stays in canvas units.
The hero alone uses `introStops` for successive mechanical passes,
`intro: true` and the SectionLine `introX/Y` and `mobileIntroX/Y` target points.
The timestamp is decorative page progress multiplied by 37.7 seconds, never a
performance measurement.

Static HTML must show complete content. Use `.anim [data-reveal]` for initial
content visibility. A pre-paint script sets `.anim` and the shared `starlight-theme`
preference; a three-second failsafe restores content if enhancement fails.
Headlines are always visible. `data-theme` is shared with the documentation.
White on `--blue-fill` has 4.4988:1 contrast: use it only for large text (at
least 24px normal or 18.67px bold); `IsoPlate` uses bold 30-unit active labels (20px at the minimum desktop
scale); other labels are 22 units. Preserve the minimum rendered label size in
phone compositions. Use
`--blue-text` for small blue text. Keep all colors in `tokens.css`.

### Shared parts

- `Headline`: `lines: string[]`, `as` (`h2` default), `class`, `style`; live text
  with the approved type treatment. Only Hero uses `h1`.
- `SectionIndex`: numeric `index` and optional `style`.
- `Leader`: `number`, `text`, optional `width`, `height`, SVG `path`, `dotX/Y`,
  `class`; note text is HTML and the leader is decorative SVG.
- `IsoPlate`: `width` (front edge), `depth` (45-degree x/y projection),
  `thickness` (vertical edge), `label`, `labelPosition` (`front` default or
  `top`), `active`, `class`. ViewBox is
  `(width + depth) × (depth + thickness)`; top and side faces are exact polygons.
  Labels are accessible through the SVG name. Slot artwork is in plate units.
  Blueprint millimeter annotations must be derived from actual transforms.
- `Fragment`: `label`, `class`, `style`, default HTML slot; square 1px border.
- `BrowserFrame`: `address`, `class`, `style`, default HTML slot.
- `TimeTick`: SVG group accepting `x`, `y`, initial `time`; the engine moves it
  and updates its clock only when the tenth changes.
- `brand/Logo`: optional `swordOnly`; the original 6 × 16 pixel sword grid.
- `Quadruped`: hand-drawn decorative SVG group, optional `transform`, `class`.

### Integrated sections

All sections are implemented. Ownership boundaries from the parallel build no
longer apply. Reusable artwork lives in `components/landing/parts/` and rail
geometry in `scripts/line/geometry.ts`; do not create worker-specific copies.
`ScoreRuler` shares the exact linear 0–100 scale between Machine and Measured.

The order is Hero, YourDay, Memory, Data, Code, Work, Machine, Measured, Install,
and Extensions. The eight feature sections use `/ 08`; Machine is an unnumbered
blueprint interlude. Install's commands and platform controls are HTML, with
all commands available without JavaScript. Its small module adds OS selection,
keyboard tabs, and clipboard feedback.

The line engine builds a monotone schedule from its cached 361 path samples.
Backtracking excursions are drawn while they fit in the viewport. The native
sticky Machine reserves 150vh in CSS. Its straight rail belongs to the whole
Stage, outside the sticky canvas, and stretches through the reserved journey.
A cached lead keeps the pen above the blueprint headline during assembly.
The shared indicator retains a join endpoint until the next section claims it,
including an exact-boundary anchor jump. Neither geometry nor viewport measurements run in the frame callback.
Keep the live contexts on a persisted `pagehide` and refresh on `pageshow`.
Stage paint is contained. Desktop stages except the sticky Machine use native
`content-visibility: auto`; their fixed aspect ratios reserve the complete
height. Phones retain normal rendering so stacked content never changes height.

### Browser verification

Use Node 22 / pnpm from `~/.local/opt/node/bin`. Run heavy commands with
`nice -n 10`. Browser tools use installed Google Chrome, never download one.
All tests serve the production `dist/` through Astro preview on
`127.0.0.1:4336`. `tests/server.ts` records its own preview PID and stops only
that process; it can reuse an already running preview. Later workers must use
their assigned port through `SITE_PORT=4336 pnpm shots ...` (and the same override
for tests/perf). Do not share another worker's port or stop their server.

```sh
pnpm check
pnpm build
pnpm test
pnpm budgets
pnpm shots -- --section hero --out /absolute/path
pnpm shots -- --section memory --widths 390,1536 --theme dark --out /absolute/path
pnpm perf
```

Screenshots capture both the whole section and its viewport. The default widths
are 1280, 1536, 1920 and 390, in both themes. Hero also captures a real mid-draw
frame. Other sections use reduced motion for final-state art direction.
Use `--motion reduce` or `--no-js` to capture the hero's fallback states.
Open the PNGs and compare them with the concepts; test success alone is not
visual acceptance. `data-noverlap` marks independent HTML text blocks for the
overlap detector. Tests cover rail joins, overflow, fonts, runtime errors,
contrast, themes, keyboard navigation, no-JS and reduced motion. Budgets follow
only assets reachable from the landing (including all lazy chunks), not docs
bundles. Performance reports are in `test-results/`, include rAF p50/p95 at 4×
CPU throttling, longest task, CLS and DOM count, and state the headless software
rendering limitation. Measure the complete integrated landing in both themes.

When an earlier session owns the assigned port, `startPreview` checks the HTML
against this checkout's `dist/index.html`. If it differs, the Chrome harness
serves the current production files through request routing on the same origin.
It never stops that foreign process or reads its worktree. `SITE_FROM_DISK=1`
forces this mode. The warning and final report must disclose it. The setting
is test-only and ships no runtime code. Run build-changing fixture checks
sequentially; never rebuild `dist` while another browser audit is using it.
