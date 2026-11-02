# Excalibur web interface

The browser client for the Excalibur daemon. The FE-01 screens currently
use typed mock accessors and local Zustand stores; no daemon is required.
Future integration will use only the gateway API in `packages/contracts`:
JSON-RPC 2.0 on `/rpc`, typed events on `/ws` and blobs on `/blobs`.

## Stack

- React 19, TypeScript (strict) and Vite
- TanStack Router (file-based routes in `src/routes`) and TanStack Query
- Zustand for client state, TanStack Virtual for long lists
- Tailwind CSS v4 with semantic design tokens in `src/index.css`
- shadcn/ui components on Radix primitives, Lucide icons, Inter font
- i18next for English and Spanish
- Vitest and Testing Library for unit tests, Playwright for browser tests

## Requirements

- Node.js 22 LTS or newer
- pnpm (enabled through `corepack enable`)

## Development

```sh
pnpm install
pnpm dev
```

Run these commands from `apps/web`. Its local pnpm workspace and lockfile
keep dependency changes within the web client.

The dev server runs on http://localhost:5173 and proxies `/rpc`, `/ws`,
`/blobs` and `/healthz` to a local daemon when needed for future API work:

```sh
excalibur daemon --listen 127.0.0.1:7787
```

Set `EXCALIBUR_DAEMON_URL` in `.env.local` to point at a different address.

## Scripts

| Script                              | Purpose                                        |
| ----------------------------------- | ---------------------------------------------- |
| `pnpm dev`                          | Start the dev server                           |
| `pnpm build`                        | Type-check and build to `dist/`                |
| `pnpm preview`                      | Serve the production build                     |
| `pnpm typecheck`                    | Run the TypeScript compiler                    |
| `pnpm lint`                         | Run oxlint                                     |
| `pnpm format` / `pnpm format:check` | Format with Prettier / verify formatting       |
| `pnpm test`                         | Run unit tests                                 |
| `pnpm test:e2e`                     | Run browser tests against the production build |
| `pnpm check`                        | Type-check, lint, format check and unit tests  |

## Layout

```text
src/
  routes/        file-based routes (routeTree.gen.ts is generated)
  features/      one folder per product area (chat, projects, tasks, ...)
  components/ui/ shared primitives (shadcn/ui)
  lib/           i18next resources and shared utilities
  test/          test setup
e2e/             Playwright tests
```

## FE-01 screens

- `/agents/excalibur?panel=tasks`: conversation, task groups and plan approval.
- `/agents/excalibur?panel=files`: virtual explorer and lazy local Monaco.
- `/agents/excalibur?panel=sheet`: editable budget with a virtualized 100 × 26 grid.
- `/projects`: six searchable projects with status and owner filters.

Spanish is the default language; the user-row language button switches to
English. Sidebar collapse and the chat's split ratio survive reloads. Desktop
splits reserve at least 360 px for chat and 320 px for the right panel, with
a 1 px separator. Below 681 px of container width, or on viewports below
1024 px, the panel opens as an overlay with a close button instead of
compressing the chat. World mode preserves the ratio and sidebar preference;
returning to Chat measures the newly mounted split before painting. Conversations,
plans, task statuses, file edits and workbook data last for the current page
session. The workbook supports formulas, ranges, TSV clipboard operations,
formatting, independent sheets and undo/redo. Its parser has no external
formula dependencies. Monaco is MIT licensed and loaded only on the Files tab.

The sheet stays in the right panel, as requested, rather than replacing the
center column in L04. Original pixel-grid avatars and the semantic Linear
palette replace the reference illustrations and brighter blue. Subagent and unimplemented navigation screens intentionally show placeholders.
The Document panel previews library files and includes samples for every
supported format when the library methods are unavailable. Set
`EXCALIBUR_DOCUMENT_VIEWER=0` at build time to disable document previews.

Browser tests cover desktop and mobile navigation, resizing, long messages,
local replies, file opening, workbook editing and project search. They use the
system Chrome installation configured in `playwright.config.ts`.

## Daemon profile connection

Only the local user's name is connected to the daemon. The sidebar profile
button shows the real name and initials and opens a read-only profile dialog.
Loading uses skeletons; disconnection uses a neutral identity and an explicit
retry. All other screens continue to use mock data.

`src/lib/rpc/` provides the typed JSON-RPC client, stable browser client ID,
10-second timeout, cancellation and distinct transport, RPC and protocol
errors. `useProfile()` caches `principal.get` under `['principal']` for five
minutes, retries transport errors once and never automatically retries server
RPC errors. Contract membership is checked against `packages/contracts`.

Run the frontend with a disposable real daemon:

```sh
export PATH="$HOME/.local/opt/node/bin:/usr/local/go/bin:$HOME/go/bin:$PATH"
EXCALIBUR_USER_NAME="Ana Prueba" pnpm dev:full
```

The daemon script builds into a temporary directory, starts the daemon on
127.0.0.1:7787, passes through EXCALIBUR_USER_NAME and removes the temporary
binary and state on shutdown. Vite retains the /rpc, /ws, /blobs and /healthz
proxy to EXCALIBUR_DAEMON_URL (default http://127.0.0.1:7787). Existing state is
never changed by this development launcher. For this profile-only workflow,
EXCALIBUR_PROVIDER=stub may be supplied to avoid using configured model providers.

`pnpm test:e2e` uses mocked profile requests and never requires Go. Run
`pnpm test:e2e:integration` separately to build a real daemon, start disposable
state with Ana Prueba, and verify the profile over the preview proxy. Its
isolated daemon and preview use ports 7788 and 4184 and stop after the tests.
The integration config explicitly selects the stub provider and makes no paid
model calls. No additional dependencies are needed for these launchers.

## Sidebar sizing and Markdown messages

The desktop sidebar has a shared `ResizeHandle` and `useResizableSize` with the
chat/right-panel split. Drag its right edge or use arrow keys (24 px steps),
Home/End (limits), and double-click (240 px default). Bounds are 200–400 px and
40% of the viewport; the drawer below 1024 px keeps its fixed 260 px width. The
icon rail in World has no handle. Collapse restores the saved custom width.
Validated widths live in `sidebar-width-v1`; invalid values reset to 240 and
legacy `sidebar-width` values are migrated. Hidden/unmeasured dimensions are
ignored, and storage failures keep usable in-memory preferences. The main split
continues to derive its panes from the saved ratio.

`src/components/markdown/Markdown.tsx` is a reusable renderer for agent messages.
User messages remain literal text. It supports GFM tables, nested and task lists,
headings, quotes, emphasis, images and fenced code. HTML is rendered as text;
URLs allow only HTTP, HTTPS and mailto (HTTP/HTTPS only for images). External
links use a new tab with noopener/noreferrer. Tables and code scroll inside
bounded containers; code has a copy action with 1.5-second feedback.

The Markdown parser loads in its own chunk. Syntax highlighting loads only
when a code block enters the viewport, using Lowlight and Highlight.js with
JavaScript, TypeScript, JSON, Bash, Diff and CSS registered. Highlighted tokens
are React text/spans rather than injected HTML. Stable renderer identities
preserve code toolbar state when a streamed Markdown string grows.

Spanish and English Markdown examples live only in browser-test fixtures. The
runtime renders daemon messages and event-driven plan approvals; it does not
seed conversations or simulate replies.

Added dependencies: react-markdown, remark-gfm and lowlight (MIT), and
highlight.js (BSD-3-Clause). The root workspace lockfile records
the additions. Browser coverage includes desktop/mobile resizing, persistence,
mode transitions, Markdown tables, internal scrolling, copy and plain user text.

## Collapsed navigation rail

On desktop, collapsing the sidebar shows the shared `SidebarRail` used by
World mode. The rail stays 64 px wide and includes navigation, Excalibur, stores, metrics, settings and the real profile avatar. Actions
have translated labels and hover/focus tooltips; the current page and selected
agent are highlighted. The top expand button and Ctrl+B restore the saved full
sidebar width. Expanding from World returns to Chat with the full sidebar;
ordinary Chat/World switching preserves the sidebar preference.

A single sidebar slot animates the width for 150 ms, with no transition under
reduced motion. Only the full desktop sidebar has a resize handle. Existing
split ratios keep both content panes correctly sized. On mobile there is no
rail in either mode: Chat uses its header drawer button, and World supplies a
matching drawer button over the canvas. The overlay drawer remains 260 px wide.

## INT-2 conversation and voice

Navigation exposes one assistant, Excalibur, and the stable conversation
`cnv_web_excalibur`. Chat preserves real history while offline, retries failed
sends with the original client message ID, and reconciles early server echoes.
There are no seeded conversations or simulated replies. World helpers remain
visualization entities; the world message bar sends to the same conversation.
Questions, plans, tool activity and notices come from the subscribed events.

The Sources chip opens a keyboard-trapped modal containing the answer's actual
web and library references. Web refs do not need a numbered citation. The modal
shows available read timestamps, document locations and library excerpts, with
external links opening in another tab.

The header call action checks `voice.status` before requesting a microphone.
Energy VAD records one segment after speech and 700 ms of silence (constructor
configurable, with a 30-second segment limit). Each segment uses `POST /blobs`,
`voice.transcribe` and `conversation.send`; assistant replies use sequential
`voice.speak` requests split at sentence boundaries. Speaking again interrupts
playback. Hangup and unmount stop tracks, recorders, playback, timers and pending
voice requests. The call includes mute, speaker, a microphone waveform and a
rolling transcript. Browser microphone permission and the contract's voice
backend are required; unavailable services produce an explicit state.

For concurrent worktrees, set `EXCALIBUR_WEB_TEST_PORT=4185` when running
Playwright. The default remains 4183. Browser fixtures intercept RPC and sockets,
and voice tests use Chromium fake media with controlled analyser energy and
mocked blobs; they never call model APIs. Unit-test workers are limited to two
and test deadlines allow for the shared development host.

The original named regression set reproduced seven failures across twelve tests:
profile attempt counting included unrelated chat requests; card fixtures vanished
when real history replaced previews; two wrapping tests reused daemon history and
also exposed an optimistic-echo race; two Markdown overflow/copy tests and the
code/budget Markdown test depended on production seeds. Profile mocks now filter
by method. Rendering tests provide isolated history fixtures, and card coverage
uses real question/plan events. The app reconciles the echo before the receipt,
keeps live history during disconnects and never switches back to preview data.
The old agent-navigation and mock-reply expectations were updated for the single
assistant and live conversation behavior.

No backend contracts, model tools, providers, eval settings or parity pins were
changed. Voice methods and unnumbered web sources follow the INT-2 contract;
the base backend does not yet expose those voice methods or `POST /blobs`, so
production voice availability depends on the BE-3 implementation.

Validation passed with `pnpm check` (217 unit tests), the production build and
all 60 browser cases across the full suite and a targeted rerun. The rerun
cleared six failures and rechecked three voice flows; stale agent/tab selectors,
the world bridge ID filter and transient network errors accounted for the
additional full-suite failures. The library-disabled switch test also passed.
The lazy Markdown module is warmed outside individual viewer-test assertion
deadlines so the shared host's cold module loading does not cause false failures.
