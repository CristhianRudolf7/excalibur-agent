# Platform — Linux implementation and primitives

Contract and backend tests of the `internal/platform` area: every
operating-system dependency of the harness behind interfaces (Sections
3.2, 3.4, 3.7, 5.2, 13.4, 13.5, 20.1, 25.2). Later milestones consume
these interfaces; this document only grows with contract changes.

## Contract

`internal/platform/platform.go` declares the nine shared interfaces;
this area implements them for Linux and adds the Section 3.2
primitives. Nothing here changes the shared signatures (see
`docs/decisions/0005-platform-linux-notes.md`).

- **Paths** (`paths_linux.go`): XDG data/config/cache roots plus
  `excalibur`, runtime root (`$XDG_RUNTIME_DIR`, then
  `/run/user/<uid>`, then a private temp dir), extension lookup
  `<data>/extensions`, `EnsureDirs` (0700) for the installer.
- **ProcessGroup** (`procgroup_linux.go`): `Configure` sets `Setpgid`
  and `Pdeathsig=SIGTERM`; `Kill(-pgid)` sends SIGTERM, waits up to
  2s for the leader, then SIGKILL. Missing groups report success.
- **ProcessPriority** (`priority_linux.go`): delegated path writes
  `cpu.weight` (default 20) into the caller's cgroup v2 directory
  resolved from `/proc/<pid>/cgroup`; otherwise nice (+10, clamped to
  19) plus best-effort idle I/O class. `LowerCurrentThread` lowers the
  calling worker thread (setpriority on the tid; raw getpriority is
  `20 - nice`, converted). Additive: `SetMemoryMax`,
  `Delegated`, `EffectiveNice`, `EffectiveWeight`.
- **ResourceSampler** (`sampler_linux.go`): RSS from
  `/proc/<pid>/status`, CPU seconds from `/proc/<pid>/stat` over
  HZ=100, process count from `pids.current` (else thread count), OOM
  kills from `memory.events`.
- **FileLock** (`flock_linux.go`): advisory `flock(2)` on a
  0600 file with parent creation; idempotent release. Additive
  `AcquireContext` polls `LOCK_EX|LOCK_NB` for cancellation.
- **Keyring** (`keyring_linux.go`): Secret Service via
  `zalando/go-keyring`; `Fallback` hook serves Get/Set/Delete when no
  bus is reachable (the Section 20.1 passphrase path); `Available`
  probes the bus without touching entries.
- **ServiceManager** (`service_linux.go`): writes the user unit
  (`Restart=on-failure`, `RestartSec=5s`, `WatchdogSec=30s`,
  `ExecStart=<bin> daemon start`) to `~/.config/systemd/user`,
  then `daemon-reload` + `enable --now` when systemctl exists on a
  non-overridden path. `Status` reports `absent`, the `is-active`
  state, or `installed`.
- **PTY** (`pty_linux.go`): `creack/pty` sessions with read/write,
  resize, idempotent close, wait. Nil commands rejected.
- **Isolator** (`isolator_linux.go`): pass-through, the Section 20
  extension point.
- **Admission** (`admission.go`, portable): semaphore capping
  background engines at `NumCPU - reserved` (default reserve 2, min
  one slot); context-aware `Acquire`, non-blocking `TryAcquire`,
  idempotent `Close` unblocking waiters with `ErrAdmissionClosed`.
- **GOMEMLIMIT** (`memlimit.go`, portable): `SetGOMEMLIMIT`,
  `GOMEMLIMITForBudget` (90% of budget), `ApplyBudgetRAM`.
- **CPU pool** (`cpupool/`, portable with per-OS lowering): pinned
  low-priority workers via `safego.Go`; `New`/`NewWithLower`,
  blocking `Submit`, `TrySubmit` (`ErrFull`), idempotent `Close`;
  panicking tasks fail alone. Linux lowers each thread (nice +10);
  other platforms no-op.
- **Stubs** (`platform_other.go`, `!linux`; `cpupool/lower_other.go`):
  every interface returns `errors.ErrUnsupported` so darwin/windows
  builds keep compiling.

## Backend tests

- `paths_linux_test.go`: XDG overrides, runtime fallback, 0700
  `EnsureDirs`, extensions suffix.
- `procgroup_linux_test.go`: `Setpgid`+`Pdeathsig` wiring, group kill
  of `sleep`, reaped-group success, invalid pid rejection.
- `priority_linux_test.go`: delegated `cpu.weight` write-back and
  `EffectiveWeight` against a stub cgroup root mirroring
  `/proc/self/cgroup`; `SetMemoryMax` set/clear; nice fallback raises
  `EffectiveNice` without delegation (Section 3.7 with/without
  cgroups); thread nice rises on the pinned thread; invalid pids.
- `sampler_linux_test.go`: self sample (RSS>0, CPU>=0, processes>=1),
  child sample, missing-pid error, stub `pids.current`/`oom_kill`
  values.
- `flock_linux_test.go`: exclusivity blocking, release handoff,
  idempotent release, 8-goroutine counter exclusion, ctx expiry and
  free-lock acquire.
- `keyring_linux_test.go`: fallback roundtrip with broken bus,
  no-fallback error, `ErrNotFound` vs unavailable classification.
- `service_linux_test.go`: lifecycle in a temp unit dir (absent,
  install content with restart/watchdog/exec, status, uninstall,
  idempotency) and `EXCALIBUR_SYSTEMD_DIR` override.
- `pty_linux_test.go`: `echo` output through the PTY, resize, `cat`
  echo roundtrip, nil-command rejection.
- `isolator_linux_test.go`: pass-through leaves the command as-is.
- `admission_test.go`: cap/default sizing, blocking at cap, ctx
  cancel, close unblocking with `ErrAdmissionClosed`, 16-goroutine
  peak concurrency bound.
- `memlimit_test.go`: budget derivation, set/restore roundtrip.
- `cpupool/cpupool_test.go`: all tasks run, default sizing, lower
  hook per worker, panic isolation, full-queue `ErrFull`,
  post-close `ErrClosed`, idempotent close.
- `platform_other_test.go` (`!linux`): every stub returns
  `errors.ErrUnsupported`.
- Repo-wide: `make verify` (`gofmt`, `vet`, `staticcheck`,
  bare-go guard, `go test ./... -race`, darwin/windows builds).

## Deferred (not in this area)

No orchestrator wiring (H8), no sd_notify watchdog pings or
`enable-linger` handling (H10 daemon work), no vault KDF/unlock flow
(H10; the keyring only exposes the fallback hook), no cgroup
per-run delegation setup (created by the future executor), no macOS
or Windows backends.
