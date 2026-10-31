# Recovery, continuous operation and install (H10)

Contract and backend tests of the H10 area: boot after a crash or
reboot with safe mode (17.3), interruption, pause and cancel across
runs (17.5), adoption of daemon-less tasks (3.6), retention jobs
(4.7), installation with systemd autostart, supervised operation and
update with rollback (25), and the vault bindings to providers and
tools (20.1 hook). Per-run journal replay stays in `internal/engine`
(H3) and run control in `internal/orchestrator` (H8); this document
covers only the daemon side, which reaches both through narrow
interfaces so neither package is modified here.

## Contract

- **Boot order (17.3)**: open databases (`quick_check` on unclean
  shutdown, pending migrations, porter start), rebuild RAM indexes,
  collect unfinished runs ordered master turns, user runs, scheduled,
  background. Per run: rebuild from the last checkpoint plus trailing
  entries, reconcile the world (file fingerprint diff, terminal
  sessions marked lost, capability tasks reconsulted), classify the
  pending tool leg per 17.2, then decide: `resume` when running,
  reconciled without uncertainty and interrupted under 6h (with an L5
  recovery note naming lost sessions and tree state); `waiting` when
  the run expected the user (question intact); `blocked` with an
  urgent notice on any uncertain non-idempotent effect (never repeats
  automatically); `paused(recovery)` otherwise. Agenda catches up per
  17.4; one summary notice closes the boot.
- **Safe mode (17.3)**: 3 crashes in 10 minutes park everything as
  `paused(safe_mode)`, the agenda records without launching, and the
  mode clears by user order or after 30 clean minutes. A run active
  at the same step during all 3 crashes is quarantined as `blocked`
  (possible crash cause) until an explicit order releases it.
  Markers persist in `<data>/crashes.json` (atomic temp+rename,
  `0600`) so fresh processes share the history.
- **Adoption (3.6)**: `ScanTaskFiles` lists `<project>/.excalibur/*.db`
  under the registered projects; `AdoptAll` imports each in
  last-write order. A corrupt file is reported, never fatal to boot.
- **Fast lane (17.5)**: `ParseOrder` accepts the slash commands and
  the Spanish/English forms (`/stop`, `para`, `para X`, `detén X`,
  `cancela X`, `descarta X`, `para todo`, `sigue X`, …); anything
  else is an error, never a guess. `Apply` dispatches over the
  `Control` seam with no model handle on the path, so orders work
  with the provider down. Master-turn interruption cancels the token,
  waits `TurnInterruptGrace` (1s) for a clean stop, aborts and leaves
  the interruption marker.
- **Retention (4.7)**: `RunRetention` executes the daily pass in
  order (compact journals, release tool blobs, trim telemetry, WAL
  checkpoint, `VACUUM INTO` backup) as idempotent phases; a failing
  phase aborts and the next pass retries from the start.
  `RetentionScheduler` fires once per day in the maintenance window.
- **Install (25.2)**: data/config/cache/runtime dirs (`0700`),
  layered TOML config without secrets, environment and
  low-priority mechanism probe (cgroup delegation or nice/ioprio,
  reported not fatal), timezone and provider registration, vault
  creation, optional systemd user unit (`Restart=on-failure`,
  growing `RestartSec`, `WatchdogSec`) enabled via linger for
  headless servers.
- **Update (25.3)**: ed25519-signed manifest (channel, version, hash,
  notes) verified before anything is staged; the drain waits out
  `after_completion` tools and urgent questions instead of
  interrupting them; swap is atomic with the previous binary kept.
  Migration failure restores the backup copies (no new data yet) and
  the old binary; post-boot failure rolls back the binary WITHOUT
  restoring the database (expand-then-contract migrations), so
  received messages survive. Pinned versions skip silently.
- **Watchdog (25.2)**: `WATCHDOG=1` via `sd_notify` only while the
  interactive path answers; a hung path stays silent so systemd
  restarts it. The loop runs under `safego.Go`.
- **Vault bindings (20.1)**: `VaultAdapter` binds `*vault.Vault` to
  the `SecretBackend` seam. `ProviderHook` resolves
  `provider_credential` at call time (vault wins; eval never opens;
  delegate scoped to its project). `ToolsRedactor` is the 13.4 step-5
  output filter before journal, context, telemetry, ATIF and blobs.
  `ResolveEnv` injects `secret://` env values inside the tool
  process with zeroing cleanup.

## Backend tests

- `internal/daemon/boot_test.go`: 17.3 decision table (resume,
  pause, block, waiting, quarantine, safe mode precedence), pause
  and block reason mapping, L5 note contents, boot ordering
  (kind then age), safe-mode trip/park/defer/cooldown/order,
  quarantine trip and release, adoption (skip, fail-open, scan).
- `internal/daemon/crash_test.go`: 17.8 rows — kill before intent
  (replay from recorded response without recalling the model),
  kill-point table (pure rerun, idempotent postcondition, keyed
  same-key resend, non-idempotent block), request-without-response
  repeats, compaction crash keeps sequence order, file changes and
  lost sessions surface in the L5 note, waiting questions restore
  intact.
- `internal/daemon/interrupt_test.go`: order forms, rejection of
  non-orders and ambiguous targets, fast-lane dispatch with the
  provider down, stop-turn isolation from background work.
- `internal/daemon/retention_job_test.go`: phase order, fail-abort
  with idempotent retry, nil-hook skipping, daily window firing.
- `internal/daemon/install_test.go`: layout, secret-free layered
  config, unit contents, warnings, config preservation, autostart
  without a manager fails.
- `internal/daemon/update_test.go`: happy path report, bad
  signature and hash rejection before staging, drain hold on
  `after_completion` tools and urgent questions, migration failure
  restores data plus binary, post-boot failure keeps the database
  (no message loss), pinning skips, file store swap/rollback.
- `internal/daemon/watchdog_test.go`: pet only when alive, silence
  when hung, socket-less no-op, ready/stop lifecycle, loop under
  `safego.Go`.
- `internal/daemon/vaultbind_test.go`: provider resolution at call
  time with use recording, closed without a vault, eval lockout,
  delegate scoping, output redaction markers, env injection with
  cleanup.
- `cmd/excalibur/install_update_test.go`: clean install into temp
  XDG dirs with unit verification, update check/verify/reject
  rows, missing-input failure.
