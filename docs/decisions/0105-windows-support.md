# Windows support and portable development tooling

Platform scope is Linux, Windows (in progress) and macOS (in progress), with
macOS implemented by a separate team. Windows extends the platform boundary in
Sections 3.4 and 25 through native backends. Backend implementation and runtime
verification remain separate from this tooling change; existing Linux behavior
and shared tool definitions are preserved. No new third-party modules are added.

The web development daemon launcher is a Node entry point that builds a temporary
OS-specific executable, preserves the existing environment and daemon flags, and
removes disposable state on exit. Nested Node launchers exchange shutdown
messages so cleanup finishes before a parent forces termination. POSIX children
run in process groups with TERM/KILL cleanup; Windows uses `taskkill /T /F`. The full-stack launcher uses
Node for the daemon and Windows shell resolution for pnpm. The Linux Bash daemon
launcher remains available. Playwright integration starts the Node launcher, and
the E2E wrapper sets the test tracker environment without shell syntax.

A standard-library Go checker provides the goroutine-launch check in PowerShell
without requiring Bash. It parses every platform's Go files, ignores comments
and strings, and retains the existing test and `internal/safego/safego.go`
exemptions. The existing Bash checker is retained for Linux workflows.

The cross-build target covers amd64 and arm64 for Windows and Darwin. A dedicated
Windows workflow builds, vets, checks goroutine launches and runs native backend
race tests with cgo on the amd64 Windows runner; a separate job checks the web
client and portable process supervisor. Linux cross-compilation alone cannot
validate Win32 process, terminal, service or keyring behavior.

The Windows design uses per-user AppData directories, Task Scheduler autostart,
Windows Credential Manager for the vault keyring, and ConPTY on Windows 10 1809+
(Sections 13.5, 20.1 and 25). Diagram renderer network isolation remains
unavailable on Windows. See [Windows development](../development/windows.md)
for prerequisites, PowerShell commands and pending runtime checks.

Local verification passed Go vet and builds for Linux, Windows and Darwin, plus
Windows/Darwin arm64 builds, Windows checker-test compilation, checker race
tests, staticcheck and the daemon tool-surface/hash tests. Web typecheck, lint,
formatting and production build passed; all 566 unit tests passed with
`EXCALIBUR_TEST_WORKERS=1` after assertion deadlines failed on the contended
shared host. Four process-supervisor tests passed. Linux daemon smoke runs
confirmed HTTP health, SIGTERM shutdown, nested SIGINT propagation and temporary
state cleanup. Native Windows execution remains for the Windows workflow.
