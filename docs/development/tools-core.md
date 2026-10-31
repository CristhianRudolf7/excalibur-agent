# H3 tools half — core tools, pipeline, terminal, fingerprints

Contract and backend tests of the H3 tools half (wave w1-tools-core):
RAM catalog, execution pipeline, core file/terminal/control tools,
fingerprints and leases. Engine, journal, orchestrator, LSP, deferred
tools and history three-way merge land in other waves.

Architecture reference: Sections 13.1, 13.2 (core subset), 13.4, 13.5,
13.6 and 14.12 (builtin registration). Appendix B values used verbatim.

## Contract

- **Catalog** (`catalog.go`, `registry.go`, `manifest.go`): `RegisterBuiltins`
  registers all H3 tools plus stub definitions for every other Section
  13.2 core tool through the same `Registry` interface as extensions
  (no privileged path, Section 14.12). `BuiltinManifest` emits the
  `excalibur-builtin` manifest (`excalibur-ext/1`, kind `builtin`,
  sorted tools). Names are `snake_case`, max 64 chars, unique,
  and `Names()` returns them sorted to protect the input cache.
- **Pipeline** (`pipeline.go`): `Executor.Execute` runs validate, resolve
  keys, record intent, execute with timeout/cancel, redact, normalize and
  truncate, store full output, record result (Section 13.4 steps 1-7).
  `RepairJSON` fixes trailing commas and smart quotes before rejecting.
  `ValidateAgainstSchema` covers the object/array/string/number/integer/
  boolean/null subset with required, properties, additionalProperties,
  enum, bounds and lengths; grave errors carry the schema's required
  part. Budgets: inline budget 40 KiB (~10K tokens), buffer 1 MiB,
  default timeout 60 s. `TruncateHeadTail` keeps head+tail with a marker
  carrying the blob ref; `MemoryOutputStore` backs `get_tool_output`.
  Cancel modes: `immediate` propagates, `graceful` bounds by timeout,
  `after_completion` detaches from parent cancel and finishes up to the
  timeout, `cooperative` bounds the wait. `RecoveryFor` /
  `RecoveryForIdempotency` map effect classes to Section 17.2 behavior
  (rerun, check probe, resend key, uncertain); keyed tools get a
  generated idempotency key when missing.
- **Files** (`files.go`, `fingerprint.go`): `read_file` (200-line windows,
  numbers, `fp` sha256, read-set recording), `write_file` (atomic
  temp+fsync+rename, preimage blob before effect, history append,
  optional LSP diagnostics), `edit_file` (three-level match, fingerprint
  check, diff on refusal, 3-mismatch escalation), `apply_patch`
  (validate-all-then-apply, per-file leases, per-file history).
  `FingerprintStore` tracks reads and consecutive mismatches
  (escalate at 3). `FindMatch` implements exact, typographic-normalized
  and trailing-space levels with ambiguous line reporting.
  `CompactDiff` shows changed ranges with context. `MemoryLeases`
  serializes writers (30 s wait naming the holder, 10 min expiry).
  `MemoryHistory` stores preimage/postimage blobs and versions so a
  crash between intent and effect stays reversible.
- **Search** (`search.go`): `grep` (regex or literal, summarized per-file
  counts, first lines, bounded to 50 results / 20 shown lines, skips
  `.git`/`node_modules`/`target` and binaries) and `glob` (`**` support,
  `.gitignore` respected with negation and dir patterns, bounded to 200,
  sorted). Both `read_only`/`idempotent`/`immediate`.
- **Terminal** (`terminal.go`, `exec_tools.go`, `signals_*.go`):
  `SessionManager` keeps pipe and PTY sessions alive with LRU eviction
  (16 per owner, 64 global, 8 most recent protected) and `OnEvict`
  owner notice. `exec` (pipe default, `tty:true` via `platform.PTY`,
  `yield_ms` clamped 250 ms-30 s default 10 s, `target` local-only,
  env forced `PAGER=cat GIT_PAGER=cat TERM` dumb/256color) and `exec_io`
  (`list` with live tails, `read` with offsets, `write`, `keys`
  with named-key sequences, `wait_for` regex-or-exit with timeout in one
  call, `signal` interrupt/terminate/kill, `close` with SIGTERM, 2 s
  margin, SIGKILL). Live tail is last 20 lines up to 2 KiB for `status`.
  Process launch uses `platform.ProcessGroup` (own group),
  `ProcessPriority` (lowered), `Isolator` (extension point) and
  `Setpgid`+`Pdeathsig` on Linux. `DetectShell`/`DetectEnv` report OS,
  arch, shells, package managers and tools. All background readers go
  through `safego.Go`.
- **Control** (`control.go`): `clock` (`now`/`convert`/`add` with IANA
  zones and Go durations, deterministic, `read_only`/`pure`/`fast`),
  `get_tool_output` (`ref`+`offset`/`limit` paging over `OutputStore`
  with `History` blob fallback, `fast`), `status` (renders
  `SnapshotProvider` data plus session summaries with live tails;
  without a provider it reports the H8 stub, `fast`).
- **Stubs** (`catalog.go` `CoreStubDefinitions`): every other Section 13.2
  core tool (conversation, web, task_db, memory, docs, skills, subagent,
  reminders, settings) has a validated definition with correct
  effect/idempotency/cancel/fast flags and an interface-owned provider
  hook; unwired calls report the owning subsystem instead of failing
  silently.
- **Effects/cancel/fast**: file reads `read_only`, writes `write_local`
  + `after_completion`, exec `exec`/`non_idempotent` + `graceful`,
  searches `read_only` + `immediate`. Master-fast: `status`, `clock`,
  `get_tool_output` (plus stubs per Section 13.2).

## Backend tests

- `pipeline_test.go`: minor repair (trailing comma) runs; grave JSON
  fails with `input_error` carrying the schema part; journal intent/result
  hooks fire; timeout maps to `timeout`; `immediate` cancels vs
  `after_completion` finishes; head+tail truncation stores the blob and
  `get_tool_output` returns it whole; redactor runs before truncation;
  recovery classes per Section 17.2; keyed tools get generated keys.
- `files_test.go`: read windows/numbers/fp + read-set + resource keys;
  atomic write with parents, history pre/postimage blobs; external change
  between read and write refused with current-region diff and no write;
  3rd consecutive mismatch escalates; edit levels 2 (typographic) and 3
  (trailing space), ambiguous multi-match line numbers, diagnostics
  appended; patch multi-file apply, add-over-existing fails atomically;
  lease wait names holder, release re-grants; diff shows -/+ ranges.
- `search_test.go`: grep per-file summary, bounded, bad regex is input
  error; glob `**` spanning, `.gitignore` respected and opt-out,
  segment matching.
- `terminal_test.go`: pipe echo output; `wait_for` returns on pattern
  without polling to timeout; terminate ends the process, close removes
  the session; fake-PTY keys reach the session, unknown keys fail, pipe
  sessions reject keys; per-owner and global eviction caps with owner
  notice; live tail bounded to 20 lines / 2 KiB with latest line;
  pager env forced, shell/env detection non-empty; remote `target`
  rejected as H13; `exec_io` list/read/wait_for/signal/close round trip.
- `control_test.go`: clock now/convert (UTC->Lima) /add deterministic,
  bad zone is input error; output paging bytes range, unknown ref is
  input error; status renders provider data plus session tail, unwired
  provider reports the H8 stub; catalog names validate/sorted/unique,
  manifest validates, fast flags match the master set.
- Existing `tools_test.go`, `parity_test.go`, `fake` tests keep passing:
  name boundary, definition matrix, registry dup/unknown, extsdk parity.

## Deferred (not in w1-tools-core)

LSP diagnostics wiring beyond the `DiagnosticProvider` hook (13.8),
deferred tools and `search_tools`/`call_tool` behavior (13.3), history
three-way merge and `revert_edit`/`workspace_snapshot` (13.7), shadow
snapshots, remote node targets (14.10), vault redaction wiring (20.1),
journal durability beyond the `IntentHook`, and any engine/orchestrator
ownership. Each lands with its milestone; the hooks above are the seam.
