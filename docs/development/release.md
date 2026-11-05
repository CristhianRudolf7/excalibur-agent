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

## Download scripts

The POSIX shell and PowerShell installers live in `scripts/install/`; identical
copies are served by the site from `apps/site/public/`. CI checks byte equality.
They select Linux/macOS/Windows and amd64/arm64, download the matching archive
and `checksums.txt`, verify SHA-256 before replacement, install into the user's
bin directory, add PATH without replacing existing entries, install autostart,
and wait for health. Reruns stop the previous service while keeping data.

`EXCALIBUR_VERSION=vX.Y.Z` pins an existing GitHub release tag;
`EXCALIBUR_BASE_URL=http://127.0.0.1:PORT` overrides the asset directory for
local smoke tests. The main-function wrapper prevents a truncated POSIX
script from starting installation before its complete function has arrived.
ShellCheck 0.11.0 passes for the shell installer and key generation wrapper.

Additional verification fixes discovered locally: `TestExecEngineSkillsCards`
used an absolute missing directory outside its temporary project, so the scope
guard could reject it before make produced the intended error. It now invokes
an absent make target inside the project (test-only protected-path change).
`TestSIGTERMContext` and the classroom daemon helper allow realistic startup
budgets on race-instrumented shared runners. Delivery screenshot fixtures use
PNG BestSpeed encoding; the image dimensions and tested evidence are unchanged.

## Signed updates

`excalibur update` downloads the public latest-release assets without GitHub
credentials. `checksums.txt.sig` is a hex Ed25519 signature of the exact bytes
of `checksums.txt`. The signed file authenticates both the archive and
`version.txt`. Archives are read in memory and only their regular executable
entry is accepted. Tampering with the signature, checksum document, version,
or archive is rejected before staging. Go tests exercise Linux, macOS and
Windows archive formats with simulated HTTP servers and generated test keys.

The public key belongs in the single `daemon.ReleasePublicKey` constant in
`internal/daemon/release.go`. It is intentionally empty until the coordinator
supplies the real public key; update and release publication fail closed in
that state. `scripts/release/genkey /secure/path/release.pem` creates a new
PKCS8 Ed25519 private PEM with mode 0600 and prints only the public hex key.
It refuses to overwrite an existing key. Keep the PEM outside the repository
and put its contents in GitHub's `EXCALIBUR_RELEASE_KEY` secret. Signing rejects
a private key that does not match the compiled trust root.

The updater preserves the existing local `--manifest`, `--payload`, `--pubkey`,
`--channel` and `--check` path. For public updates it refuses nonterminal runs,
stops an idle daemon gracefully, stages and swaps through `daemon.Updater`,
checks the new executable's version, restarts an installed service and waits
for HTTP health. A failed health check rolls back the binary; user databases
are retained. The original executable path is retained across the swap so
service files do not accidentally point to the backup image.

## Cutting a release

1. Generate the signing key outside the repository with
   `scripts/release/genkey /secure/path/release.pem`. Set the printed public key
   in `daemon.ReleasePublicKey`, and have the repository owner configure the
   PEM contents as `EXCALIBUR_RELEASE_KEY`. No private key enters this tree.
2. Merge the release work, run `make web` and `make verify`, and confirm native
   main CI. The coordinator creates and pushes a `vX.Y.Z` tag; workers do not
   push tags, publish releases or change GitHub settings.
3. `.github/workflows/release.yml` builds the web once, embeds it into six
   native Go builds and stamps `main.version`. It packages
   `excalibur_<os>_<arch>.tar.gz` or `.zip`, runs the real installer against each
   local archive, checks UI, health and same-origin chat intake, and uninstalls.
   Windows ARM64 uses CGO disabled and modernc SQLite because the bundled
   sqlite-vec Windows library is x64; the other builds use CGO and sqlite_fts5.
4. A Docker check builds and runs both Linux architectures from those native
   archives. Only after all native and Docker checks pass does the publish job
   create the GitHub Release with generated notes, signed checksums and
   version file, and push the multi-architecture image to GHCR with `latest`
   and the version tag. Manual dispatch takes an existing version tag.
5. Confirm the public site's installer copies match their source scripts and
   smoke the public URLs after the coordinator deploys the site. Site/DNS and
   GitHub configuration are outside this worker's changes.

For a local Linux smoke (after `make web`):

```sh
mkdir -p bin/linux/amd64
go build -tags sqlite_fts5 -trimpath -ldflags '-s -w -X main.version=v0.0.0-smoke' -o bin/linux/amd64/excalibur ./cmd/excalibur
python3 scripts/release/package.py --os linux --arch amd64 --binary bin/linux/amd64/excalibur --out /tmp/excalibur-assets --version v0.0.0-smoke
EXCALIBUR_SMOKE_BROWSER=1 go run ./scripts/release/smoke /tmp/excalibur-assets
docker build --build-arg TARGETARCH=amd64 -t excalibur-local .
docker run -d --name excalibur-local -p 127.0.0.1:7787:7787 -v excalibur-local-data:/data excalibur-local
curl -fsS http://127.0.0.1:7787/healthz
curl -fsS http://127.0.0.1:7787/
```

The Chrome check uses installed Google Chrome (`channel: 'chrome'`) and web
workspace Playwright dependencies. They are developer test dependencies only.
The fixture uses a temporary HOME on Unix, no real model credentials, and a
local HTTP asset server. Windows uses the runner's disposable native known
folders. The smoke removes its service and temporary Unix home on exit.

An installed listen override can be persisted with
`excalibur install --autostart --listen 127.0.0.1:17787` or by setting
`EXCALIBUR_LISTEN` during installation. It is stored in `listen.addr` beside the
user config, so native services retain it without inheriting the installer
shell. `excalibur open` and both scripts read that effective address. Only
loopback addresses and nonzero ports are accepted for installed services;
containers use their explicit daemon flags instead.

The fresh, unconfigured Linux daemon was also checked separately in Chrome:
the Connect button successfully started account sign-in and the event
WebSocket received frames, without completing external OAuth or calling a
paid API. Linux download archives require glibc; the shell installer executes
`--version` before replacing an existing install and reports unsupported
runtime/loader failures clearly.
