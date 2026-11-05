# Worker B landing verification

Run from `apps/site` with Node 22 on `PATH`. No browser download is needed;
Playwright uses system Chrome.

```sh
export PATH=$HOME/.local/opt/node/bin:$PATH
nice -n 10 pnpm build
SITE_PORT=4333 nice -n 10 pnpm exec playwright test --config tests/b/playwright.config.ts
SITE_PORT=4333 nice -n 10 node --experimental-strip-types tests/b/capture.ts
SITE_PORT=4333 nice -n 10 node --experimental-strip-types tests/b/states.ts
SITE_PORT=4333 nice -n 10 node --experimental-strip-types tests/b/perf.ts
```

The default fixture fulfills Chrome requests from this worktree's production
`dist/`. It needs no listening server and cannot reuse another worker's stale
preview. Set `B_DIRECT_PREVIEW=1` to verify an already running, owned production
preview at `127.0.0.1:4333` instead. Astro's global preview lock may refer to another
worker: use `--ignore-lock` when starting an owned foreground preview, never
`--force` or `astro preview stop`. Stop only the PID you recorded yourself.

The 29 section tests cover fitting, a common benchmark axis, path geometry against
native Chrome, exact rail joins, blueprint arithmetic, native pin duration and
release, direct anchors, motion cleanup, terminal stop, and complete no-JS states.
The shared 31-test suite also passed against the owned production preview.

Screenshots are written to the task's `implementacion/b/` directory. Capture filters
are `B_SECTIONS` and `B_WIDTHS`; state filters are `B_THEMES`, `B_WIDTHS`, and
`B_NO_JS_ONLY=1`. The state tool captures the sticky machine as a viewport, since
beyond-viewport document clips misrepresent sticky paint positions.

Performance uses an 18-second native scripted scroll, 4x CDP CPU throttling, and
rAF samples. A zero longest-task value means no task reached the browser's 50ms
long-task threshold. Headless software rendering and host contention can inflate
paint and scheduling costs. This worktree still has worker A's section placeholders;
repeat budgets and performance after integration.
