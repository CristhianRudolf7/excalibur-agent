# 0005 — Linux platform notes and discrepancies

Date: 2026-10-31. Scope: `internal/platform` Linux implementations
(Sections 3.2, 3.4 table, 3.7, 5.2, 13.4, 13.5, 20.1, 25.2).

No shared contract was changed: `internal/platform/platform.go` is
untouched, and every addition is a new file, constructor, method on a
concrete Linux type, or subpackage. The `Other*` stubs still return
`errors.ErrUnsupported` and the binary still builds for darwin and
windows.

## Discrepancies and closest feasible behaviour

1. `FileLock.Acquire` has no `context.Context` parameter, but its
   wave-0 comment promises blocking "until the lock is held or ctx
   expires". Changing the signature would break the shared contract,
   so `Acquire` keeps blocking flock semantics and the additive
   `AcquireContext(ctx, path)` delivers the expiry via
   `LOCK_EX|LOCK_NB` polling. Alternative (changing the interface)
   rejected: other agents code against the fake, which mirrors the
   current signature.

2. `ProcessPriority.LowerProcess(pid)` carries no memory limit, but
   Section 13.4 describes `memory.max` per execution. `LowerProcess`
   therefore never imposes a memory cap (it writes only `cpu.weight`
   on the delegated path, nice+ioprio otherwise); the additive
   `SetMemoryMax(pid, bytes)` applies the orchestrator's per-run
   budget separately. Alternative (a fixed default cap inside
   `LowerProcess`) rejected: an arbitrary cap could OOM-kill
   legitimate tool runs.

3. The raw `getpriority(2)` syscall returns `20 - nice`, and
   `golang.org/x/sys/unix` exposes the raw value. All nice reads go
   through a `getNice` conversion (`20 - raw`); without it the
   fallback policy misfires (observed: nice 0 read as 20, lowering to
   "19" read back as 1). Verified empirically against libc
   `getpriority` and `/proc` stat. Alternative (using libc via cgo)
   rejected: cgo would break the darwin/windows cross builds.

4. `ServiceManager.Install` writes the unit file first and treats a
   missing `systemctl` (or an overridden `UnitDir` /
   `EXCALIBUR_SYSTEMD_DIR` sandbox outside systemd's search path) as
   degraded success: the file is the source of truth and `Status`
   reports `installed`. A real default-path install still surfaces
   `systemctl` errors. Alternative (hard failure without systemd)
   rejected: containers and minimal servers must install the file
   before the supervisor exists.

5. `ResourceSampler.Sample` counts `Processes` from the owning
   cgroup's `pids.current`, falling back to `/proc/<pid>/task`
   threads; `OOMKills` is 0 without a cgroup. Machine-wide process
   counts are deliberately not reported: the watchdog (Section 5.2)
   needs the daemon's footprint, not the host's.

## Initial values chosen for calibration (Appendix B)

- `DefaultToolWeight = 20` (cgroup v2 `cpu.weight` vs the kernel
  default 100), `DefaultToolNice = 10`, ioprio idle class fallback.
- `killGracePeriod = 2s` between SIGTERM and SIGKILL (Appendix B).
- Admission default `NumCPU - 2` (`DefaultReservedCPUs`), minimum one
  slot. CPU pool default `NumCPU` workers, queue `4 * size`.
- `GOMEMLIMIT` target 90% of the RAM budget (`GOMEMLIMITForBudget`);
  cgo/SQLite page caches stay outside the limit by Go runtime design.
