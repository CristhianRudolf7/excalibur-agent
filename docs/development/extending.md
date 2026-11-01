# Adding functionality safely

Use this guide to choose an extension seam, wire it into a working execution
path, and verify that existing users keep the same behavior. The architecture
specification remains external; the relevant contracts are Sections 3, 10,
12–14, 17, 20.1, 21 and 23. This guide describes the current code, including
places where a declared surface still uses a stand-in.

## System map

```text
clients → internal/gateway + internal/api → internal/daemon
                                             ↓
embedded exec (internal/cli) ─────────→ internal/engine
                                             ↓
                  tools → capabilities / skills / memory / agenda / vault
                  model calls → internal/models → internal/providers
```

The gateway is the client JSON-RPC transport; the models gateway is a separate
model-call boundary. The daemon owns persistent services, conversation and
orchestration. `internal/daemon/engine_wire.go` and
`internal/daemon/services.go` compose its engine services. Embedded exec builds
its catalog with `wireengine.BuildEmbedded` in
`internal/wireengine/catalog.go`; `internal/cli/exec_engine.go` applies the
execution profile. The daemon also uses that shared catalog builder, then
binds daemon services. Registering only in a CLI command or only in a daemon
route does not establish parity.

Execution profiles are `default` (the full assistant), `delegate` (project
memory, without personal memory or conversation/master), and `eval` (isolated
state, no memory or master): see `ProfileName` in `internal/cli/profile.go`.
These differ from agent-role profiles in `internal/agents`. Preserve the
assistant path when adding behavior intended only for autonomous exec runs.

`cli.EffectiveTools` in `internal/cli/exec.go` defines the effective core and
deferred surfaces. Eval uses the frozen table and skill catalog in
`internal/eval/profile.go`; `EvalProfileConfig.Hash` records its configuration.
The fixed tool prefix also has `DeferredCatalog.PrefixHash` in
`internal/tools/deferred.go`. Stable schemas, ordering, descriptions and skill
content protect cache reuse and make evaluation and auditing comparable. A
new deferred tool can leave the prefix unchanged while still changing the
catalog-name parity hash. Do not update pins merely to silence a failure.

## Choose the seam

| Need | Use | Main contract / composition point |
| --- | --- | --- |
| A small operation the model invokes | Tool | `tools.Tool`; shared catalog builder |
| An optional or domain-specific operation | Deferred tool | Same `Tool`; `DeferredCatalog.RegisterDeferred` |
| An external service with durable task states | Capability adapter | `capabilities.Capability`; capability registry and tool adapter |
| Reusable instructions using existing operations | Skill | `SKILL.md`; `skills.Index` |
| Client control or structured client data | API method | `api.Methods`, gateway routing, daemon backend and JSON contracts |
| Work at a time, after an event, or as a graph | Agenda / workflow | `agenda.Launcher`, `Schedule`, `StepExecutor` |
| A language-model transport | Provider | `providers.Provider`; provider registry |
| Installable operation or automation | Package / extension / workflow | Tool package manager, extension loader, agenda graph |
| Another memory representation | Memory service change | `memory.Kind`, admission and retrieval; no kind-plugin registry |

Prefer a skill when instructions suffice. A tool is executable behavior, not
an instruction bundle. Prefer deferral for optional features so the fixed
prefix stays small. A client API method is not automatically a model tool.

## Tools and deferred tools

1. Implement `tools.Tool` from `internal/tools/tool.go`: `Definition`,
   `Invoke(context.Context, tools.Invocation) (tools.Result, error)`, and
   `ResourceKeys(json.RawMessage) ([]string, error)`. Validate arguments and
   honor cancellation. Return canonical serialization keys for affected
   resources; an empty list is appropriate only when there is no shared
   resource. Optional `tools.Prober` and `tools.Compensator` support recovery
   and undo.
2. Fill `tools.Definition` deliberately. `Name` must be unique snake_case,
   at most 64 characters. Supply `Description` and `InputSchema`; include an
   `OutputSchema` when consumers depend on a structured result. `Effect` and
   `Idempotency` describe actual effects and retry safety. `Cancel` describes
   interruption, and `Timeout` bounds execution. `OutputBudget` caps inline
   output. `Platforms` and `Profiles` declare availability. `Group`, `Tags`
   and `Keywords` make deferred discovery useful. `Inverse`, `Probe` and
   `DryRun` name supported companion operations; `Fast` must reflect whether
   the operation is suitable for a short synchronous path.
3. Register through `Registry.Register` in `internal/tools/registry.go`, or
   `DeferredCatalog.RegisterDeferred` in `internal/tools/deferred.go`.
   Deferral is catalog membership, **not a Definition field**. If a tool is
   already registered, use `MarkDeferred`; it does not implement the tool.
   Built-ins are composed by `RegisterBuiltins` in `internal/tools/catalog.go`.
   Add optional wiring at the shared `BuildEmbedded` composition point and
   bind the real collaborators in both embedded and daemon paths.
4. Execute through `tools.Executor` in `internal/tools/pipeline.go`, not by
   calling `Invoke` directly from production orchestration. Wire its journal,
   output store and redactor. Preserve `Result.Status`, `ExitCode`,
   `Truncated` and `BlobRef`; successful transport does not mean a successful
   operation. `get_tool_output` must share the executor's output store.
5. Test direct invocation, pipeline execution, deferred discovery and actual
   invocation through `call_tool`. Test allowed and denied profiles.

This small read-only tool shows the shape. Imports are `context`,
`encoding/json`, `time`, and this module's `internal/tools` and
`internal/types`; snippets in this guide assume an enclosing package/function
and existing collaborators unless a full declaration is shown.

```go
type readinessTool struct{}

func (readinessTool) Definition() tools.Definition {
    return tools.Definition{
        Name: "readiness", Description: "Read local service readiness",
        InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
        Effect: types.EffectReadOnly, Idempotency: types.IdemPure,
        Cancel: types.CancelImmediate, Timeout: time.Second, OutputBudget: 1024,
        Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
        Group: "service",
    }
}
func (readinessTool) Invoke(ctx context.Context, _ tools.Invocation) (tools.Result, error) {
    if err := ctx.Err(); err != nil {
        return tools.Result{Status: tools.StatusCanceled}, err
    }
    return tools.Result{Status: tools.StatusOK, Output: "ready"}, nil
}
func (readinessTool) ResourceKeys(json.RawMessage) ([]string, error) { return nil, nil }

// In the catalog composition function, with its existing catalog:
// if err := cat.RegisterDeferred(readinessTool{}); err != nil { return err }
```

Availability metadata is not an authorization system. `MapRegistry.Register`
validates the definition but does not enforce profile or platform admission.
`Executor.Execute` has no profile argument. Inspect `cli.EffectiveTools`, the
profile gate and deferred pruning in `internal/cli/exec_engine.go`, as well as
any daemon exposure. Both discovery and execution must obey the chosen
surface; hiding a definition alone is insufficient. Do not add an optional
assistant tool to eval's frozen table by default.

For credentials, follow `internal/wireengine/vault.go`: `VaultResolver`
adapts provider resolution, `ExecSecretResolver` resolves exec environment
references, and `ProfileRedactor` supplies the output policy.
`vault.ResolveRequest` in `internal/vault` carries tool, run, project,
delegation and destination policy. Resolve `secret://` references at the
execution boundary with the correct tool identity; never substitute a raw
secret into a model-visible argument, error or description. Never print,
log or commit credentials. Redact before storing output or emitting events.
Eval has no vault; delegate resolution must retain project restrictions.

## Capabilities, adapters and channels

1. Implement `capabilities.Capability` in
   `internal/capabilities/capability.go`: `Manifest`, `Invoke`, `Poll`,
   `Cancel`, and `Update`. These methods do not take a context in the current
   contract. Bound network activity inside the adapter; do not invent a
   context-aware variant of the interface.
2. Validate `capabilities.Manifest`: identity, nonempty version, operations,
   platforms/profiles and resource limits. Each `Operation` carries the
   schemas, effect, idempotency, cancellation and discovery fields;
   `TimeoutSecs` maps to a tool timeout and `OutputBudget` to inline bytes.
   The adapter copies operation-level profiles/platforms; manifest-level
   declarations alone do not filter the tool surface.
3. Register the implementation with `capabilities.Registry.Register` in
   `internal/capabilities/registry.go`. Use `tools.ToolsFromCapability` in
   `internal/tools/capability_adapter.go` and register its returned tools as
   deferred. Operation names must be unique across the whole tool catalog.
4. Wire client access through `daemon.CapabilityRoutes` in
   `internal/daemon/caps.go`. A `capability.register` manifest without a
   native implementation or MCP execution route can be listed but cannot
   execute. Test both client access and model-tool access.
5. Preserve idempotency keys and all task states, including `input_required`,
   failure, cancellation and restart polling. Resolve secrets privately at
   the adapter boundary and redact payloads. Apply the profile rules above.

```go
// cap implements capabilities.Capability; reg and cat are the shared registries.
if err := capRegistry.Register(cap); err != nil { return err }
adapted, err := tools.ToolsFromCapability(cap)
if err != nil { return err }
for _, tool := range adapted {
    if err := cat.RegisterDeferred(tool); err != nil { return err }
}
```

Channels are not a universal new plugin interface. Current messaging and
workspace contracts are capability manifests in
`internal/capabilities/channels.go`; ordinary builds use deterministic fakes.
For contextual applications, `tools.AppChannel` in
`internal/tools/extensions_routes.go` supplies `Invoke`, `EventsSince` and
`Close`; `Loader.BindApp` in `internal/tools/extensions.go` binds an
implementation. Read its event and instance semantics before adding a
transport. Do not mistake `MemoryAppChannel` for a live service connector.

## Skills

1. Create an English `SKILL.md` with `name`, `description` and the metadata
   parsed by `Frontmatter` / `ExcaliburMeta` in `internal/skills/spec.go`.
   Declare group, version, platforms, prerequisite binaries/environment
   names, tools and lifecycle state. `allowed-tools` declares an additional
   restriction, not a grant of permission or a substitute for profile
   gating. Today the index carries that list and `LoadSkillTool` prints it
   as instructions; the engine does not install a per-skill execution
   allowlist. Do not present it as an enforced security boundary. Never put secret values in prerequisite declarations.
2. Use a known group from `internal/skills/groups.go`. A new group requires
   an explicit code/catalog change; arbitrary group IDs are not accepted.
   Built-ins live under `internal/skills/builtin/<group>/<skill>/SKILL.md`
   and are embedded by `BuiltinFS` in `internal/skills/builtin.go`.
3. For external skills use `skills.Index.Install` in
   `internal/skills/install.go` after user authorization, staging and review
   of the contents. Index precedence is project `.agents/skills`, user,
   installed, then builtin: see `Roots` and `Index.Scan` in
   `internal/skills/index.go`. Check diagnostics and conflicts after scanning.
4. Bind `SearchSkillsTool` and `LoadSkillTool` from
   `internal/skills/skilltools.go` to the same index used by the run. Loading
   resolves declared deferred tools; it does not implement missing tools.
   Skills do not have `tools.Definition` fields such as Effect or Timeout;
   their executable tools still need those contracts and profile gates.
5. Test format, precedence, missing requirements, tool resolution and
   `allowed-tools`. Keep eval's builtin skill fingerprint stable or record
   the intentional change with its new profile hash.

```go
ix := skills.NewIndex(skills.Roots{Project: projectSkills}, skills.BuiltinFS(),
    skills.WithToolCheck(func(name string) bool {
        _, err := reg.Get(name)
        return err == nil
    }))
if err := ix.Scan(); err != nil { return err }
// Inspect ix.Diagnostics() and ix.Conflicts() before exposing the index.
```

## Client API methods

There is no general runtime API-method plugin registry. Adding a method is a
coordinated contract change, not just a handler registration.

1. Add the constant, parameter/result types and `api.Methods` entry in
   `internal/api/methods.go`; update the JSON schemas in `packages/contracts`.
2. Route it in `Server.route` in `internal/gateway/gateway.go`, using the
   existing backend seam. Bind the daemon service in
   `internal/daemon/routes.go` / `DaemonBackend.Passthrough` or the relevant
   service route. Existing families are examples, not an automatic route
   for arbitrary method names.
3. Validate inputs, scope, idempotency, cancellation and pagination; preserve
   typed `api.Error` codes from `internal/api/envelope.go`. API methods do
   not carry `tools.Definition`; explicitly enforce permissions, profile
   access and result limits in their service boundary.
4. Test dispatch, invalid parameters, unknown methods, real service wiring
   and contract export parity in `internal/api/api_test.go`. Record the
   additive contract change in a decision record. If the model also needs
   the action, add a separate tool adapter.

For example, an existing family is wired through `daemon.SkillsOp`:

```go
// In a route test with an initialized ix; this is an existing API method.
result, err := daemon.SkillsOp(ix, api.MethodSkillsSearch,
    json.RawMessage(`{"query":"planning","limit":3}`))
if err != nil { return err }
_ = result
```

Send secret values only through dedicated vault methods, never through
`conversation.send`. Do not serialize internal credentials in generic
result objects. Changing API, gateway or contracts requires explicit scope
and review of those shared contracts; it is not incidental extension work.

## Agenda jobs and workflows

1. Use `agenda.Schedule` in `internal/agenda/schedule.go` and
   `Agenda.AddSchedule` in `internal/agenda/agenda.go` for timed work.
   Choose timezone, missed-fire policy, scope and opaque payload explicitly.
   Implement `agenda.Launcher` (`LaunchFire`, `LaunchReminder`) to enqueue
   work quickly; never execute a long task synchronously in the timer loop.
2. For a graph, use `WorkflowDef` and its validation in
   `internal/agenda/workflow.go`. Implement `agenda.StepExecutor.ExecuteNode`
   from `internal/agenda/engine.go` for tool, agent and script effects; the
   engine handles control nodes. Connect `Confirmer` and `EventWaiter` where
   needed, and persist instances through the appropriate agenda store.
3. `Engine.Start` pins the workflow version. Preserve resumable step records,
   idempotency, bounded loops, retry limits, compensation and confirmation.
   The graph has no blanket tool Definition: route its tool nodes through
   the normal executor and enforce scope/profile restrictions there.
4. Internal maintenance is a fixed table, not an open job-plugin registry:
   `DefaultInternalJobs` in `internal/agenda/internaljobs.go` has ten entries
   and `ValidateInternalJobs` enforces that set. `InternalJob.ShouldRun`
   yields to user activity and respects low-activity windows. Adding a new
   internal job needs a contract decision plus an execution/lifecycle hook;
   appending metadata does not schedule a handler. Inspect daemon lifecycle
   wiring in `internal/daemon/services.go` before connecting background work.
5. Test downtime reconciliation, DST/timezones, safe mode, cancellation,
   duplicate fires and restart behavior. Use fake clocks/executors. Job
   payloads store secret references, never resolved secrets. Eval does not
   gain a background agenda or assistant memory as a side effect.

```go
// a is an Agenda with a real store and a quick, scoped Launcher.
err := a.AddSchedule(&agenda.Schedule{
    ID: "local-maintenance", Kind: agenda.ScheduleEvery,
    Timezone: "UTC", Interval: time.Hour, Missed: agenda.MissedSkip,
    ScopeID: scopeID, Payload: "local-maintenance",
})
if err != nil { return err }
```

## Providers

Provider adapters are shared connectors. Extend them only as an explicitly
reviewed provider task; never bypass the models gateway to add a feature.

1. Implement `providers.Provider` in `internal/providers/provider.go`:
   `Name`, `Chat`, `Stream` and `Close`. Preserve usage, typed provider
   errors, cancellation, streaming tool-call reconstruction and emitter
   errors. Use simulated HTTP servers for transport tests.
2. Register a `registry.Constructor` with `registry.Register` in
   `internal/providers/registry/registry.go`. It receives `ResolvedDef` and
   returns `(providers.Provider, error)`. Instances are built by
   `registry.New` / `NewMany` from `ProviderDef`; integrate through existing
   config and `internal/models`, not a parallel HTTP client in a tool.
3. Use registry credential resolution (`ResolveKey`) and its vault hook,
   environment and explicit-config fallback; do not print resolved config.
   Profiles control model routing and tools outside this transport interface.
   `Definition` fields and tool deferral do not apply to providers.
4. Test streaming, error classes, retry/fallback bounds, usage and close;
   preserve the frozen eval single-model/configuration rules.

```go
// ctor has type registry.Constructor and constructs the new adapter.
if err := registry.Register("local_adapter", ctor); err != nil { return err }
p, err := registry.New(ctx, registry.ProviderDef{
    Kind: "local_adapter", Model: "local-model", BaseURL: endpoint,
}, secretResolver)
if err != nil { return err }
defer p.Close()
// Hand p to the existing models composition; do not call it around the gateway.
```

## Memory kinds and service adapters

There is no dynamically registered memory-kind extension interface.
`memory.Kind` in `internal/memory/types.go` is a fixed set; storage and
admission are implemented in `internal/memory/repo.go` and
`internal/memory/admit.go`. A new kind requires coordinated admission,
validation, persistence/migration, retrieval, expiry/review, provenance and
scope tests. Do not merely add a string constant or write rows directly.

For an existing kind, construct a `memory.Candidate` and use
`Service.Propose`, `Admit` or `Write` according to the admission contract.
For service substitutions, the real seams include `Embedder` in
`internal/memory/embed.go` and `Decider`, `Extractor`, `Reranker` and
`VisionExtractor` in `internal/memory/service.go`, selected by service options.
These are not new memory kinds. Preserve source trust and personal/project
boundaries; external sources cannot become identity or preferences. Never
store credentials as memory. Model-backed implementations still use
`internal/models`. Expose memory through existing adapters in
`internal/memory/tools.go`, obeying their tool metadata; eval memory remains
off and delegate memory remains project-scoped.

```go
// svc already has the run's allowed scopes and dependencies.
_, err := svc.Propose(ctx, memory.Candidate{
    Scope: scopeID, Kind: memory.KindFact, Statement: statement,
    Sources: sources, Trust: trust, Conversation: conversationID,
})
if err != nil { return err }
```

## Store items: installable tools and flows

Installing, creating or publishing an item requires the user's explicit
request (Sections 12.8, 13.9 and 14.1). Ask first when that authorization is
absent, show what will be installed or published and its requirements, and
wait for the answer before activation or external publication. An explicit
request already supplies authorization within its scope. Automatic skill
learning is deferred. Low-level install/create functions do not themselves
implement a universal confirmation gate; callers must preserve this rule.

What works today:

- **Skills:** `skills.Source` (`DirSource` and `TarGzSource`) stages content
  without executing it. `Index.Install` validates format, builds an
  `InstallSummary` with contents, tools and missing prerequisites, replaces
  the installed directory with rollback on rename failure, writes an
  `InstallRecord`, then rescans. Unknown tools are reported rather than
  rejected at this boundary. `Index.CheckInstalled` detects hash drift and
  revalidates installed skills; version/hash records are not a signature or
  a promise of automatic semantic-version resolution. Review the summary
  before activation; `Install` returns it after performing the installation.
- **Local extensions:** `extInstall` in `internal/cli/ext.go` copies a local
  directory containing `excalibur-extension.yaml` to `ExtensionsRoot`.
  Copying is not validation or activation. `Loader.LoadDir`, `Activate` and
  `ActivateChecked` in `internal/tools/extensions.go` provide parsing,
  binding and optional conformance probes. Disabling/removing retires
  deferred tools and closes the connector immediately; `Update` refuses a
  replacement while calls remain in flight. Do not assume disable drains
  calls. Use `Watch` / `Close` only with an explicit lifecycle owner.
- **Tool packages:** `PackageManifest`, `PackageManager.Create` and
  `ToolPackageTool` in `internal/tools/packages.go` implement validation,
  declared tests and deferred activation. The manager keeps records in
  memory; do not assume a durable remote package registry. Implementations
  are scripts, browser routines, tool sequences or app calls, each subject
  to the collaborators actually wired in `executeImpl`. Effectful package
  tests use no-effects output and do not prove real effects work. A package
  extension additionally needs `tool.json`, consumed by `Loader.bindPackage`.
- **Flows:** `agenda.WorkflowDef` validates versioned graphs; `Engine.Start`
  pins the version and `Engine.RegisterSubflow` registers nested graphs.
  Workflow tests use `agenda.RunTests` in `internal/agenda/engine.go`, and
  simulation lives in `internal/agenda/flowsim.go`. This supplies execution
  and testing seams, not a remote workflow-store installer.
- **Store capability:** `StoreManifest` in
  `internal/capabilities/services.go` declares `store_install` and
  `store_publish`. `StoreFake` records keyed requests deterministically;
  it does not fetch, review, install or publish remote artifacts.

Extension manifests use `tools.Manifest` in `internal/tools/manifest.go`
(or the matching public `extsdk.Manifest` in `pkg/extsdk/manifest.go`). The
current contract is `excalibur-ext/1`. Supply identity, a nonempty version,
kind (`mcp`, `app`, `package`, `builtin`), tools, and applicable lifecycle,
transport, config, profile/platform and resource fields. Manifest validation
checks the contract and tool definitions; it does not implement a general
version solver. `pkg/extsdk/tool.go` is the external tool contract; external
modules cannot import this repository's `internal` packages. SDK parity is
covered in `internal/tools/parity_test.go`.

A local activation skeleton, **after authorization and review**, is:

```go
loader := tools.NewLoader(reg, cat, executor)
defer loader.Close()
ext, err := loader.LoadDir(localDirectory)
if err != nil { return err }
_, err = loader.ActivateChecked(ext, probes)
if err != nil { return err }
```

Package manifests do not expose profile/platform fields; their generated
definition currently lists Linux and all three execution profiles, while the
`tool_package` creator is declared for default/delegate. Keep optional
packages out of eval through its effective catalog gate. Extension and
capability manifests also require admission checks at the actual execution
surface; do not assume metadata itself enforces permissions. Manifests,
package scripts, flow inputs, test fixtures and install summaries must carry
secret references or environment names, never credential values.

What is not built here: a production remote store backend, a populated
published catalog, or a general remote tool/flow dependency resolver and
installer. `store/catalog`, `store/examples` and `store/installer` contain
README scaffolding. `excalibur ext install` rejects a nonlocal reference with
a message that it needs the store capability. `daemon.SkillsOp` currently
rejects `skills.install` because that route needs a staged directory; the API
method name alone is not a working download/install path. The specification's
`package_install` / `package_publish` names are not wired production tools in
the current catalog. Build on the staging, loader, package and workflow seams
above rather than advertising an end-to-end store that does not yet exist.

## Safety checklist for every change

- Match the architecture responsibilities, state/error cases and verification
  rows for the affected sections. Keep shared `internal/types`, `internal/api`,
  Store, Provider, Tool and platform contracts additive and minimal. Record
  contract changes and intentional frozen-surface changes under
  `docs/decisions/NNNN-<slug>.md`, including the reason and expected hashes.
- Give new behavior an `EXCALIBUR_*` switch, normally default ON. Scope
  autonomous-only changes to exec/eval; test the OFF path and the assistant
  path. Document name, default, accepted values, affected profiles, behavior
  and rollback in the package's section of
  `docs/development/agent-quality.md`. A documentation-only change does not
  need a runtime switch. Use the existing package switch-reading convention;
  there is no universal feature-switch registration interface.
- Keep eval's tool table, skill hash and profile hash stable unless an
  intentional, reviewed change is recorded. Check prefix, names and profile
  pins independently. Preserve deterministic ordering. Do not silently make
  installed tools or host-specific skills part of a frozen eval catalog.
- Do not casually modify connectors: `internal/providers/**`, the model
  gateway protocol in `internal/models`, `internal/gateway`, `internal/api`,
  `packages/contracts`, `integrations/**`, MCP/ACP servers in
  `internal/cli/mcp*.go` / `internal/cli/acp*.go`, or the Harbor invocation in
  `cmd/excalibur/bench.go`. Changes there need their own explicit contract
  scope and parity tests. Read them when wiring an extension.
- Add unit tests with fakes and `httptest` for success, malformed input,
  cancellation, timeout, duplicate effects, failure status, scope/profile
  denial, bounded output and retrieval of the full result. For durable work,
  cover crash/restart and repeat invocation with the same key. Use fakes in
  the owning package or its `fake` subpackage. Never call paid APIs or launch
  live evaluations in ordinary tests.
- Use Section 23.6 replay where it exercises the changed path:
  `eval.Recorder`, `eval.Player.Replay` and `HashRequest` in
  `internal/eval/replay.go` compare compiled requests and stop on mismatch.
  Replay tests need local recordings; they are not permission to collect a
  new paid recording. Test intentional request changes with fakes too.
- Run `TestDaemonToolSurfaceMatchesEmbedded` and
  `TestEmbeddedSurfaceAndEvalProfileDoNotMove` in
  `internal/daemon/tools_parity_test.go` for catalog changes, SDK parity in
  `internal/tools/parity_test.go` for manifest changes, profile reachability
  in `internal/cli/exec_surface_test.go` / `internal/cli/exec_engine_test.go`,
  and contract parity in `internal/api/api_test.go` for API changes. A listed
  stub is not a wired tool: test the actual operation through both paths.
- Launch long-lived goroutines with `safego.Go` from
  `internal/safego`, and define their cancellation, panic boundary and join
  behavior. Run `scripts/check-no-bare-go.sh`. Keep OS operations behind
  `internal/platform`; non-Linux stubs return `errors.ErrUnsupported` while
  Darwin and Windows still build. Do not hide unsupported behavior behind
  silent success.
- Bound test steps, attempts, output and wall time. Run Go commands at low
  priority with `-p 2`, and all tests inside a memory-capped scope. Use 3G
  normally and 6G for engine race tests on shared machines. Race-test every
  touched package plus engine, tools and CLI. Keep test budgets small enough
  to finish in seconds; a memory cap does not make an infinite loop correct.

From the repository root, the pre-merge commands are:

```sh
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
gofmt -l .                         # must print nothing
nice -n 19 go vet -p 2 ./...
GOMAXPROCS=2 nice -n 19 staticcheck ./...
bash scripts/check-no-bare-go.sh
nice -n 19 go build -p 2 ./...
GOOS=darwin nice -n 19 go build -p 2 ./...
GOOS=windows nice -n 19 go build -p 2 ./...

# Add every other touched package to the appropriate capped invocation.
systemd-run --user --scope -p MemoryMax=3G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -race ./internal/tools/... ./internal/cli/...
systemd-run --user --scope -p MemoryMax=6G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -race ./internal/engine/...
# Full regression before merge; same cap includes engine race tests.
systemd-run --user --scope -p MemoryMax=6G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -race ./...
```

Use the actual Go binary on PATH inside the scope. If the user systemd
manager is unavailable, establish an equivalent cgroup memory limit before
running tests; do not fall back to an uncapped run. `staticcheck` has no Go
`-p` flag; constrain its concurrency with `GOMAXPROCS=2` on shared machines.
The repository's `make verify` also defines the standard checks, but use the
explicit capped commands above when sharing a memory-limited host.

## Common pitfalls

- **A scripted provider never finishes.** A fake can repeat its last response
  forever while the finish gate asks for more work. Give engine tests an
  explicit small step/attempt budget and context deadline, and make the
  script reach a valid terminal state. Assert that failure paths terminate.
- **The tool hides a failed command.** A nil Go error or nonempty stdout does
  not imply success. Preserve the exit code and normalized status; include
  useful stderr. Never convert a failed process into an apparently successful
  completion merely because output was captured.
- **Output consumes the context or disappears.** Set an inline budget, store
  the redacted full output, return the retrieval reference and test paging
  through the same store. Truncation without a resolvable reference forces
  unnecessary repeated work.
- **A name exists but its service does not.** A stub, registered manifest or
  deferred marker can pass a name-list test while invocation says “not
  wired.” Exercise discovery, execution, result retrieval and state changes
  with the actual injected fake or service.
- **A schema edit moves a pinned hash.** Input schemas, descriptions,
  ordering and skill catalog changes can alter cache prefixes or profile
  fingerprints. Identify exactly which contract changed, record the decision
  and update only the corresponding expectations after reviewing the diff.
- **Background work outlives its owner or dies too soon.** Define whether a
  child process belongs to one call, one run, a detached terminal session or
  the daemon. Use the existing session manager and platform process-group
  operations, not an untracked shell child. Test cancellation, disconnect,
  completion and daemon shutdown. See `internal/tools/terminal.go`,
  `internal/tools/terminal_procs.go` and
  `internal/tools/terminal_sessions_test.go` for the current lifecycle seam.
