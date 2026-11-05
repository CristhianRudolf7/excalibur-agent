# Tasks, reminders and project workspaces

These views use the typed JSON-RPC client and TanStack Query. Task and reminder
wire types follow the INT-2 shared contract. Project and file types follow the
existing daemon handlers: `project.get` accepts `project_id`; `files.list`
returns relative string entries with `/` directory suffixes and `/…` collapsed
directories. ProjectExplorer reuses the file explorer's virtualized tree-row
presentation and navigates directories without inventing file contents.

The `/tasks` route and chat task panel share the same read-only task view.
Queries support status, project, text and cursor filters. The detail drawer
shows the request, requirements, criteria, evidence, plan dependencies, notes
and runs. Task events invalidate all task query variants and open details.

Reminders use the persistent agenda client contract. The picker interprets
wall time in the selected IANA timezone, rejecting invalid zones and
nonexistent daylight-saving times. Upcoming groups use the viewer's calendar
days, with overdue reminders included in Today. Times display in each
reminder's stored timezone. Mutations cancel in-flight reads, snapshot the
query cache, apply an optimistic change and restore the snapshot on failure.
Controls stay disabled until the mutation and reconciliation finish.

Events use the existing bounded reconnect and replay deduplication transport.
A reconnect refreshes the query cache. `reminder.fired` displays a temporary
toast and highlights the corresponding reminder. Missing methods and transport
errors show explicit retry states. Production views never fall back to mock
data.

## Event switch

`EXCALIBUR_WEB_WORK_EVENTS` is a build-time web switch, default `1` (enabled).
Set it to `0` and rebuild to disable the task/reminder event subscription;
other values keep it enabled. RPC reads, explicit refetches and mutations
remain available. Restore `1` and rebuild to restore live updates. It has no
effect on daemon profiles, model tools, eval configuration or chat events.

## Verification

- `VITEST_MAX_WORKERS=2 pnpm check --testTimeout=20000` runs typecheck, lint, formatting and unit
  tests with bounded test concurrency and a 20-second per-test budget on the
  shared development machine.
- `pnpm build` builds the production application and regenerates routes.
- `EXCALIBUR_WEB_TEST_PORT=4284 pnpm exec playwright test e2e/tasks.spec.ts e2e/reminders.spec.ts
e2e/projects.spec.ts --workers=2` exercises the RPC views, mutations,
  rollback, live events, unavailable states, keyboard navigation and 390 px
  layouts. All RPC and event traffic is mocked.
- The task/project smoke cases in `e2e/workspace.spec.ts` now use RPC fixtures
  instead of asserting seeded projects or locally cycling task statuses.

The new task/reminder backend methods are delivered by the backend workers.
Until those methods are present, the views intentionally show unavailable
states. No backend contract changes are made by this web implementation.

The Playwright port override isolates this worktree from concurrent test
servers. The default remains 4183 when the variable is absent.
