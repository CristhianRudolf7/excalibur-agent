# AGENTS.md

Instructions for coding agents working in this repository. The working
language is English only: code, identifiers, comments and every Markdown
file. No Spanish files and no `.es.md` pairs.

## Purpose and layout

Excalibur is a personal AI assistant and programming harness: a single
Go binary (`cmd/excalibur`) organized in `internal/` packages. The web
interface is out of scope; it talks to the daemon through the JSON-RPC
2.0 API in `packages/contracts`.

Key trees: `cmd/excalibur` (binary), `internal/` (core), `pkg/extsdk`
(public extension SDK), `packages/contracts` (shared JSON schemas),
`docs/{architecture,decisions,development,research,store,user-guide}`,
`integrations/{harbor,clients}`, `scripts/bench`, `store/{catalog,
examples,installer}`, `tests/` (integration and Section 23.5 suites).

## Source of truth

The ONLY specification is the architecture document. It lives OUTSIDE
this repository and is never copied, translated or committed here;
reference its sections by number only. Implement exactly what it says
for your sections: every responsibility, algorithm, state, error case,
interruption behavior and every row of its verification tables must
exist in code and be covered by Go tests.

Scope already fixed: Go, single module, Linux only (other OSes via
`internal/platform` stubs returning `errors.ErrUnsupported`; the binary
must still build with `GOOS=darwin` and `GOOS=windows`), security
deferred except the secrets vault (20.1), no autopoiesis, no automatic
skill learning, no mobile.

## Contract rules

- `internal/types`, `internal/api` and the core interfaces (`Store`,
  `Provider`, `Tool`, `internal/platform`) are shared contracts. Do not
  break them. Changes must be additive and minimal, and recorded in
  `docs/decisions/NNNN-<slug>.md`.
- Stay inside your assigned packages; other agents edit the rest in
  parallel. Depend on interfaces and use the fakes in `.../fake`
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

Status: H0–H13 implemented, each with its verification-table tests
green. Per-section evidence lives in `docs/development/coverage.md`;
close-out fixes are recorded in `docs/decisions/0034-w5-closure.md`.
Do not regress any milestone: every change must keep its area's
contract and backend tests passing (`go test ./... -race`).

- H0 Repository skeleton and shared contracts (wave 0) — done.
- H1 Providers of language models — done (`internal/providers`).
- H2 Base storage — done (identifiers, layered config, Linux
  `internal/platform`, SQLite porter, blobs, migrations).
- H3 Durable engine and terminal — done (journal, agent loop, gateway
  subset, context compiler basics, core file/terminal tools, extension
  registry).

- H1 Providers of language models: `internal/providers` adapters on the
  wave-0 contract; contract tests against simulated servers.
- H2 Base storage: identifiers, layered config loading, Linux
  `internal/platform`, SQLite porter, blobs, migrations.
- H3 Durable engine and terminal: journal, agent loop, gateway subset,
  context compiler basics, core file/terminal tools, extension registry.
- H4 Non-interactive mode and benchmarks: `exec`, eval profile, events,
  metrics, ATIF, Harbor adapter, bench launchers.
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
- H13 Capabilities, automation, extensions on Linux: capability
  contracts, MCP, extension kit, triggers, workflows, flow simulation.
