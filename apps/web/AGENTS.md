# AGENTS.md: web interface

Instructions for coding agents working in `apps/web`. The root `AGENTS.md`
still applies. In particular, everything is written in English: code,
identifiers, comments, commit messages and every Markdown file.

## Scope

- Work only inside `apps/web`. Do not modify Go code, `internal/`,
  `cmd/`, `packages/contracts`, the gateway or any connector. If the web
  client needs something the API does not offer, stop and report it.
- The client talks to the daemon only through the gateway API: JSON-RPC
  2.0 on `/rpc`, typed events on `/ws` (subscribe with a cursor and resume
  after reconnecting), blobs on `/blobs`. Method and event names come from
  `packages/contracts/methods.json` and `events.json`; never hard-code a
  name that is not listed there.
- Web only for now. No desktop or mobile packaging.

## Stack and conventions

- React 19 with function components and hooks. TypeScript strict; no
  `any` unless a comment explains why.
- Routing: TanStack Router file routes in `src/routes`. Commit the
  regenerated `src/routeTree.gen.ts` when routes change.
- Server data: TanStack Query on top of the JSON-RPC client in `src/lib`.
  Client and live event state: Zustand stores.
- Styling: Tailwind CSS v4 utilities that reference the semantic tokens
  in `src/index.css` (`bg-background`, `text-muted-foreground`, ...).
  Never use raw hex colors or Tailwind palette colors in components, so
  palettes and light/dark themes stay swappable.
- Components: shadcn/ui (`pnpm dlx shadcn@latest add <name>`) in
  `src/components/ui`, Lucide icons, `cn()` from `src/lib/utils.ts`.
- Feature code lives in `src/features/<area>/`, with its components,
  hooks, stores and tests together.
- Every user-visible string goes through i18next, with English and
  Spanish entries.
- Long lists (files, messages, tasks) are virtualized.
- Accessibility: semantic elements, keyboard support, visible focus,
  WCAG AA contrast.
- Heavy libraries are added only when their feature is built: Monaco
  (code editor), Tiptap (documents), Univer (spreadsheets), React Flow
  (org chart and flows), PixiJS (2D world), ECharts (metrics).

## Verification

Run from `apps/web` before reporting work as done:

```sh
pnpm check       # typecheck, lint, format check, unit tests
pnpm build
pnpm test:e2e    # when UI behavior changed
```

New behavior needs tests: unit tests next to the code
(`*.test.ts(x)`) and Playwright tests in `e2e/` for user flows.

Node.js lives in `~/.local/opt/node/bin` on the development machine.
Prepend it to `PATH` if `node` is not found.
