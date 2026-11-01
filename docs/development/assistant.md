# Assistant daemon wiring

This page records which assistant (daemon) features run in production and
how they start. The architecture specification defines each feature; the
section numbers below refer to it.

## What the daemon starts

`excalibur daemon start` assembles one service stack and starts it before
the gate serves requests. `excalibur daemon stop` cancels the service loops
and waits up to five seconds for each one to exit.

| Service | Spec | Behaviour |
|---|---|---|
| Agenda timer | 21.1, 21.2 | Fires scheduled work and reminders through the orchestrator. |
| Trigger pump | 21.3 | Multiplexes event sources. Daemon-side adapters push normalized events into the internal event channel. No external webhook ingress exists yet. |
| Daily memory review | 10.6, 21.9 | Runs per scope in the review loop. It checks the schedule every 15 minutes and yields between scopes. |
| Retention and backup | 4.6, 4.7 | Polls every 10 minutes and runs once a day in the 03:00 local hour: journal and tool-blob retention on the main database, then a `VACUUM INTO` backup in the seven-daily and four-weekly rotation. |
| RAM watchdog | 5.2, 22.5 | Samples the daemon RSS every second and sheds load over the threshold. |

### RAM threshold

The watchdog threshold is three quarters of physical RAM on Linux. Set
`EXCALIBUR_RAM_THRESHOLD_MB` to choose a value in MiB. Other platforms have
no sampler yet, so the watchdog stays inert unless the variable is set.

## Engine and tool surface

The daemon step engine takes its model endpoint from the configured
provider. With no provider configured, every run fails with a clear
configuration error. The engine builds its context requests from the same
tool registry it executes, so the model sees the tools it can call.

The daemon tool surface is composed by the same function as the embedded
path (`wireengine.BuildEmbedded`). Memory, library, document, skill, task,
subagent and note tools therefore answer with their real providers on the
daemon path. Memory tools use the daemon memory service. Subagent and
directive acknowledgement tools work against the daemon orchestrator.

## Vault

The vault opens at daemon start, in this order:

1. A passphrase file named by `EXCALIBUR_VAULT_PASSPHRASE_FILE` unlocks it.
   The file must have mode `0600`.
2. Otherwise the platform keyring unlocks it when reachable. The first use
   creates the data key there.
3. Otherwise the vault starts locked. The daemon keeps running, and tools
   that need a secret wait on the user.

Provider keys resolve through the vault when the vault is unlocked. If the
key lives in a locked vault at start, the daemon starts with the
deterministic intake path and logs a notice. Restart the daemon after
`excalibur vault unlock` to pick up the provider.

Exec resolves `secret://` references in its environment inside the tool
process, and every tool output is redacted before it reaches the journal,
context or telemetry.

### CLI

| Command | Behaviour |
|---|---|
| `vault set <name> [--type T] [--url U] [--user X] [--scopes S] [--tools T] [--domains D] [--stdin]` | Creates or updates an entry. The value comes from the terminal without echo, or from standard input with `--stdin`. |
| `vault get <name>` | Prints the value to standard output. It is the only command that prints a value. |
| `vault list`, `vault show <name>` | Print metadata only. |
| `vault edit <name> [--scopes S] [--tools T] [--domains D]` | Changes the given policy fields. |
| `vault rm <name> --yes` | Deletes an entry. `--yes` is required. |
| `vault generate <name> [--length N] [--charset C]` | Stores a random value. The value never prints. |
| `vault lock`, `vault unlock` | Go through the running daemon when reachable. Without a daemon they report that nothing stays unlocked. |
| `vault rotate` | Replaces the data key in one transaction. It refuses while a daemon runs, because the daemon holds the current key. |
| `vault export <file>`, `vault import <file>` | Move entries through an encrypted backup with its own passphrase. |
| `vault usage <name> [--limit N]` | Shows where a secret was used. It never prints values. |

Every command takes `--data-dir` and `--passphrase-file`. Use
`--backup-passphrase-file` with `export` and `import` in scripts.

## Projects

The project registry persists in the main database. Projects registered
through `project.register` survive a daemon restart, and so do archives.

## Tests

- `internal/daemon/services_lifecycle_test.go`: all services start, run and
  stop together.
- `internal/daemon/engine_turn_test.go`: one daemon turn through a scripted
  provider, and the unconfigured failure.
- `internal/daemon/tools_parity_test.go`: daemon tool surface against the
  embedded surface, and pinned hashes of the embedded tool list, prefix and
  eval profile.
- `internal/daemon/vault_test.go`: vault start flows, the `vault.*` gate
  round trip, provider key resolution, and redaction of exec output.
- `internal/daemon/projects_store_test.go`: project registry across a
  restart.
- `cmd/excalibur/vault_test.go`: the vault CLI round trip, export and
  import, and the daemon-less lock path.

## Not yet wired

- The daemon has no run driver. Runs that the orchestrator spawns (agenda
  fires, trigger work, `task.start`) are queued without an engine attached,
  because the orchestrator attaches engines only on the embedded path.
- Provider credentials resolve when the provider is built, not per call.
- The daily review does not yet yield to user activity.
- Telemetry trimming and the WAL checkpoint phase of retention need a
  telemetry database handle in the daemon.
