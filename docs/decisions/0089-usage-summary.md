# 0089: Usage summary from recorded metrics

The additive `usage.summary` client API reads the existing telemetry database
and metrics.query snapshots (Section 22.2). The Go registry, JSON method export,
gateway dispatch and TypeScript contract include the method. No model tools,
provider adapters, prices or eval pins change.

The daemon opens telemetry.db with the existing telemetry migrations and
attaches its SQLStore to BackendServices. Queries merge retained SQL snapshots
with current metrics.query rows; a later in-memory snapshot replaces the SQL
snapshot with the same run ID. Records without a run ID deduplicate by task ID.
Database failures remain errors, rather than becoming empty successes.

Since is inclusive and until exclusive. Malformed timestamps, reversed/equal
bounds and unsupported bucket sizes return invalid_params. Whole snapshots are
assigned to their UTC start time; legacy records without a start use their stored
update timestamp. The default bucket is day; hour is also supported. Costs are
summed as recorded, without estimation or repricing. Cached tokens are cache-read
plus cache-write tokens. Input/output counters retain their stored meanings.
Model rows appear only when the snapshot records a model. Tool calls come from
CallsByTool; failures count error execute_tool spans using gen_ai.tool.name,
filtered by the span start time. No retry counter is treated as a failure.

Buckets sort oldest first, models/tools by name and recent runs newest first
with run-ID tie-breaking. Recent runs are capped at 50 without truncating totals.
No data produces zero totals and empty arrays. Task short codes are omitted
because telemetry snapshots do not contain them. Duration uses recorded end minus
start, or recorded wall seconds for unfinished/legacy snapshots.

Storage limits remain visible: task_metrics retains the latest snapshot per
task, not every historical attempt. Missing/retained-away detail cannot be
reconstructed. The new reader does not introduce a new metrics producer or
invent historical data; runs without recorded metrics are absent. Failure
spans and run snapshots have independent retention and time granularity.

`EXCALIBUR_USAGE_SUMMARY` defaults on; 0/false/off disable the new client
method. Backend verification is documented in `docs/development/int2-be-2.md`.
