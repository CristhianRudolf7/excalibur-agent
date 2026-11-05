# AGENTS.md

Instructions for coding agents working in this repository. The working
language is English only: code, identifiers, comments and every Markdown
file. No Spanish files and no `.es.md` pairs.

## Purpose and layout

Excalibur is a personal AI assistant and programming harness: a single
Go binary (`cmd/excalibur`), a React web app (`apps/web`) and a public
Astro site (`apps/site`). The web app talks to the daemon through the
JSON-RPC 2.0 API in `packages/contracts`.

Key trees: `apps/` (web app and site), `extensions/` (bundled integrations),
`cmd/excalibur` (binary), `internal/` (core), `pkg/extsdk`
(public extension SDK), `packages/contracts` (shared JSON schemas),
`docs/{architecture,decisions,development,research,store,user-guide}`,
`integrations/{harbor,clients}`, `scripts/bench`, `store/{catalog,
examples,installer}`, `tests/` (integration and Section 23.5 suites).

## Source of truth

The architecture specification lives outside this repository and must not be
copied or committed here. Reference its sections by number. Public behavior
and verification evidence are documented in `docs/` and tested in code.

Linux, Windows and macOS are supported. OS-specific code belongs in
`internal/platform` and OS-suffixed files. Keep builds working for all three;
record feature-specific limits and native verification gaps in the platform
guides. Autopoiesis, automatic skill learning and mobile packaging are out of
scope.

The daemon binds to localhost by default. The secrets vault encrypts stored
credentials; tools execute with the user's permissions and isolation varies
by platform. Public previews expose an app to anyone with the link. See the
README security notes before exposing the daemon or a preview.

## Contract rules

- `internal/types`, `internal/api` and the core interfaces (`Store`,
  `Provider`, `Tool`, `internal/platform`) are shared contracts. Do not
  break them. Changes must be additive and minimal, and recorded in
  `docs/decisions/NNNN-<slug>.md`.
- Keep changes within the task scope. Depend on interfaces and use the fakes in `.../fake`
  subpackages in tests.
- Every long-lived goroutine uses `safego.Go`; bare `go` statements are
  flagged by `scripts/check-no-bare-go.sh`.
- Ordinary tests use simulated servers (`httptest`) and NEVER call paid
  APIs. Live checks sit behind the `live` build tag and `EXCALIBUR_LIVE=1`.
- Never print, log or commit model credentials or API keys.
- `docs/development/` holds one document per milestone with its contract
  and backend tests. Never describe as implemented what is only planned.

## Verification commands

From the repository root, all of these must pass:

```sh
gofmt -l .
go vet ./...
staticcheck ./...
go test ./... -race
GOOS=darwin go build ./...
GOOS=windows go build ./...
```

Or simply `make verify` (`make test`, `make lint`, `make cross`).
While iterating prefer `go test -race -p 2` for package-scoped runs.

## Milestones

H0–H13 have implementations and verification-table tests. Verify the current
checkout before claiming those tests pass. Production model wiring builds
real providers
from layered config, environment, the operator credentials file and
`secret://` vault refs through `internal/providers/registry`, and
every entry point (`exec`, `task` background runs, `mcp`, `acp`,
daemon intake decider, memory embeddings) runs through the
`internal/models` gateway; `exec` fails with exit 1 when
unconfigured and keeps the deterministic step only under explicit
`EXCALIBUR_PROVIDER=stub`. Live checks sit behind `-tags live` and
`EXCALIBUR_LIVE=1` with results in
`docs/development/live-smoke.md`. Per-section evidence lives in
`docs/development/coverage.md`; close-out fixes are recorded in
`docs/decisions/0034-w5-closure.md`, the wiring decisions in
`docs/decisions/0035-w6-live-wiring.md`, and the native TB21 support
(Harbor 0.25.0 pin, Section 23.2 resolutions, `bench` command, rich
trial records, 2-task live verification) in
`docs/decisions/0036-tb21-harbor-pinning.md`,
`docs/decisions/0037-tb21-unverified-resolved.md` and
`docs/development/terminal-bench.md`.
Do not regress any milestone: every change must keep its area's
contract and backend tests passing (`go test ./... -race`).

- H0 Repository skeleton and shared contracts: module, types and verification.
- H1 Providers of language models: `internal/providers` adapters on the
  wave-0 contract; contract tests against simulated servers.
- H2 Base storage: identifiers, layered config loading, cross-platform
  `internal/platform`, SQLite porter, blobs, migrations.
- H3 Durable engine and terminal: journal, agent loop, gateway subset,
  context compiler basics, core file/terminal tools, extension registry.
- H4 Non-interactive mode and benchmarks: `exec`, eval profile, events,
  metrics, ATIF, Harbor adapter, bench launchers, plus native TB21
  (`excalibur bench tb21|tb40 run|smoke|status|report`,
  `scripts/bench/run.sh` wrapper, rich per-trial records, 2-task live
  verification; see `docs/development/terminal-bench.md`).
- H5 Verifiable work: tasks/plans/requirements/criteria/evidence,
  completion gate, run states, loop detector, limits.
- H6 Context and cache: layers, deltas, masking, compaction, cache
  adapters, token counting.
- H7 Programming tools and edit concurrency: deferred tools,
  `search_tools`, history, snapshots, LSP, code graph, leases, worktrees.
- H8 Subagents, programming skills, delegation: orchestrator, profiles,
  mailboxes, directives, skill index, `task` family, MCP server, ACP.
- H9 Daemon, master, conversation: gate, intake, fast lane, projects,
  scopes, delegation through the daemon.
- H10 Recovery, continuous operation, vault: boot, safe mode, agenda,
  retention, install/update, secrets vault.
- H11 Memory and documents: retriever, consolidation, embeddings,
  library, citations.
- H12 Assistant skills and decider: assistant/domain skill groups,
  external skill installs, roles, escalation, decider.
- H13 Capabilities, automation, extensions: capability
  contracts, MCP, extension kit, triggers, workflows, flow simulation.
