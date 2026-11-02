# Excalibur public site

The landing page and documentation for Excalibur, built with Astro and
Starlight. The output is a static site that can be hosted on any static
host (Cloudflare Pages, Vercel, Netlify).

## Stack

- Astro with the Starlight documentation theme (search, sidebar, light and
  dark themes, English and Spanish)
- Tailwind CSS v4 through `@astrojs/starlight-tailwind`
- React for interactive islands on the landing page
- Sitemap generation and the Inter font

## Development

From the repository root, `pnpm install` installs every workspace package.
Then, from `apps/site`:

```sh
pnpm dev       # http://localhost:4321
pnpm build     # static output in dist/
pnpm preview   # serve the build
pnpm check     # type checks and formatting
```

## Routes

| Path           | Source                      |
| -------------- | --------------------------- |
| `/`            | `src/pages/index.astro`     |
| `/docs/...`    | `src/content/docs/docs/`    |
| `/es/docs/...` | `src/content/docs/es/docs/` |
