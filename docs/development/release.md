# Public release engineering

## Native CI corrections

The logs for runs 41843594333 (macOS) and 41843595594 (Windows) were
retrieved through the jobs API. Both native workflows now cover main pushes
and pull requests. Staticcheck is pinned to 2026.2.2.

- Google tool errors follow Go error conventions.
- Removed unused Torch helpers; the runtime no longer installs Torch separately.
- Robot Lab fake commands recognize the Windows `py -3` prefix. Path assertions
  use native separators. Unsupported-host installer tests assert rejection.
- Rendering uses CGL on macOS, no MUJOCO_GL on Windows, and the operator's
  selected backend on Linux. The macOS dependency test checks the actual pip
  requirements step. Its Python environment test now checks the real child
  environment instead of importing the removed `robotlab.sandbox` module.
- Removed `TestDarwinSandboxMemoryLimits`: it tested that removed Python
  module, which is no longer on any runtime execution path. No replacement
  Python sandbox or claim of Python memory limits is introduced here.
- Protected-path changes are tests only: `TestExecSignalRecords` allows 60
  seconds for a race-instrumented helper to initialize on Intel macOS;
  shutdown still has its original two-second assertion.
  `TestExecRateLimitsExpireWallBudget` allows 30 seconds so initialization
  does not consume the entire provider retry budget.
- The Settings focus failures in these runs predate the committed fix placing
  Passwords after Robot Lab. The current component already matches the test.
  The Windows media POSIX-mode assertion reported in those logs is also absent
  from the current branch.

Native macOS and Windows execution must be confirmed by CI; this workspace
runs Linux.

## Embedded application and lifecycle

`make web` builds `apps/web` and copies its output into the ignored
`internal/webui/assets/built/` directory. A committed placeholder keeps plain
Go builds independent of Node. Release jobs embed the production build; no
Node, pnpm or Go is required on the installed machine.

The persistent daemon serves the SPA alongside `/rpc`, `/ws`, `/blobs` and
`/healthz`. Missing assets and unknown API subpaths return 404. Browser
requests must have the same origin; loopback UI deployments also validate the
Host header. No browser bearer token was required by the existing API. The
WebSocket transports retain their routes, with origin checks now covering the
main event socket as well. Default daemon listen is `127.0.0.1:7787`; use
`--listen` or `EXCALIBUR_LISTEN` to override it. Container binding explicitly
requires `--allow-remote`.

A fresh daemon initializes the account relay using the existing default
model selection so Settings → Connections can connect a ChatGPT/Codex account
without restarting. Existing explicit provider selections are retained.
No live account login or paid model request is part of ordinary verification.

`excalibur install --autostart` configures the native per-user service and
starts the daemon. Minimal Linux sessions without a user systemd bus emit a
warning and start a detached daemon for the current session; the unit file
remains available for the next user session. Browser driver setup is best
effort and bounded to two minutes. `EXCALIBUR_SKIP_BROWSER_INSTALL=1` explicitly
defers it for offline installations and release smoke tests.

`excalibur open` prints the URL. `excalibur uninstall` removes the service and
stops the daemon; `--purge` additionally removes data, configuration and cache.
The executable remains available to reinstall. On Windows, purging preserves
the binary subtree inside the native application data directory.

Local Linux verification: the production bundle built; the real shell
installer downloaded and verified a locally served archive into a temporary
HOME; `/` and `/settings` served the SPA, `/healthz` answered, and same-origin
principal/account/chat RPCs succeeded. Playwright using `channel: 'chrome'`
loaded the connection screen and sent a chat message with a daemon receipt.
Uninstall stopped the daemon and removed the service. The service name in
smoke tests is unique (`EXCALIBUR_SERVICE_NAME`) to isolate it from other
installations. Native autostart behavior remains subject to native CI.

The non-root Linux Docker image was built locally from the embedded binary,
started with a named `/data` volume and loopback host port mapping, served
`/` and `/healthz`, and reached Docker's healthy state as user `10001:10001`.
