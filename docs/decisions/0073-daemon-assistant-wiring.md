# 0073: Wire the assistant daemon features into production

The architecture specification defines several assistant features that were
implemented as libraries but never started or never reached the daemon:
the agenda and trigger loops, the daily review, retention, the RAM
watchdog, the vault, the project registry, and most tools on the daemon
path. `docs/development/assistant.md` lists the production start order and the
tool surface.

## Decisions

- **One orchestrator for the stack.** `Assemble` accepts the orchestrator
  the command created, so the agenda, trigger launchers and the engine
  share one set of runs. Before this, two orchestrators existed and only
  one was reachable.
- **Loops report completion.** Each service loop returns a done channel.
  `Stop` cancels the loops and waits up to five seconds, so shutdown is
  observable in tests and nothing outlives the daemon.
- **Daemon tool surface is the embedded composition.** The daemon calls
  `wireengine.BuildEmbedded` and attaches the daemon memory service,
  instead of keeping its own list of stubs. The parity test checks every
  embedded tool against the daemon, and pinned hashes keep the embedded
  surface and the eval profile from moving.
- **Vault start is non-fatal when locked.** A locked vault leaves the
  daemon running. Secret-dependent work waits on the user, as Section 20.1
  requires.
- **Vault CLI works on the local file.** Entry operations open `vault.db`
  directly. Lock and unlock use the gate when a daemon runs. Rotation is
  refused beside a daemon, since the daemon holds the current key.
- **RAM threshold default.** Three quarters of physical RAM, overridable in
  MiB with `EXCALIBUR_RAM_THRESHOLD_MB`. Section 5.2 names a configured
  threshold without a default, so this default is a product choice.
- **Projects persist through the existing mirror.** The workspace package
  already had a SQL mirror and a restore path. Production now attaches them.

## Not decided here

- A per-run tool surface. The daemon engine shares one registry and
  executor across runs, so per-run identity (file versions, secret uses)
  is not recorded on the daemon path yet.
- A daemon run driver for orchestrator-spawned runs.
