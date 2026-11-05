# Windows development

Windows is a supported platform, alongside Linux and macOS. Native backends
implement paths, process trees, priorities, sampling, file locks, Credential
Manager, Task Scheduler and ConPTY. Cross-compilation verifies buildability;
native integration checks require a Windows runner and installed dependencies.

## Requirements

- Go 1.27.3 or later, as declared in `go.mod`.
- Node.js 22 or later and pnpm 12.16.1, pinned in `package.json`.
  Run `corepack enable` to enable pnpm.
- Git for Windows is recommended for existing Bash scripts. The web development
  launchers and Go verification commands work without Bash or Make.
- Chrome or Edge is optional for browser tools. Web Playwright configurations
  currently select Chrome.
- LibreOffice is optional for legacy document conversion.
- Python 3, available through the `py` launcher, is optional for Python workers.
- MinGW-w64 GCC and cgo are required for Go's Windows amd64 race detector.
  Ordinary builds use the portable SQLite backend unless built with `sqlite_fts5`.

## Build and test in PowerShell

Run these commands from the repository root; Make is not required:

```powershell
go build -p 2 -o .\excalibur.exe ./cmd/excalibur
go build -p 2 ./...
gofmt -l .
go vet -p 2 ./...
go run -p 2 ./scripts/check-no-bare-go
go test -p 2 -count=1 ./...
```

With GCC on `PATH` and `go env CGO_ENABLED` reporting `1`, add `-race` to the
`go test` command. The Windows workflow runs build, vet, race tests for native
backend packages, and web checks on `windows-latest`. Cross-compiling on Linux
checks compilation; it does not verify Win32 behavior. The portable goroutine
checker parses all platform files and excludes tests and the sanctioned launch
site in `internal/safego`.

```powershell
corepack enable
pnpm install --frozen-lockfile
pnpm --filter @excalibur/web typecheck
pnpm --filter @excalibur/web lint
pnpm --filter @excalibur/web test
pnpm --filter @excalibur/web build
node --test apps/web/scripts/processes.test.mjs
cd apps/web
pnpm dev:full
```

`dev:full` builds a temporary daemon, starts it on `127.0.0.1:7787`, and starts
Vite. Set `$env:EXCALIBUR_DEV_LISTEN` to override the daemon address. The launcher
inherits the environment, uses the OS temporary directory for disposable data,
and removes it after shutdown. Ctrl+C stops the child process trees; Windows
uses `taskkill /T /F`. Forced termination of the launcher itself may leave its
temporary directory. `pnpm test:e2e` sets the Robot Lab test tracker without shell
environment syntax and forwards Playwright arguments. Integration tests use
`node scripts/dev-daemon.mjs`; the existing Linux shell launcher remains available.

## Windows platform backends

| Location | Directory |
| --- | --- |
| Data | `%LOCALAPPDATA%\Excalibur` |
| Configuration | `%APPDATA%\Excalibur` |
| Runtime files | `%LOCALAPPDATA%\Excalibur\run` |

Autostart uses a per-user Task Scheduler task that runs the daemon at
logon, with restart supervision. The vault keyring uses Windows Credential
Manager through the existing keyring adapter; secrets are not stored in the
scheduled task or command line. These integrations belong behind
`internal/platform` (Sections 3.4, 20.1 and 25).

Interactive terminal sessions use ConPTY and require Windows 10 version 1809 or
later (Section 13.5). Diagram renderers have no Windows network isolation;
network namespace isolation remains unavailable. Backend tests cover these interfaces. Actual Task Scheduler registration,
Credential Manager access, ConPTY interaction and child-tree cancellation must
also be checked on a native Windows host; this Linux checkout cannot verify them.
