# Scheduled automations

`NewManager(Config) (*Manager, error)` restores the working set from the
shared `*store.DB`. A nil DB selects an in-memory working set. The SQL
backend requires migration 9 from `migrations.Main()`. Existing wire types
and dependency interfaces in `types.go` are unchanged.

Set `AgentRunner`, `ToolInvoker`, and `Notifier` to daemon adapters. Set
`WorkDir` to the daemon data directory's automation subdirectory when the
daemon uses a custom data location; the default is
`$XDG_DATA_HOME/excalibur/automations` (or
`~/.local/share/excalibur/automations`). `Now` supports deterministic tests.
`Language: "en"` selects English completion messages; otherwise they are
Spanish.

Call `Start(ctx)` once to enable scheduling. Manual operations and event
subscriptions work before Start. Runs use a manager-owned context, so an
RPC request ending does not cancel its background run. Call `Stop()`
before closing the database: it cancels runs, waits for executors and
bounded completion notifications, and drains the ordered event queue.
Subscription callbacks may call Service methods; they should return
promptly. Unsubscribing prevents future deliveries from being queued;
already queued deliveries may still arrive.

The manager validates a reachable acyclic graph of 1–30 steps. A run
follows one path through condition branches, retaining shared merge nodes
and skipping untaken nodes. Prior outputs are strings; conditions also
receive valid JSON outputs at `steps.<id>.json`. Templates are expanded in
instructions, code, messages, and recursively in tool argument values.
Scripts use `python3`, `node`, or `bash` inside a persistent per-automation
directory, with prior outputs in `AUTOMATION_INPUT`. Output previews are
limited to 2,000 Unicode characters, summaries to 300, and subprocess
capture buffers to 64 KiB. Script files are removed after execution; other
files remain available to later runs. Scripts are supported on Linux;
other platforms return `errors.ErrUnsupported`.

Runs have a 15-minute budget. Pause, delete, shutdown, and parent context
cancellation stop running work. Three consecutive failures pause the
schedule. SQL snapshots atomically preserve current state, full version
specifications, and the latest 200 runs. Runs interrupted by a process
restart are marked cancelled. The scheduled slot is consumed durably
before execution; overdue schedules execute once at startup, then advance
to a future slot. Finished once schedules remain active without a slot.
Daily DST gaps advance to the first valid wall minute and overlaps use
the first occurrence. Monthly dates clamp to the last calendar day.

Creation approval is enforced by the agent tool, owned by AUTO-BE2; the
manager is the trusted service boundary. RPC routing, event-bus bridging,
and daemon adapters are also integrated by AUTO-BE2.

Verification:

```sh
systemd-run --user --scope -q -p MemoryMax=6G go test -race -p 2 ./internal/automations/... ./internal/store/...
go vet ./internal/automations/... ./internal/store/...
staticcheck ./internal/automations/... ./internal/store/...
gofmt -l internal/automations internal/store/migrations
```
