# Scheduled automation backend adapters (AUTO-BE2)

Implemented: seven RPC methods, API payloads and contract registries; live event
bridge; ten agent-tool actions; mandatory approval on create/update; synchronous
engine steps; executor-backed tool steps; persisted live Excalibur chat notices;
daemon startup/shutdown hooks. The Service interface owns graph validation,
scheduling, version persistence and run execution (AUTO-BE1).

The engine adapter creates a fresh run for each step and returns the final model
answer. It fails on missing model, model errors, missing final answer or incomplete
engine completion. The parent context and a fifteen-minute cap bound the run.
Agent tools cannot ask questions, invoke automations or bypass restrictions with
call_tool, deferred dispatch or subagent. The original eval profile is untouched.

## Merge handoff

`cmd/excalibur/automations.go:newAutomationService` intentionally returns nil until
BE1 lands. RPCs and the tool report "automations not available" in this state.
Replace the factory body with Manager construction using DB, WorkDir
(already derived from the daemon data directory), principal Language, AgentRunner,
ToolInvoker and Notifier. Reconcile exact Config field names with BE1. If its
Start/Stop signatures differ from AutomationLifecycle (Start(context.Context),
Stop()), adapt those signatures in this factory. The daemon starts the manager
with its service context and stops it before stopping the orchestrator.

Frontend must add the shared wire shapes/RPCs to its typed contract and subscribe
with types `automation.updated`, `automation.deleted`, `automation.run` using the
existing feature-specific websocket filter. There is no server allow-list to add.
No frontend files are changed by BE2.

## Verification

Fake-based Go tests cover every tool action, approval rejection, RPC mapping,
limits, events and unsubscribe, actual engine final text/error/cancellation,
executor invocation/restrictions and persisted/live chat notices. Ordinary tests
make no external API calls. Contract parity tests run in internal/api.

Completed checks (all Go test commands used a systemd user scope with
MemoryMax=6G):

- Required API, daemon, automations and command package suites: pass.
- Related wireengine, agents, tools, skills and gateway suites: pass.
- Race checks across these packages: pass; the full daemon suite completed in
  169.7 seconds. An earlier ninety-second trial was too short for its existing
  SQLite/library coverage; rerunning with a five-minute limit passed.
- New automation adapter, RPC, lifecycle and catalog race checks: pass, including
  cancellation during an in-flight model request and unchanged eval membership.
- `go vet ./...`, `staticcheck ./...`, `gofmt -l .`, goroutine policy: pass.
- `GOOS=darwin go build -p 2 ./...` and Windows equivalent: pass.
