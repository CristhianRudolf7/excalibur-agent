# Building and running on macOS

The Darwin platform backends support both Apple Silicon (`arm64`) and Intel
(`amd64`) Macs. macOS is a supported platform. Native integration checks require a Mac;
cross-compilation on Linux checks compilation, not macOS runtime behavior.
The platform implementation follows Sections 3.4, 20.1 and 25; see the
[macOS platform decision](../decisions/0104-macos-platform.md) for its contracts
and limitations.

## Build prerequisites

Install Apple's [Xcode command line tools](https://developer.apple.com/documentation/xcode/installing-the-command-line-tools/)
for the C compiler and macOS SDK:

```sh
xcode-select --install
```

Install Go with [Homebrew](https://docs.brew.sh/Installation):

```sh
brew install go
go version
```

Use the Go version required by `go.mod` or newer (currently 1.27.3). From the
repository root, build the native binary with cgo and SQLite FTS5:

```sh
mkdir -p bin
CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/excalibur ./cmd/excalibur
./bin/excalibur --help
```

This selects the SQLite driver with FTS5 and sqlite-vec linked through cgo.
A plain build without `sqlite_fts5`, or a build with `CGO_ENABLED=0`, selects
the alternate pure-Go SQLite driver, which includes FTS5 but lacks sqlite-vec
vector search. The Darwin platform code itself does not require cgo.
Cross-builds with cgo disabled do not validate the native SQLite/cgo build.

## Paths, Keychain and autostart

Configuration and data retain the XDG layout: `~/.config/excalibur`,
`~/.local/share/excalibur` and `~/.cache/excalibur`, unless overridden by
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_CACHE_HOME`. Runtime files use
`XDG_RUNTIME_DIR` or a user-specific directory under the macOS temporary root.

The vault stores its master key in macOS Keychain through `/usr/bin/security`.
Use an unlocked user Keychain in a logged-in session. Headless sessions may
need the explicitly configured passphrase fallback; the CLI exposes
`--vault-mode passphrase` and `--passphrase-file` for unattended unlock.
Protect an unlock file with mode `0600` and keep it out of the repository.

Run the installer from the built binary. Add `--autostart` to create and load
the user LaunchAgent:

```sh
./bin/excalibur install --autostart
```

The default plist is
`~/Library/LaunchAgents/com.excalibur.daemon.plist`. It starts `daemon start`
at login and restarts unsuccessful exits through launchd. Installation uses
the current user's `gui/<uid>` launchctl domain, so it requires a GUI login
session; it is not a system LaunchDaemon. Logs live under the XDG data
directory's `logs` child. The plist captures PATH, TMPDIR and explicit XDG
overrides; reinstall it after changing those values or moving the binary.
launchd does not provide the Linux systemd watchdog protocol. macOS process
priority uses nice and Darwin background scheduling; Linux cgroup memory
limits are unavailable.

## Browsers and diagrams

Install Google Chrome separately. The installer downloads the Playwright
driver only, without downloading browsers. The default `chrome` channel
searches `/Applications/Google Chrome.app`, then
`~/Applications/Google Chrome.app`, then `google-chrome` on PATH. The standard
system bundle uses Playwright's channel resolution; other discovered locations
are passed as an explicit executable. The explicit `chromium` channel also
accepts Chromium app bundles and `chromium` on PATH. Browser sandboxing remains
enabled by default.

Browser-based diagram rendering checks `chromium`, `chromium-browser`,
`google-chrome` and `google-chrome-stable` on PATH first, followed by Google
Chrome and Chromium bundles in `/Applications` and `~/Applications`.

## Robot Lab

Robot Lab on macOS requires Apple Silicon. The pinned torch release has no
macOS x86_64 wheel, so the installer and runtime reject Intel Macs. Installing
the optional Python dependencies requires an explicit action through the
Robot Lab Install card and Python 3.10 or newer. macOS uses built-in CGL
rendering, without an OSMesa dependency. The installer derives a macOS
requirements file without the Linux torch `+cpu` suffix or CPU wheel index.

The Python child attempts the address-space limit first and falls back to a
data-size limit if XNU rejects it because existing mappings exceed the limit.
Both memory limits are best-effort on macOS; CPU limits and background
priority still apply. This does not provide Linux bubblewrap isolation.

## Verification

On a Mac, run the Darwin tests and the native SQLite build checks:

```sh
go vet ./...
go test -race -p 2 -count=1 ./internal/browser ./internal/tools ./internal/robotlab
CGO_ENABLED=1 go test -race -p 2 -tags sqlite_fts5 ./internal/store
```

Darwin tests inject browser lookups and filesystem results, exercise both
architecture gates, and mock Python resource-limit failures. Native browser
launch, actual CGL rendering and dependency installation still need a Mac with
those optional dependencies. Keychain tests use injected backends by default;
real Keychain checks require the existing `EXCALIBUR_KEYCHAIN_TEST=1` opt-in.
LaunchAgent lifecycle and other platform integration tests also require a
macOS runner. No native macOS execution was performed by this Linux worker.
