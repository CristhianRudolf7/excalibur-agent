# Excalibur web app

React 19 and TypeScript client for the Excalibur daemon. Vite serves development
assets; TanStack Router, TanStack Query and Zustand manage routing, server data
and live state. Feature code and tests live together under `src/features/`.

## Run locally

Use Node.js 22+ and pnpm 12.16.1. From the repository root:

```sh
pnpm install --frozen-lockfile
cd apps/web
pnpm dev:full
```

`dev:full` builds a temporary Go daemon, starts it on `127.0.0.1:7787` with
disposable data, and starts Vite at `http://localhost:5173`. It inherits model
configuration from the environment. Ctrl+C stops the owned children and removes
temporary state. `EXCALIBUR_DEV_LISTEN` overrides the daemon address; also set
`EXCALIBUR_DAEMON_URL` if the proxy needs a different target.

For an existing daemon, run it separately:

```sh
excalibur daemon start --listen 127.0.0.1:7787
```

Then run `pnpm dev` in this directory. Vite proxies `/rpc`, `/ws`, `/blobs` and
`/healthz` to `http://127.0.0.1:7787` by default. Set `EXCALIBUR_DAEMON_URL` to
change the target. Microphone/camera features need localhost or HTTPS and browser
permission. Browser automation needs system Chrome and the Playwright driver.
Robotics Lab has separate optional Python dependencies.

## Check and build

Run from `apps/web`:

```sh
pnpm typecheck
pnpm lint
pnpm format:check
pnpm test
pnpm build
pnpm test:e2e
```

`pnpm check` combines the first four commands. Playwright uses system Chrome;
its default configuration builds and previews the production app on port 4183.
`EXCALIBUR_WEB_TEST_PORT` selects another port. Tests use simulated API responses;
`pnpm test:e2e:integration` uses the separate integration configuration.
`pnpm preview` serves `dist/` for inspection, not a production daemon.

## Development boundaries

The client uses JSON-RPC on `/rpc`, event subscriptions on `/ws` and blobs on
`/blobs`. Shared method/event definitions live in `packages/contracts`; typed
client code lives in `src/lib/rpc`. Production views report unavailable APIs
instead of silently substituting fixtures. Some extension/store catalog state
is local to the browser; it does not install a remote marketplace package.

Keep visible strings in i18n, use semantic theme tokens and preserve keyboard,
reduced-motion and narrow-screen behavior. Read [AGENTS.md](AGENTS.md) for
conventions and the [root README](../../README.md) for current features and limits.
