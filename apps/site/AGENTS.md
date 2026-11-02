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
  developed, evaluated or tuned.
- Document only what exists. Planned features are labeled as planned.

## Structure

- `src/pages/index.astro`: landing page at `/`.
- `src/content/docs/docs/`: English documentation at `/docs`.
- `src/content/docs/es/docs/`: Spanish documentation at `/es/docs`.
  This locale is the one exception to the English-only Markdown rule,
  because the public site is bilingual. Every English page should have a
  Spanish counterpart with the same path.
- `src/components/`: Astro and React components for the landing page.
  Use React only where interaction is needed; everything else is static
  Astro.
- `src/styles/global.css`: Tailwind CSS v4 plus Starlight integration and
  brand tokens. Components use tokens, never raw hex colors.
- Images go through `astro:assets` for optimization.

## Verification

Run from `apps/site` before reporting work as done:

```sh
pnpm check   # astro check (TypeScript 6) and Prettier
pnpm build   # static build, search index and sitemap
```

Start the dev server in the background with `pnpm astro dev --background`
and manage it with `astro dev stop`, `astro dev status` and
`astro dev logs`.

References: https://docs.astro.build and https://starlight.astro.build.

Node.js lives in `~/.local/opt/node/bin` on the development machine.
Prepend it to `PATH` if `node` is not found.
