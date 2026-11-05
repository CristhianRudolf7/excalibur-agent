# Excalibur public site

A static Astro 7 site for https://excalibur.crehlabs.com. The English landing
uses live HTML, inline SVG, and locally bundled GSAP/ScrollTrigger. It has no
hydrated framework islands or raster artwork. Starlight 0.42 provides the
English and Spanish manual, native navigation, and Pagefind search.

Shared tokens and Latin-only, self-hosted Inter and JetBrains Mono fonts give
both interfaces the same identity. The `starlight-theme` preference follows
the system until a visitor chooses a theme. All content remains available
without JavaScript; reduced motion disables scrubbing and pinning.

## Development and verification

From the repository root, install workspace dependencies with `pnpm install`.
Use the pinned local Node toolchain, then run from `apps/site`:

```sh
export PATH=$HOME/.local/opt/node/bin:$PATH
nice -n 10 pnpm check
nice -n 10 pnpm build
SITE_PORT=4336 nice -n 10 pnpm test
nice -n 10 node --test tests/content/links.test.mjs
nice -n 10 pnpm budgets
SITE_PORT=4336 SITE_THEME=light nice -n 10 pnpm perf
SITE_PORT=4336 SITE_THEME=dark nice -n 10 pnpm perf
```

Browser checks use installed Google Chrome; do not download browsers. They
serve production output on `127.0.0.1:4336` and stop only a preview they start.
If the port serves a different build, the harness warns and routes this
checkout's `dist` files inside Chrome at that same origin. It leaves the
existing process alone. This mode also supports `SITE_FROM_DISK=1`.

`tests/docs/verify.ts` builds a temporary published style fixture, verifies it,
and restores the normal production build. Run it separately from other
browser checks because rebuilding deletes old chunks. `tests/content/browser.mjs`
checks all 44 articles across both themes and seven widths. The final screenshot
and optional recording runner is `tests/final/capture.ts` (`QA_VIDEO=1`).
Landing JavaScript budgets include the inline pre-paint script as well as every
reachable lazy chunk. Desktop stages contain paint and skip native off-screen
rendering; the sticky machine retains its complete native layout, with a separate rail
through the full reserved scroll journey.

Reports are generated in ignored `test-results/`; audit screenshots are saved
outside the repository. Performance reports disclose headless software rendering.

For interactive development: `pnpm dev --host 127.0.0.1 --port 4336`.

## Routes and content

| Path         | Source                                          |
| ------------ | ----------------------------------------------- |
| `/`          | `src/pages/index.astro`                         |
| `/#install`  | `src/components/landing/sections/Install.astro` |
| `/docs/…`    | `src/content/docs/docs/`                        |
| `/es/docs/…` | `src/content/docs/es/docs/`                     |

The manual has six shared sidebar groups and 22 translated guides per locale.
Its home plates come from the resolved sidebar. Sample landing documents,
amounts, previews, and schedules are labeled; blueprint millimeters and clocks
are decorative. The benchmark repeats the owner-approved published figures.

`vercel.json` configures short plain-text caching for `/install.sh` and
`/install.ps1`, immutable caching for hashed assets, and revalidation for
HTML and the search index. The release branch supplies the two installer
files in `public/`; this site does not synthesize installers or verify remote
release artifacts. No deployment or push is part of local site verification.
